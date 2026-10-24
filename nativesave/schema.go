package nativesave

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"strconv"
	"strings"
	"unicode/utf8"
)

// strictDecode validates against the actual typed record before unmarshalling.
// In particular, encoding/json alone accepts duplicate and case-aliased keys,
// missing required members, and replacement characters for malformed strings.
func strictDecode(data []byte, dst any) error {
	return decodeSchema(data, dst, 4096)
}

// DecodeHostJSON reuses the native exact schema checker for the private host
// protocol/continuation. The host separately enforces its smaller narrative and
// 8MiB attachment bounds. Existing mechanical records retain their 4096 limit.
func DecodeHostJSON(data []byte, dst any) error {
	if len(data) == 0 || len(data) > 16<<20 {
		return fmt.Errorf("native host JSON exceeds frame bound")
	}
	return decodeSchema(data, dst, 1<<20)
}
func decodeSchema(data []byte, dst any, maxString int) error {
	if err := validJSONStrings(data); err != nil {
		return err
	}
	t := reflect.TypeOf(dst)
	if t == nil || t.Kind() != reflect.Pointer {
		return fmt.Errorf("native schema destination must be a pointer")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	token, err := d.Token()
	if err != nil {
		return err
	}
	if err := schemaValue(d, token, t.Elem(), 0, "record", maxString); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return fmt.Errorf("extra or malformed JSON after record: %v", err)
	}
	return json.Unmarshal(data, dst)
}

// Marshal replaces malformed UTF-8 in Go strings. Reject it in caller-owned
// records before encoding so a save cannot silently change a display name/key.
func validGoStrings(v reflect.Value, depth int) error {
	if depth > 64 {
		return fmt.Errorf("native value exceeds depth 64")
	}
	if !v.IsValid() {
		return nil
	}
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if !v.IsNil() {
			return validGoStrings(v.Elem(), depth+1)
		}
	case reflect.String:
		if !utf8.ValidString(v.String()) || len(v.String()) > 4096 {
			return fmt.Errorf("native value contains malformed or oversized string")
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if v.Type().Field(i).PkgPath == "" {
				if err := validGoStrings(v.Field(i), depth+1); err != nil {
					return err
				}
			}
		}
	case reflect.Slice, reflect.Array:
		if v.Type().Elem().Kind() != reflect.Uint8 {
			for i := 0; i < v.Len(); i++ {
				if err := validGoStrings(v.Index(i), depth+1); err != nil {
					return err
				}
			}
		}
	case reflect.Map:
		it := v.MapRange()
		for it.Next() {
			if err := validGoStrings(it.Key(), depth+1); err != nil {
				return err
			}
			if err := validGoStrings(it.Value(), depth+1); err != nil {
				return err
			}
		}
	}
	return nil
}

type schemaField struct {
	typeOf   reflect.Type
	optional bool
}

func schemaFields(t reflect.Type, fields map[string]schemaField) {
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.PkgPath != "" {
			continue
		}
		tag := strings.Split(f.Tag.Get("json"), ",")
		if tag[0] == "-" {
			continue
		}
		if f.Anonymous && tag[0] == "" && f.Type.Kind() == reflect.Struct {
			schemaFields(f.Type, fields)
			continue
		}
		name := tag[0]
		if name == "" {
			name = f.Name
		}
		optional := false
		for _, option := range tag[1:] {
			optional = optional || option == "omitempty"
		}
		fields[name] = schemaField{f.Type, optional}
	}
}

func schemaValue(d *json.Decoder, token json.Token, t reflect.Type, depth int, name string, maxString int) error {
	if depth > 64 {
		return fmt.Errorf("native JSON exceeds depth 64")
	}
	if token == nil {
		if t.Kind() == reflect.Pointer || t.Kind() == reflect.Slice || t.Kind() == reflect.Map {
			return nil
		}
		return fmt.Errorf("%s: null is not valid for %s", name, t)
	}
	if t.Kind() == reflect.Pointer {
		return schemaValue(d, token, t.Elem(), depth, name, maxString)
	}
	bad := func() error { return fmt.Errorf("%s: expected %s, got %v", name, t, token) }
	switch t.Kind() {
	case reflect.Struct, reflect.Map:
		if token != json.Delim('{') {
			return bad()
		}
		fields := map[string]schemaField{}
		if t.Kind() == reflect.Struct {
			schemaFields(t, fields)
		} else if t.Key().Kind() != reflect.String {
			return fmt.Errorf("unsupported native map key %s", t.Key())
		}
		seen := map[string]bool{}
		for d.More() {
			keyToken, err := d.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok || len(key) > 4096 || seen[key] {
				return fmt.Errorf("%s: duplicate, malformed or oversized object key", name)
			}
			seen[key] = true
			var valueType reflect.Type
			if t.Kind() == reflect.Struct {
				field, ok := fields[key]
				if !ok {
					return fmt.Errorf("%s: unknown or case-mismatched member %q", name, key)
				}
				valueType = field.typeOf
			} else {
				if strings.HasSuffix(t.Key().Name(), "ID") && len(key) > 256 {
					return fmt.Errorf("%s: map identity exceeds 256 bytes", name)
				}
				valueType = t.Elem()
			}
			value, err := d.Token()
			if err != nil {
				return err
			}
			if err := schemaValue(d, value, valueType, depth+1, key, maxString); err != nil {
				return err
			}
		}
		if end, err := d.Token(); err != nil || end != json.Delim('}') {
			return fmt.Errorf("%s: malformed object end", name)
		}
		for key, field := range fields {
			if !field.optional && !seen[key] {
				return fmt.Errorf("%s: missing required member %q", name, key)
			}
		}
	case reflect.Slice, reflect.Array:
		if t.Kind() == reflect.Slice && t.Elem().Kind() == reflect.Uint8 {
			encoded, ok := token.(string)
			if !ok || len(encoded) > 4096 {
				return bad()
			}
			if _, err := base64.StdEncoding.Strict().DecodeString(encoded); err != nil {
				return fmt.Errorf("%s: malformed binary encoding", name)
			}
			return nil
		}
		if token != json.Delim('[') {
			return bad()
		}
		n := 0
		for d.More() {
			value, err := d.Token()
			if err != nil {
				return err
			}
			if err := schemaValue(d, value, t.Elem(), depth+1, name, maxString); err != nil {
				return err
			}
			n++
			if t.Kind() == reflect.Array && n > t.Len() {
				return fmt.Errorf("%s: oversized fixed array", name)
			}
		}
		if end, err := d.Token(); err != nil || end != json.Delim(']') {
			return fmt.Errorf("%s: malformed array end", name)
		}
		if t.Kind() == reflect.Array && n != t.Len() {
			return fmt.Errorf("%s: fixed array length mismatch", name)
		}
	case reflect.String:
		s, ok := token.(string)
		if !ok || len(s) > maxString {
			return bad()
		}
		if (strings.HasSuffix(t.Name(), "ID") || name == "id" || name == "request_id") && len(s) > 256 {
			return fmt.Errorf("%s: identity exceeds 256 bytes", name)
		}
	case reflect.Bool:
		if _, ok := token.(bool); !ok {
			return bad()
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, ok := token.(json.Number)
		if !ok {
			return bad()
		}
		if _, err := strconv.ParseInt(string(n), 10, t.Bits()); err != nil {
			return fmt.Errorf("%s: invalid bounded integer", name)
		}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n, ok := token.(json.Number)
		if !ok {
			return bad()
		}
		if _, err := strconv.ParseUint(string(n), 10, t.Bits()); err != nil {
			return fmt.Errorf("%s: invalid bounded unsigned integer", name)
		}
	default:
		return fmt.Errorf("unsupported native schema type %s", t)
	}
	return nil
}

func validJSONStrings(data []byte) error {
	if !utf8.Valid(data) {
		return fmt.Errorf("native JSON contains malformed UTF-8")
	}
	for i := 0; i < len(data); i++ {
		if data[i] != '"' {
			continue
		}
		for i++; i < len(data) && data[i] != '"'; i++ {
			if data[i] != '\\' {
				continue
			}
			i++
			if i >= len(data) {
				return fmt.Errorf("truncated native string escape")
			}
			if data[i] != 'u' {
				continue
			}
			if i+4 >= len(data) {
				return fmt.Errorf("truncated native Unicode escape")
			}
			u, err := strconv.ParseUint(string(data[i+1:i+5]), 16, 16)
			if err != nil {
				return fmt.Errorf("invalid native Unicode escape")
			}
			i += 4
			if u >= 0xdc00 && u <= 0xdfff {
				return fmt.Errorf("unpaired native low surrogate")
			}
			if u >= 0xd800 && u <= 0xdbff {
				if i+6 >= len(data) || data[i+1] != '\\' || data[i+2] != 'u' {
					return fmt.Errorf("unpaired native high surrogate")
				}
				low, err := strconv.ParseUint(string(data[i+3:i+7]), 16, 16)
				if err != nil || low < 0xdc00 || low > 0xdfff {
					return fmt.Errorf("invalid native surrogate pair")
				}
				i += 6
			}
		}
	}
	return nil
}
