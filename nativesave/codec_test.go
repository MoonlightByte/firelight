package nativesave

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/MoonlightByte/firelight/core"
)

func saveTestPtr[T any](v T) *T { return &v }

func saveTestBlueprint() core.Blueprint {
	return core.Blueprint{
		Rules: core.Rules{Transfer: core.Unequips, Wear: core.WearAny}, Locations: []core.Location{{ID: "camp", Name: "Camp"}},
		Characters:           []core.Character{{ID: "hero", Name: "Ari", Location: "camp", Stats: map[core.StatID]int64{"defense": 10}, Resources: map[core.ResourceID]core.Resource{"hp": {Current: 6, Minimum: 0, Maximum: 10}, "focus": {Current: 3, Minimum: 0, Maximum: 3}}}},
		ConditionDefinitions: []core.ConditionDefinition{{ID: "guard", Instances: core.Unique, Modifiers: []core.ConditionModifier{{ID: "defense", Stat: "defense", Fixed: saveTestPtr(int64(1))}}}},
		Conditions:           []core.ConditionInstance{}, RetiredConditionIDs: []core.ConditionInstanceID{"old-genesis"},
		AbilityDefinitions: []core.AbilityDefinition{{ID: "guard-action", Parameters: []core.AbilityParameter{{ID: "fresh", Kind: core.ConditionIDArgument}}, Steps: []core.AbilityStep{
			{Spend: &core.SpendTemplate{Payer: core.AbilityOperand{Literal: &core.AbilityValue{Character: saveTestPtr(core.CharacterID("hero"))}}, Resource: "focus", Amount: core.AbilityOperand{Literal: &core.AbilityValue{Integer: saveTestPtr(int64(2))}}}},
			{ApplyCondition: &core.ConditionTemplate{Instance: core.AbilityOperand{Parameter: "fresh"}, Type: "guard", Target: core.AbilityOperand{Literal: &core.AbilityValue{Character: saveTestPtr(core.CharacterID("hero"))}}}},
		}}},
	}
}

func saveTestCommit(t *testing.T, w *core.World, s *SessionState, op core.Operation) core.Batch {
	t.Helper()
	id := fmt.Sprintf("%s:request:%d", s.Campaign, s.Next)
	b, err := w.Execute(core.Request{ID: id, ExpectedRevision: w.Revision(), Actor: core.Ref{Kind: core.CharacterKind, ID: "hero"}, Operations: []core.Operation{op}})
	if err != nil {
		t.Fatal(err)
	}
	s.Next++
	s.LastAttempt = &AttemptRef{RequestID: id, Committed: true}
	return b
}

func saveTestFixture(t *testing.T) (*core.World, SessionState) {
	t.Helper()
	w, err := core.NewWorld(saveTestBlueprint())
	if err != nil {
		t.Fatal(err)
	}
	s := SessionState{Campaign: "codec-test", Next: 1}
	saveTestCommit(t, w, &s, core.Operation{Invoke: &core.InvokeAbility{Ability: "guard-action", Arguments: []core.AbilityArgument{{Name: "fresh", Value: core.AbilityValue{ConditionID: saveTestPtr(core.ConditionInstanceID("paid-guard"))}}}}})
	saveTestCommit(t, w, &s, core.Operation{RemoveCondition: &core.RemoveCondition{Instance: "paid-guard", Target: "hero"}})
	saveTestCommit(t, w, &s, core.Operation{Spend: &core.ResourceOp{Character: "hero", Resource: "focus", Amount: 1}})
	return w, s
}

func saveTestEncode(t *testing.T, cp core.Checkpoint, s SessionState) ([]byte, ArchiveInfo) {
	t.Helper()
	var buf bytes.Buffer
	info, err := Write(&buf, cp, s)
	if err != nil {
		t.Fatal(err)
	}
	return buf.Bytes(), info
}

func TestCodecRoundtripPreservesReceiptAndRetiredTruth(t *testing.T) {
	w, s := saveTestFixture(t)
	cp := w.ExportCheckpoint()
	data, info := saveTestEncode(t, cp, s)
	sum := sha256.Sum256(data)
	if info.Bytes != int64(len(data)) || info.SHA256 != hex.EncodeToString(sum[:]) {
		t.Fatal("physical archive identity mismatch")
	}
	loaded, err := Read(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Info != info || !reflect.DeepEqual(s, loaded.Session) || !reflect.DeepEqual(cp, loaded.World.ExportCheckpoint()) {
		t.Fatal("archive did not preserve the complete checkpoint")
	}
	before := loaded.World.ExportCheckpoint()
	old := cp.History[0]
	retry, err := loaded.World.Execute(old.Request)
	if err != nil || !reflect.DeepEqual(old, retry) || !reflect.DeepEqual(before, loaded.World.ExportCheckpoint()) {
		t.Fatal("saved paid receipt was executed again", err)
	}
	if loaded.World.Snapshot().Characters[0].Resources["focus"].Current != 0 || len(loaded.World.Snapshot().Conditions) != 0 {
		t.Fatal("fixed exhausted-funds/retired-condition oracle failed")
	}
	data[0] = '!'
	if !reflect.DeepEqual(before, loaded.World.ExportCheckpoint()) {
		t.Fatal("input bytes alias resumed authority")
	}
}

func TestCodecGenesisPresenceMask(t *testing.T) {
	for bits := 0; bits < 16; bits++ {
		t.Run(fmt.Sprint(bits), func(t *testing.T) {
			b := saveTestBlueprint()
			b.AbilityDefinitions = nil
			b.ConditionDefinitions = nil
			b.Conditions = nil
			b.RetiredConditionIDs = nil
			if bits&1 != 0 {
				b.AbilityDefinitions = []core.AbilityDefinition{}
			}
			if bits&2 != 0 {
				b.ConditionDefinitions = []core.ConditionDefinition{}
			}
			if bits&4 != 0 {
				b.Conditions = []core.ConditionInstance{}
			}
			if bits&8 != 0 {
				b.RetiredConditionIDs = []core.ConditionInstanceID{}
			}
			w, err := core.NewWorld(b)
			if err != nil {
				t.Fatal(err)
			}
			cp := w.ExportCheckpoint()
			data, _ := saveTestEncode(t, cp, SessionState{Campaign: "empty", Next: 1})
			loaded, err := Read(bytes.NewReader(data))
			if err != nil || !reflect.DeepEqual(cp, loaded.World.ExportCheckpoint()) {
				t.Fatal("nil/empty genesis shape changed", err)
			}
		})
	}
}

func TestStrictSchemaRejectsAmbiguousAndMalformedRecords(t *testing.T) {
	type sample struct {
		ID       string                `json:"id"`
		Count    uint64                `json:"count"`
		Label    string                `json:"label"`
		Stats    map[core.StatID]int64 `json:"stats"`
		Optional *int64                `json:"optional,omitempty"`
		Binary   []byte                `json:"binary"`
		Seed     [2]uint64             `json:"seed"`
	}
	valid := `{"id":"x","count":1,"label":"ok","stats":{"Defense":1,"defense":2},"binary":"AQI=","seed":[0,1]}`
	var out sample
	if err := strictDecode([]byte(valid), &out); err != nil || len(out.Stats) != 2 {
		t.Fatal("case-sensitive authored map keys rejected", err)
	}
	for _, tc := range []struct{ name, from, to string }{
		{"duplicate decoded key", `"count":1`, `"count":1,"co\u0075nt":2`},
		{"case alias", `"count":1`, `"Count":1`},
		{"missing member", `"count":1,`, ``},
		{"null scalar", `"count":1`, `"count":null`},
		{"negative unsigned", `"count":1`, `"count":-1`},
		{"float integer", `"count":1`, `"count":1.0`},
		{"exponent integer", `"count":1`, `"count":1e0`},
		{"overflow", `"count":1`, `"count":18446744073709551616`},
		{"high surrogate", `"ok"`, `"\ud800"`},
		{"low surrogate", `"ok"`, `"\udc00"`},
		{"bad pair", `"ok"`, `"\ud800\u0041"`},
		{"invalid base64", `"AQI="`, `"A?I="`},
		{"fixed array short", `[0,1]`, `[0]`},
		{"fixed array long", `[0,1]`, `[0,1,2]`},
		{"long identity", `"x"`, `"` + strings.Repeat("a", 257) + `"`},
		{"long label", `"ok"`, `"` + strings.Repeat("a", 4097) + `"`},
		{"duplicate map key", `"Defense":1`, `"defense":1`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := strictDecode([]byte(strings.Replace(valid, tc.from, tc.to, 1)), &sample{}); err == nil {
				t.Fatal("invalid schema accepted")
			}
		})
	}
	bad := bytes.Replace([]byte(valid), []byte("ok"), []byte{0xff}, 1)
	if err := strictDecode(bad, &sample{}); err == nil {
		t.Fatal("malformed UTF-8 accepted")
	}
	paired := strings.Replace(valid, `"ok"`, `"\ud83d\ude00"`, 1)
	if err := strictDecode([]byte(paired), &sample{}); err != nil {
		t.Fatal("valid surrogate pair rejected", err)
	}
	type nested struct {
		Child *nested `json:"child"`
	}
	deep := strings.Repeat(`{"child":`, 66) + `null` + strings.Repeat(`}`, 66)
	if err := strictDecode([]byte(deep), &nested{}); err == nil {
		t.Fatal("excessive nesting accepted")
	}
}

func rewriteLine(t *testing.T, data []byte, index int, edit func(map[string]any)) []byte {
	t.Helper()
	lines := bytes.Split(bytes.TrimSuffix(data, []byte{'\n'}), []byte{'\n'})
	var value map[string]any
	if err := json.Unmarshal(lines[index], &value); err != nil {
		t.Fatal(err)
	}
	edit(value)
	changed, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	lines[index] = changed
	return append(bytes.Join(lines, []byte{'\n'}), '\n')
}

func TestCodecRejectsLateDamageAndIncompleteArchives(t *testing.T) {
	w, s := saveTestFixture(t)
	cp := w.ExportCheckpoint()
	data, _ := saveTestEncode(t, cp, s)
	for _, tc := range []struct {
		name   string
		change func([]byte) []byte
	}{
		{"wrong version", func(b []byte) []byte {
			return rewriteLine(t, b, 0, func(v map[string]any) { v["meta"].(map[string]any)["format"] = "unknown" })
		}},
		{"missing head", func(b []byte) []byte {
			return rewriteLine(t, b, 0, func(v map[string]any) { delete(v["meta"].(map[string]any), "head") })
		}},
		{"null genesis", func(b []byte) []byte { return rewriteLine(t, b, 1, func(v map[string]any) { v["data"] = nil }) }},
		{"mask present conflict", func(b []byte) []byte {
			return rewriteLine(t, b, 1, func(v map[string]any) { v["empty"].(map[string]any)["abilities"] = true })
		}},
		{"mask present empty", func(b []byte) []byte {
			return rewriteLine(t, b, 1, func(v map[string]any) { v["data"].(map[string]any)["conditions"] = []any{} })
		}},
		{"mask missing flag", func(b []byte) []byte {
			return rewriteLine(t, b, 1, func(v map[string]any) { delete(v["empty"].(map[string]any), "conditions") })
		}},
		{"late trace", func(b []byte) []byte {
			return rewriteLine(t, b, 4, func(v map[string]any) {
				event := v["data"].(map[string]any)["events"].([]any)[0].(map[string]any)
				event["resource"].(map[string]any)["before"].(map[string]any)["current"] = 2
			})
		}},
		{"allocation too low", func(b []byte) []byte {
			return rewriteLine(t, b, 5, func(v map[string]any) { v["data"].(map[string]any)["next"] = 3 })
		}},
		{"last attempt contradiction", func(b []byte) []byte {
			return rewriteLine(t, b, 5, func(v map[string]any) {
				v["data"].(map[string]any)["last_attempt"].(map[string]any)["committed"] = false
			})
		}},
		{"footer count", func(b []byte) []byte {
			return rewriteLine(t, b, 6, func(v map[string]any) { v["meta"].(map[string]any)["count"] = 2 })
		}},
		{"footer state digest", func(b []byte) []byte {
			return rewriteLine(t, b, 6, func(v map[string]any) { v["state_digest"] = strings.Repeat("0", 64) })
		}},
		{"missing final newline", func(b []byte) []byte { return b[:len(b)-1] }},
		{"extra physical data", func(b []byte) []byte { return append(append([]byte{}, b...), ' ') }},
		{"oversized header", func([]byte) []byte { return []byte(strings.Repeat(" ", MaxHeadBytes) + "\n") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			loaded, err := Read(bytes.NewReader(tc.change(data)))
			if err == nil || loaded != nil {
				t.Fatal("invalid archive exposed a World", err)
			}
			if !reflect.DeepEqual(cp, w.ExportCheckpoint()) {
				t.Fatal("failed read changed live source World")
			}
		})
	}
	for i, b := range data {
		if b == '\n' && i < len(data)-1 {
			if loaded, err := Read(bytes.NewReader(data[:i+1])); err == nil || loaded != nil {
				t.Fatal("proper prefix accepted as complete archive", i)
			}
		}
	}
	bad := cp
	bad.State = w.Snapshot()
	bad.State.Characters[0].Name = "forged"
	if _, err := Write(io.Discard, bad, s); err == nil {
		t.Fatal("contradictory checkpoint audit was accepted")
	}
	invalidName := saveTestBlueprint()
	invalidName.Characters[0].Name = string([]byte{0xff})
	if x, err := core.NewWorld(invalidName); err == nil {
		if _, err := Write(io.Discard, x.ExportCheckpoint(), SessionState{Campaign: "name", Next: 1}); err == nil {
			t.Fatal("malformed Go string was silently replaced")
		}
	}
}

func TestCodecAllocationAndWorkLimits(t *testing.T) {
	w, s := saveTestFixture(t)
	cp := w.ExportCheckpoint()
	for _, next := range []uint64{0, 3, math.MaxUint64} {
		bad := s
		bad.Next = next
		if _, err := Write(io.Discard, cp, bad); err == nil {
			t.Fatal("invalid allocation accepted", next)
		}
	}
	bad := s
	bad.LastAttempt = &AttemptRef{RequestID: s.Campaign + ":request:04", Committed: false}
	bad.Next = 5
	if _, err := Write(io.Discard, cp, bad); err == nil {
		t.Fatal("noncanonical attempt sequence accepted")
	}
	r := recordReader{input: bufioReader("x\n"), bytes: MaxBytes - 1, sum: sha256.New()}
	if _, err := r.line(MaxRecordBytes); err == nil {
		t.Fatal("total read byte cap ignored")
	}
	r = recordReader{input: bufioReader(strings.Repeat("x", MaxRecordBytes) + "\n"), sum: sha256.New()}
	if _, err := r.line(MaxRecordBytes); err == nil {
		t.Fatal("record cap ignored")
	}
	var output bytes.Buffer
	writer := recordWriter{output: &output, bytes: MaxBytes - 1, sum: sha256.New()}
	if err := writer.record(struct{}{}, MaxRecordBytes); err == nil || output.Len() != 0 {
		t.Fatal("writer exceeded total cap")
	}
	if _, err := Write(shortWriter{}, cp, s); !errors.Is(err, io.ErrShortWrite) {
		t.Fatal("short write was hidden", err)
	}
}

type shortWriter struct{}

func (shortWriter) Write(p []byte) (int, error) { return len(p) - 1, nil }

func bufioReader(s string) *bufio.Reader { return bufio.NewReaderSize(strings.NewReader(s), 4096) }

func TestCodecCapacity4096WithEarlySavedRetry(t *testing.T) {
	w, s := saveTestFixture(t)
	for w.Revision() < 4096 {
		saveTestCommit(t, w, &s, core.Operation{Heal: &core.ResourceOp{Character: "hero", Resource: "hp", Amount: 1}})
	}
	cp := w.ExportCheckpoint()
	data, info := saveTestEncode(t, cp, s)
	loaded, err := Read(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if info.Count != 4096 || !reflect.DeepEqual(cp, loaded.World.ExportCheckpoint()) {
		t.Fatal("capacity archive lost continuation or history")
	}
	old := cp.History[0]
	retry, err := loaded.World.Execute(old.Request)
	if err != nil || !reflect.DeepEqual(old, retry) || !reflect.DeepEqual(cp, loaded.World.ExportCheckpoint()) {
		t.Fatal("early receipt was not retained at capacity", err)
	}
	t.Logf("actual 4096-request archive: %d bytes", len(data))
}

func TestCodecRetainsSuccessfulForeignRequestReference(t *testing.T) {
	w, e := core.NewWorld(saveTestBlueprint())
	if e != nil {
		t.Fatal(e)
	}
	receipt, e := w.Execute(core.Request{ID: "retained:outside-native-prefix", Actor: core.Ref{Kind: core.CharacterKind, ID: "hero"}, Operations: []core.Operation{{Heal: &core.ResourceOp{Character: "hero", Resource: "hp", Amount: 1}}}})
	if e != nil {
		t.Fatal(e)
	}
	state := SessionState{Campaign: "foreign", Next: 1, LastAttempt: &AttemptRef{RequestID: receipt.Request.ID, Committed: true}}
	data, _ := saveTestEncode(t, w.ExportCheckpoint(), state)
	loaded, e := Read(bytes.NewReader(data))
	if e != nil || !reflect.DeepEqual(state, loaded.Session) {
		t.Fatal(e)
	}
	state.LastAttempt.Committed = false
	if e := validateSession(state, w.History()); e == nil {
		t.Fatal("foreign rejected attempt misrepresented as owned allocation")
	}
	state.LastAttempt = &AttemptRef{RequestID: "unknown:foreign", Committed: true}
	if e := validateSession(state, w.History()); e == nil {
		t.Fatal("invented successful receipt")
	}
}
