package livestate

import (
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/MoonlightByte/firelight/core"
)

// journaled is sealed with a held journal of two entries, after a drop.
func journaled() Document {
	d := sealed()
	tick := uint64(40)
	d.Journal = &core.Journal{Next: 8, Entries: []core.JournalEntry{
		{Seq: 5, ID: "journal:5", Kind: "departure", Tick: &tick, Place: "loc:camp", Participants: []string{"char:a", "occ:mira"}, Text: text("left at dawn")},
		{Seq: 7, ID: "journal:7", Kind: "arrival", Place: "loc:gone"},
	}}
	d.Checksum = checksum(d)
	return d
}

func TestCheckJournal(t *testing.T) {
	d := journaled()
	if err := Check(&d); err != nil {
		t.Fatal(err)
	}
	// The journal is part of the checksum.
	v := journaled()
	*v.Journal.Entries[0].Text = "left at dusk"
	wantFault(t, Check(&v), "E_LIVE_STATE", "checksum", "mismatch")

	refused := func(name string, edit func(*Document), field, actual string) {
		t.Helper()
		v := journaled()
		edit(&v)
		v.Checksum = checksum(v)
		var f *core.Fault
		if err := Check(&v); !errors.As(err, &f) || f.Code != "E_LIVE_STATE" || f.Field != field || f.Actual != actual {
			t.Fatalf("%s: got %#v, want %s %q", name, err, field, actual)
		}
	}
	refused("version 4 with a journal", func(v *Document) { v.Version = 4 }, "version", "4")
	refused("next 0", func(v *Document) { v.Journal.Next = 0 }, "journal", "0")
	refused("a seq at next", func(v *Document) { v.Journal.Next = 7 }, "journal", "7")
	refused("seq out of order", func(v *Document) { v.Journal.Entries[1].Seq = 5 }, "journal", "5")
	refused("seq 0", func(v *Document) { v.Journal.Entries[0].Seq = 0 }, "journal", "0")
	refused("an ID twice", func(v *Document) { v.Journal.Entries[1].ID = "journal:5" }, "journal", "journal:5")
	refused("a blank ID", func(v *Document) { v.Journal.Entries[1].ID = " " }, "journal", "invalid")
	refused("an unknown kind", func(v *Document) { v.Journal.Entries[1].Kind = "diary" }, "journal", "diary")
	refused("a blank place", func(v *Document) { v.Journal.Entries[1].Place = "" }, "journal", "invalid")
	refused("a participant twice", func(v *Document) { v.Journal.Entries[0].Participants[1] = "char:a" }, "journal", "char:a")
	refused("long text", func(v *Document) { *v.Journal.Entries[0].Text = strings.Repeat("t", 513) }, "journal", "513 bytes")
	refused("too many participants", func(v *Document) {
		v.Journal.Entries[0].Participants = nil
		for i := 0; i < 17; i++ {
			v.Journal.Entries[0].Participants = append(v.Journal.Entries[0].Participants, "char:"+strconv.Itoa(i))
		}
	}, "journal", "17")
	refused("too many entries", func(v *Document) {
		v.Journal.Entries = nil
		for i := 1; i <= core.MaxJournalEntries+1; i++ {
			v.Journal.Entries = append(v.Journal.Entries, core.JournalEntry{Seq: uint64(i), ID: core.JournalEntryID("j:" + strconv.Itoa(i)), Kind: "note", Place: "loc:camp"})
		}
		v.Journal.Next = core.MaxJournalEntries + 2
	}, "journal", "1025")

	// The version ladder: a version 5 document with no journal names none.
	raw, err := json.Marshal(sealed())
	if err != nil || strings.Contains(string(raw), `"journal"`) {
		t.Fatalf("no journal %s %v", raw, err)
	}
}

func TestMergeJournal(t *testing.T) {
	locations := []core.Location{{ID: "loc:camp"}, {ID: "loc:ford"}}
	source := &core.Journal{Next: 2, Entries: []core.JournalEntry{{Seq: 1, ID: "journal:src", Kind: "note", Place: "loc:camp"}}}

	// The held journal replaces the source's, a copy. A place it names that
	// the world no longer declares is history, not a refusal.
	d := journaled()
	b := core.Blueprint{Locations: locations, Journal: source}
	if err := Merge(&b, &d); err != nil || !reflect.DeepEqual(b.Journal, d.Journal) || b.Journal == d.Journal || &b.Journal.Entries[0] == &d.Journal.Entries[0] {
		t.Fatalf("held %v %+v", err, b.Journal)
	}
	b.Journal.Entries[0].Participants[0] = "x"
	*b.Journal.Entries[0].Text = "x"
	if d.Journal.Entries[0].Participants[0] != "char:a" || *d.Journal.Entries[0].Text != "left at dawn" {
		t.Fatal("the world aliases the document")
	}
	if source.Entries[0].ID != "journal:src" {
		t.Fatal("the source changed")
	}

	// A document that holds none takes the source's: old versions too.
	for _, v := range []Document{sealed(), {Version: 4}, {Version: 1}} {
		b = core.Blueprint{Locations: locations, Journal: source}
		if err := Merge(&b, &v); err != nil || b.Journal != source {
			t.Fatalf("version %d: %v %+v", v.Version, err, b.Journal)
		}
	}
}

func TestNextJournal(t *testing.T) {
	s := core.Snapshot{}
	s.Locations = []core.Location{{ID: "loc:camp"}, {ID: "loc:ford"}}
	s.Occupants = sealed().Occupants
	// The next document holds the world's journal, a copy.
	tick := uint64(50)
	s.Journal = &core.Journal{Next: 9, Entries: []core.JournalEntry{{Seq: 8, ID: "journal:8", Kind: "note", Tick: &tick, Place: "loc:ford", Participants: []string{"char:a"}, Text: text("x")}}}
	n := Next(journaled(), s, "", "")
	if !reflect.DeepEqual(n.Journal, s.Journal) || n.Version != Version || Check(&n) != nil {
		t.Fatalf("world journal %+v", n.Journal)
	}
	n.Journal.Entries[0].Participants[0], *n.Journal.Entries[0].Tick, *n.Journal.Entries[0].Text = "y", 1, "y"
	if s.Journal.Entries[0].Participants[0] != "char:a" || *s.Journal.Entries[0].Tick != 50 || *s.Journal.Entries[0].Text != "x" {
		t.Fatal("the document aliases the world")
	}
	// The first one comes from the world.
	n = Next(sealed(), s, "", "")
	if !reflect.DeepEqual(n.Journal, s.Journal) || Check(&n) != nil {
		t.Fatalf("first journal %+v", n.Journal)
	}
	// A world without one keeps the held one.
	s.Journal = nil
	d := journaled()
	n = Next(d, s, "", "")
	if !reflect.DeepEqual(n.Journal, d.Journal) || n.Checksum != d.Checksum {
		t.Fatalf("held journal %+v", n.Journal)
	}
	// Neither: none.
	if n := Next(sealed(), s, "", ""); n.Journal != nil {
		t.Fatalf("no journal %+v", n.Journal)
	}
}

func TestChangesJournal(t *testing.T) {
	b := core.Batch{Events: []core.Event{{Kind: "Other"}, {Kind: core.JournalRecorded, Journal: &core.JournalChange{}}}}
	if !Changes(b, false, false) {
		t.Fatal("a recorded entry is a change")
	}
}
