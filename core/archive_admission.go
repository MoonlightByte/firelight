package core

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"unicode/utf8"
)

const MaxArchiveBytes uint64 = 128 << 20
const MaxArchiveRecordBytes = 1 << 20

// MaxGenesisRecordBytes bounds the one genesis record, so a world declared up
// to its entity caps still publishes. Batch records keep the 1 MiB bound.
const MaxGenesisRecordBytes = 16 << 20
const archiveReserve uint64 = 64 << 10

// This small walk enforces the native Alpha encoding's string boundaries on
// actual typed records. It does not clone values, execute callbacks or define a
// second schema/model. Legacy records are outside this opt-in admission rule.
func alphaStringBounds(v reflect.Value, depth int, field string) error {
	if depth > 64 {
		return adventureFault("encoding", "depth <=64", "too deep")
	}
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if !v.IsNil() {
			return alphaStringBounds(v.Elem(), depth+1, field)
		}
	case reflect.String:
		limit := 4096
		if strings.HasSuffix(v.Type().Name(), "ID") || field == "ID" || field == "RequestID" {
			limit = 256
		}
		if len(v.String()) > limit || !utf8.ValidString(v.String()) {
			return adventureFault("encoding", fmt.Sprintf("valid UTF-8 string <=%d bytes", limit), "invalid or overlong")
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if e := alphaStringBounds(v.Field(i), depth+1, v.Type().Field(i).Name); e != nil {
				return e
			}
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			if e := alphaStringBounds(v.Index(i), depth+1, field); e != nil {
				return e
			}
		}
	case reflect.Map:
		iter := v.MapRange()
		for iter.Next() {
			if e := alphaStringBounds(iter.Key(), depth+1, field); e != nil {
				return e
			}
			if e := alphaStringBounds(iter.Value(), depth+1, field); e != nil {
				return e
			}
		}
	}
	return nil
}
func alphaEncoded(v any) ([]byte, error) {
	if e := alphaStringBounds(reflect.ValueOf(v), 0, ""); e != nil {
		return nil, e
	}
	return json.Marshal(v)
}
func genesisArchiveBytes(b Blueprint) (uint64, error) {
	if b.Adventure == nil && b.Equipment == nil && b.Clock == nil && len(b.ItemDefinitions) == 0 {
		return 0, nil
	}
	raw, e := alphaEncoded(b)
	if e != nil {
		return 0, e
	}
	if len(raw) > MaxGenesisRecordBytes-1024 {
		return 0, adventureFault("genesis bytes", "at most 16MiB minus 1KiB framing", "too large")
	}
	return uint64(len(raw)) + archiveReserve, nil
}
func (a *authority) admitBatch(batch Batch) (uint64, error) {
	if a.state.Adventure == nil && a.state.Equipment == nil && a.state.Clock == nil && len(a.state.ItemDefinitions) == 0 {
		return 0, nil
	}
	if len(a.history) >= MaxArchiveBatches {
		return 0, fail("E_LIMIT", batch.Request.ID, "history", "at most 8192 committed requests", "full", "campaign history capacity exhausted")
	}
	raw, e := alphaEncoded(batch)
	if e != nil {
		return 0, e
	}
	if len(raw) > MaxArchiveRecordBytes-64 {
		return 0, adventureFault("batch bytes", "at most 1MiB minus 64 framing", "too large")
	}
	amount := uint64(len(raw)) + 64
	if a.archiveBytes > MaxArchiveBytes || amount > MaxArchiveBytes-a.archiveBytes {
		return 0, fail("E_LIMIT", batch.Request.ID, "history bytes", "at most 128MiB complete archive", "full", "campaign byte capacity exhausted")
	}
	return amount, nil
}
