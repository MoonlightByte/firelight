package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// pageWorld returns an Equipment world of pages whose genesis encodes to
// about size bytes.
func pageWorld(size int) Blueprint {
	b := Blueprint{
		Rules:      Rules{Transfer: Unequips, Wear: WearAny},
		Locations:  []Location{{ID: "keep", Name: "Keep"}},
		Characters: []Character{{ID: "a", Name: "A", Location: "keep", Stats: map[StatID]int64{}}},
		Equipment:  &EquipmentRules{Version: EquipmentVersion},
	}
	for n := 0; n < size/4096; n++ {
		b.Items = append(b.Items, Item{ID: ItemID(fmt.Sprintf("item:%05d", n)), Name: "Page", Owner: "a", Description: strings.Repeat("d", 4000)})
	}
	return b
}

// pad lengthens item descriptions by n bytes in total.
func pad(t *testing.T, b *Blueprint, n int) {
	t.Helper()
	for i := range b.Items {
		add := min(n, 4096-len(b.Items[i].Description))
		b.Items[i].Description += strings.Repeat("d", add)
		n -= add
	}
	if n != 0 {
		t.Fatal("no room to pad")
	}
}

// A world near its entity caps fits one genesis record of 16 MiB; batch
// records keep the 1 MiB bound (docs/WORLD_SIZE.md).
func TestGenesisRecordBound(t *testing.T) {
	limit := 16<<20 - 1024 // the documented bound, not the constant
	if limit != MaxGenesisRecordBytes-1024 {
		t.Fatal("genesis record bound differs from docs/WORLD_SIZE.md", MaxGenesisRecordBytes)
	}
	b := pageWorld(limit)
	if limit <= MaxArchiveRecordBytes || len(b.Items) >= MaxWorldItems {
		t.Fatal("bound fixture is not above the batch record bound or is above the item cap", len(b.Items))
	}
	w, err := NewWorld(b)
	if err != nil {
		t.Fatal(err)
	}
	// NewWorld measures its normalized copy; pad to exactly the bound there.
	pad(t, &b, limit-int(w.authority.archiveBytes-archiveReserve))
	if w, err = NewWorld(b); err != nil || w.authority.archiveBytes != uint64(limit)+archiveReserve {
		t.Fatal("exact genesis bound refused", err)
	}
	// A save measures the exported genesis; it must be the bytes admitted here.
	if raw, err := json.Marshal(w.ExportCheckpoint().Genesis); err != nil || len(raw) != limit {
		t.Fatal("exported genesis differs from the admitted bytes", len(raw), err)
	}
	pad(t, &b, 1)
	_, err = NewWorld(b)
	var f *Fault
	if !errors.As(err, &f) || f.Code != "E_ADVENTURE" || f.Field != "genesis bytes" {
		t.Fatal("genesis one byte over the bound accepted", err)
	}
}

// The index answers every reference as the scans do, including IDs repeated
// across kinds or within one, which Validate refuses but callers still ask
// about: characters win over items over locations, and the first declaration
// within a kind wins.
func TestRefsMatchScans(t *testing.T) {
	at := func(k EntityKind, id string) *Ref { return &Ref{k, id} }
	b := Blueprint{
		Characters: []Character{{ID: "x"}, {ID: "c"}, {ID: "c", Name: "second"}},
		Items: []Item{
			{ID: "x"}, {ID: "i"}, {ID: "y"},
			{ID: "box", Container: true, Custody: at(LocationKind, "l")}, {ID: "box"},
			{ID: "held", Container: true, Custody: at(CharacterKind, "c")}, {ID: "loose", Container: true},
			{ID: "lost", Container: true, Custody: at(LocationKind, "nowhere")},
			{ID: "in-box", Custody: at(ItemKind, "box")}, {ID: "in-held", Custody: at(ItemKind, "held")},
			{ID: "worn", Wearer: "c", Custody: at(LocationKind, "l")},
			{ID: "s0"}, {ID: "s1", SplitFrom: "s0"}, {ID: "s2", SplitFrom: "s1"},
			{ID: "k1", SplitFrom: "k2"}, {ID: "k2", SplitFrom: "k1"},
			{ID: "to-char", SplitFrom: "c"}, {ID: "to-shared", SplitFrom: "x"}, {ID: "to-none", SplitFrom: "none"},
		},
		Locations: []Location{{ID: "y"}, {ID: "l"}, {ID: "x"}},
	}
	refs := NewRefs(&b)
	for _, id := range []string{"x", "c", "i", "y", "box", "held", "loose", "lost", "l", "none", ""} {
		k1, ok1 := b.KindOf(id)
		k2, ok2 := refs.KindOf(id)
		if k1 != k2 || ok1 != ok2 {
			t.Fatalf("KindOf %q: scan %q %v, index %q %v", id, k1, ok1, k2, ok2)
		}
		for _, k := range []EntityKind{CharacterKind, ItemKind, LocationKind, "other"} {
			r := Ref{k, id}
			if a, z := b.CheckRef(r), refs.CheckRef(r); !reflect.DeepEqual(a, z) {
				t.Fatalf("CheckRef %v: scan %v, index %v", r, a, z)
			}
			if a, z := b.CheckCustodyRef(r), refs.CheckCustodyRef(r); !reflect.DeepEqual(a, z) {
				t.Fatalf("CheckCustodyRef %v: scan %v, index %v", r, a, z)
			}
		}
	}
	for _, i := range b.Items {
		if a, z := b.checkItemCustody(i), checkItemCustody(i, refs.CheckCustodyRef); !reflect.DeepEqual(a, z) {
			t.Fatalf("custody %s: scan %v, index %v", i.ID, a, z)
		}
		if a, z := b.checkItemLineage(i), checkItemLineage(i, refs.KindOf, refs.item); !reflect.DeepEqual(a, z) {
			t.Fatalf("lineage %s: scan %v, index %v", i.ID, a, z)
		}
	}
	// Agreement alone would pass if both lost a check: a container is resolved
	// through to its location.
	var f *Fault
	if err := refs.CheckCustodyRef(Ref{ItemKind, "lost"}); !errors.As(err, &f) || f.Code != "E_REFERENCE" || f.Subject != "nowhere" {
		t.Fatal("container at a missing location accepted", err)
	}
}

// Validate refuses an item that names a missing entity, through the index, and
// a cycle of stock ancestry.
func TestValidateRefusesMissingItemReferences(t *testing.T) {
	base := func() Blueprint {
		return Blueprint{
			Rules:      Rules{Transfer: Unequips, Wear: WearAny},
			Locations:  []Location{{ID: "keep", Name: "Keep"}},
			Characters: []Character{{ID: "a", Name: "A", Location: "keep", Stats: map[StatID]int64{}}},
			Items: []Item{
				{ID: "box", Name: "Box", Container: true, Custody: &Ref{LocationKind, "keep"}},
				{ID: "gem", Name: "Gem", Owner: "a"},
				{ID: "coin", Name: "Coin", Owner: "a"},
			},
		}
	}
	if err := Validate(base()); err != nil {
		t.Fatal("base world refused", err)
	}
	for _, tc := range []struct {
		name, subject string
		edit          func(b *Blueprint)
	}{
		{"owner", "ghost", func(b *Blueprint) { b.Items[1].Owner = "ghost" }},
		{"wearer", "ghost", func(b *Blueprint) { b.Items[1].Wearer = "ghost" }},
		{"custody", "ghost", func(b *Blueprint) { b.Items[1].Custody = &Ref{CharacterKind, "ghost"} }},
		{"container", "ghost", func(b *Blueprint) { b.Items[1].Custody = &Ref{ItemKind, "ghost"} }},
		{"split_from", "ghost", func(b *Blueprint) { b.Items[1].SplitFrom = "ghost" }},
		{"cycle", "gem", func(b *Blueprint) { b.Items[1].SplitFrom, b.Items[2].SplitFrom = "coin", "gem" }},
	} {
		b := base()
		tc.edit(&b)
		var f *Fault
		if err := Validate(b); !errors.As(err, &f) || f.Code != "E_REFERENCE" || f.Subject != tc.subject {
			t.Fatalf("%s: %v", tc.name, err)
		}
	}
}
