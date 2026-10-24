package nativesave

import (
	"bytes"
	"github.com/MoonlightByte/firelight/core"
	"reflect"
	"testing"
)

func TestCreationArchiveSemanticsAndExactHistoricalReceipt(t *testing.T) {
	for _, mode := range []string{"plain", "equipment", "clock", "both"} {
		t.Run(mode, func(t *testing.T) {
			b := saveTestBlueprint()
			oldTag := SemanticsVersion
			if mode == "equipment" || mode == "both" {
				b.Equipment = &core.EquipmentRules{Version: core.EquipmentVersion}
				oldTag = EquipmentSemanticsVersion
			}
			if mode == "clock" || mode == "both" {
				b.Clock = &core.Clock{Unit: "tick"}
				oldTag = ClockSemanticsVersion
			}
			w, e := core.NewWorld(b)
			if e != nil {
				t.Fatal(e)
			}
			s := SessionState{Campaign: "birth-test", Next: 1}
			old, info := saveTestEncode(t, w.ExportCheckpoint(), s)
			if info.Semantics != oldTag {
				t.Fatal(info)
			}
			loaded, e := Read(bytes.NewReader(old))
			if e != nil {
				t.Fatal(e)
			}
			round, _ := saveTestEncode(t, loaded.World.ExportCheckpoint(), loaded.Session)
			if !bytes.Equal(old, round) {
				t.Fatal("creation-free byte roundtrip changed")
			}
			birth := saveTestCommit(t, w, &s, core.Operation{CreateCharacter: &core.Character{ID: "new", Name: "Ari", Location: "camp", Stats: map[core.StatID]int64{"defense": 12}, Resources: map[core.ResourceID]core.Resource{"hp": {Current: 8, Maximum: 12}}}})
			saveTestCommit(t, w, &s, core.Operation{Damage: &core.ResourceOp{Character: "new", Resource: "hp", Amount: 8}})
			data, info := saveTestEncode(t, w.ExportCheckpoint(), s)
			if info.Semantics != CreationSemanticsVersion {
				t.Fatal(info)
			}
			loaded, e = Read(bytes.NewReader(data))
			if e != nil {
				t.Fatal(e)
			}
			if !reflect.DeepEqual(w.ExportCheckpoint(), loaded.World.ExportCheckpoint()) {
				t.Fatal("creation facts/history lost")
			}
			before := loaded.World.ExportCheckpoint()
			got, e := loaded.World.Execute(birth.Request)
			if e != nil || !reflect.DeepEqual(got, birth) || !reflect.DeepEqual(before, loaded.World.ExportCheckpoint()) {
				t.Fatal("historical birth reexecuted", e)
			}
			for _, corrupt := range [][]byte{
				bytes.ReplaceAll(data, []byte(CreationSemanticsVersion), []byte(oldTag)),
				bytes.ReplaceAll(old, []byte(oldTag), []byte(CreationSemanticsVersion)),
			} {
				if bad, e := Read(bytes.NewReader(corrupt)); e == nil || bad != nil {
					t.Fatal("tag/history contradiction returned World")
				}
			}
			// Presence of CharacterCreated under an old tag cannot authorize ignoring
			// unknown payloads; full event/request consistency still runs first.
			corrupt := bytes.Replace(data, []byte(`"defense":12`), []byte(`"defense":13`), 1)
			if bad, e := Read(bytes.NewReader(corrupt)); e == nil || bad != nil {
				t.Fatal("corrupted original creation admitted")
			}
		})
	}
}

func TestStoreAllowsAuditedCreationTagTransition(t *testing.T) {
	b := saveTestBlueprint()
	b.Clock = &core.Clock{Unit: "tick"}
	w, e := core.NewWorld(b)
	if e != nil {
		t.Fatal(e)
	}
	st, e := Acquire(t.TempDir(), "birth-store", true)
	if e != nil {
		t.Fatal(e)
	}
	defer st.Close()
	s := SessionState{Campaign: "birth-store", Next: 1}
	if _, e = st.SaveJoint(w.ExportCheckpoint(), s, []byte(`{"phase":"before"}`)); e != nil {
		t.Fatal(e)
	}
	birth := saveTestCommit(t, w, &s, core.Operation{CreateCharacter: &core.Character{ID: "new", Location: "camp"}})
	if _, e = st.SaveJoint(w.ExportCheckpoint(), s, []byte(`{"phase":"after"}`)); e != nil {
		t.Fatal(e)
	}
	loaded, e := st.LoadJoint()
	if e != nil {
		t.Fatal(e)
	}
	if loaded.Info.Semantics != CreationSemanticsVersion || string(loaded.Attachment) != `{"phase":"after"}` {
		t.Fatal("joint creation tag transition")
	}
	got, e := loaded.World.Execute(birth.Request)
	if e != nil || !reflect.DeepEqual(got, birth) {
		t.Fatal(e)
	}
}
