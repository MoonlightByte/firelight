# The journal

The journal is the world's record of what happened where: the party set
out, arrived, found something, or the agent noted an event. The engine
keeps each entry as a typed record, atomic with the request that made it,
so a trip and its departure entry commit together or not at all. The host
keeps the prose, keyed by the entry's ID.

"Agent" means any caller that runs the game: a DM model, a script, a menu.
The agent decides what is worth an entry and says it in its own words. The
engine numbers the entry, stamps the time and the place, checks the names,
and keeps the history.

The files:

- `examples/occupants/world.nql`: the valley of the map guide. Ada and Bram
  are the party, at the trailhead at tick 28,800.
- `examples/journal/trip.nql`: the party sets out for the watchtower and
  is stopped at the old mill.
- `tests/journal_test.go`: every rule below, at its bounds.
- `cmd/firelight-apply/livestate_journal_test.go`: the host flow, and thousands
  of random requests run through `live_state` and on a kept checkpoint,
  with the same answers, events, views and journal, seq included.

## Facts and totals

| The agent or host controls | The engine derives |
| --- | --- |
| that an entry is recorded, its ID and kind | its seq, which never repeats |
| the place, when it names one | the place otherwise: where the actor stands at that point |
| the people it names, or `party` | the party at that point; which names are declared characters or occupants |
| the text, in its own words | the clock tick at that point in the request |
| | which entry a full journal drops |

## Recording

From `trip.nql`:

```nql
record journal "journal:valley/0001" departure with party "occ:valley/B01/peddler" text "Set out for the watchtower before dawn; the peddler stayed behind.";
travel party to "loc:valley/B04";
record journal "journal:valley/0002" arrival with party text "Goblins at the old mill stopped us short of the watchtower.";
```

`record journal "<id>" <kind> [at "<place>"] [with [party] ["<id>" ...]]
[text "<text>"];` adds one entry:

- **id:** the host's. It shares the namespace of characters, items, places,
  occupants and quests, so it is never reused while the entry is held.
  Mint IDs that never repeat, such as a counter per campaign.
- **kind:** `departure`, `arrival`, `event`, `discovery` or `note`. The
  engine never reads it for rules.
- **seq:** one more than the last entry ever recorded in this world. It
  never repeats, even after a drop, and it is the paging key.
- **tick:** the world clock at that point in the request. Action order
  sets it: the departure above has the tick before the trip, the arrival
  the tick after it. A world without a clock records none.
- **place:** the place named, which must be declared. Without one, where
  the actor stands at that point: after the trip above, the old mill, where
  the goblins stopped the party. An actor that stands nowhere, such as an
  item, has no place, and the entry is refused; a host whose requests run
  with such an actor names `at` on every entry.
- **participants:** `party` names every party member at that point. Each
  listed ID that is a declared character or occupant is a participant.
  **Any other ID is left out, never refused**, and the event names it in
  `dropped_participants`: a roster that drifted from the world never stops
  a trip. An entry has at most 16 participants: the party first, then the
  listed names in the order given; those past the 16th are left out and
  named the same way. At most 16 names may be listed. The participants are
  sorted.
- **text:** one line of at most 512 bytes, not blank. A newline, a tab or
  any other control character is refused (`control character`), so flatten
  the prose first, as quest texts. The engine keeps the text and never
  reads it. Cut a longer text by UTF-8 bytes at a character boundary,
  never by characters: a cut inside a character arrives as U+FFFD, 3 bytes,
  and can push the text over the bound.

The receipt holds one `JournalRecorded` event per entry. The departure's
`journal`:

```json
{
  "intent": {"id": "journal:valley/0001", "kind": "departure", "party": true,
             "with": ["occ:valley/B01/peddler"],
             "text": "Set out for the watchtower before dawn; the peddler stayed behind."},
  "entry": {"seq": 1, "id": "journal:valley/0001", "kind": "departure", "tick": 28800,
            "place": "loc:valley/B01", "participants": ["char:ada", "char:bram"],
            "text": "Set out for the watchtower before dawn; the peddler stayed behind."},
  "dropped_participants": ["occ:valley/B01/peddler"]
}
```

The peddler is not declared, so the entry leaves them out and says so.

## A full journal

A journal holds at most 1,024 entries. A new entry then drops the oldest,
and the event's `dropped` holds it. The host has the dropped record in the
receipt and keeps its prose anyway. A dropped entry's ID is free again,
which is why IDs should never repeat.

## Refusals

Every refusal applies nothing of the request.

| Code | Field | Expected | Actual | Means |
| --- | --- | --- | --- | --- |
| E_DUPLICATE | `journal` | `unique entry ID` | the ID | the ID is taken: by an entry, or by a character, item, place, occupant or quest |
| E_REFERENCE, E_TYPE | `reference` | `location` | `missing`, or the kind | the place named is not a declared place |
| E_JOURNAL | `place` | `a place, or an actor standing at one` | the actor's kind | no place named, and the actor stands nowhere |
| E_JOURNAL | `party` | `a world map` | `absent` | `party` in a world without a map, at compile |
| E_JOURNAL | `participants` | `at most 16 participants` | the count | more than 16 names listed, at compile |
| E_JOURNAL | `text` | `valid text of at most 512 bytes` | `control character`, `invalid`, or its length | a newline, tab or other control character; a blank text; or one over 512 bytes |
| E_JOURNAL | `id` | `valid entry ID` | `invalid` | a blank ID, at compile |
| E_JOURNAL | `kind` | the five kinds | the kind | an unknown kind handed to the engine directly; NQL refuses it at the token (E_PARSE) |
| E_OVERFLOW | `journal` | `an unused seq` | `maximum` | the last seq is taken; it cannot happen in play |

An undeclared place, a blank text or one over the bound, and more than 16
listed are refused at compile, at the token.

## Reading the journal

nql-apply takes `"journal": {...}` with any of these filters, all of which
must match:

| Filter | Matches |
| --- | --- |
| `"id": ["<id>", ...]` | those entries |
| `"after": <seq>` | entries with a higher seq |
| `"since": <tick>` | entries at that tick or later; an entry without a tick never matches |
| `"place": "<place>"` | entries at that place |
| `"participant": "<id>"` | entries naming that character or occupant |
| `"kind": ["<kind>", ...]` | entries of those kinds |
| `"limit": <n>` | the first n, 1 to 1,024; 0 or absent is no limit |

It returns the matching entries in seq order, with `next`, the seq the next
entry will take, read at one revision. `"journal": {}` returns all of them.
To page, pass `limit` and then `after` the last seq returned. After
`trip.nql`, `{"participant": "char:bram"}`:

```json
{
  "revision": 1,
  "next": 3,
  "entries": [
    {"seq": 1, "id": "journal:valley/0001", "kind": "departure", "tick": 28800,
     "place": "loc:valley/B01", "participants": ["char:ada", "char:bram"],
     "text": "Set out for the watchtower before dawn; the peddler stayed behind."},
    {"seq": 2, "id": "journal:valley/0002", "kind": "arrival", "tick": 29400,
     "place": "loc:valley/B03", "participants": ["char:ada", "char:bram"],
     "text": "Goblins at the old mill stopped us short of the watchtower."}
  ]
}
```

An unknown kind, or a limit outside 0 to 1,024, fails the view (E_JOURNAL
`kind` or `limit`).

## The trip and its entry

Put the departure entry in the trip's request, before `travel party to`,
and the arrival entry after it. Both commit with the trip or not at all.
Name the place with `at` whenever it is known, as the departure's is; a
derived place depends on the request's actor.

- **A retry the document keeps** (the last 64 requests,
  docs/LIVE_STATE.md) is answered as already applied: nothing runs twice.
- **A retry older than that** runs again, and its entry's ID is taken:
  the whole request is refused, E_DUPLICATE `journal` at that ID, and the
  trip does not apply twice. That refusal means the entry and its trip
  already applied. Read the view with that ID, `{"id": ["<id>"]}`, find
  the entry, and treat the trip as done. A view call never fails for it.

## Hosts that rebuild the world per call

A host that keeps the returned checkpoint keeps everything. A host that
writes a fresh world for each call passes `live_state` (docs/LIVE_STATE.md),
which holds the journal from version 5. It declares no journal and writes
nothing back: the document's journal wins over the source's. The places
and people an entry names are history, never checked against the world
again.

A host declares the journal in two cases: once, to convert a campaign's
entries when no document holds a journal yet; and on every call when it
passes no `live_state`, writing each new entry back from the receipt. An
event's `entry` maps one to one onto a declaration:

```nql
journal "journal:valley/0001" departure seq 1 at "loc:valley/B01" tick 28800 with "char:ada" "char:bram" text "Set out for the watchtower before dawn; the peddler stayed behind.";
journal next 3;
```

`tick`, `with` and `text` are written when the entry has them. Declare the
entries in seq order. `journal next <n>;` states the next seq; without it,
it is one past the last entry, so state it whenever entries were dropped.

## Hosts with their own journal files

| Host record | Engine |
| --- | --- |
| an entry identified by its position in a list | an entry with an ID that never repeats, paged by seq |
| a summary, rewritten or backfilled later | the text, kept once; the long prose stays with the host, keyed by ID |
| a departure written in a separate step after the trip | an entry in the trip's own request |
| a whole-journal copy kept to detect a half-done write | the request's atomicity; the view by ID after an old retry |

## Limits

- At most 1,024 entries, 16 participants per entry, 512 bytes of text.
  A full journal of typical entries is about 400 KB of document.
- `record journal` is not an Ability step.
- The engine keeps no prose beyond the text; the host keeps its own by
  entry ID.
