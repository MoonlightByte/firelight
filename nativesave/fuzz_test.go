package nativesave

import (
	"bytes"
	"github.com/MoonlightByte/firelight/core"
	"reflect"
	"testing"
)

// This bounded decoder probe complements the fixed corrupt-history oracles.
// It does not equate fuzz volume with valid history or durable storage.
func FuzzNativeRead(f *testing.F) {
	b := saveTestBlueprint()
	b.Adventure = &core.Adventure{Definition: core.AdventureDefinition{Party: []core.CharacterID{"hero"}, Health: "hp", Seed: [2]uint64{1, 2}}}
	w, err := core.NewWorld(b)
	if err != nil {
		f.Fatal(err)
	}
	var seed bytes.Buffer
	if _, err := Write(&seed, w.ExportCheckpoint(), SessionState{Campaign: "fuzz", Next: 1}); err != nil {
		f.Fatal(err)
	}
	f.Add(seed.Bytes())
	f.Add([]byte("{}\n"))
	f.Add([]byte{0xff, '\n'})
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 64*1024 {
			return
		}
		loaded, err := Read(bytes.NewReader(data))
		if err != nil {
			if loaded != nil {
				t.Fatal("failed archive exposed a partially reconstructed World")
			}
			return
		}
		before := loaded.World.ExportCheckpoint()
		var encoded bytes.Buffer
		if _, err := Write(&encoded, before, loaded.Session); err != nil {
			t.Fatal("accepted archive cannot be encoded", err)
		}
		again, err := Read(&encoded)
		if err != nil || !reflect.DeepEqual(before, again.World.ExportCheckpoint()) || !reflect.DeepEqual(loaded.Session, again.Session) {
			t.Fatal("accepted archive lost coherent truth on roundtrip", err)
		}
	})
}
