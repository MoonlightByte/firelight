# Live state: occupants, the map, quests and the journal between calls

A host that keeps the checkpoint nql-apply returns keeps everything. Some
hosts write a fresh world source for each call instead, because they keep
records of their own, such as character sheets, and write them into the
source. Such a host passes `live_state`: a document holding what only the
engine writes. Version 5 holds:

- the occupants (docs/LOCATIONS.md);
- each quest's status and log (docs/QUESTS.md);
- the journal (docs/JOURNAL.md);
- in a world with a map (docs/WORLD_MAP.md), where each character stands
  and the map state play changes: the party, the visited places, the
  declared hazards and the closed routes;
- in a world with a clock, the clock.

The world source then declares the static world and the seeds: every place,
the routes, the characters with their sheets, the authored occupants, and
the quests with their requirements. The document holds the occupants as
play left them: counts, tallies with their reasons, moves, attitudes and
names. It holds each quest's status and log as quest actions left them.
With a map it also holds where travel and moves left the characters, and
what map edits changed. It holds the time as trips and `advance time` left
it, and every journal entry recorded. The host never writes any of these
back into the source.

The files:

- `livestate/`: the document, its checks and the merge.
- `cmd/firelight-apply/livestate_test.go`: every rule below. It also runs
  thousands of random requests twice, once through `live_state` and once on
  a kept checkpoint, and checks that every answer, event and place is the
  same.

## Facts and totals

| The host controls | The engine derives |
| --- | --- |
| the world source: places, routes, characters and their sheets, authored occupants, quests and their requirements | which places, quests and characters the document holds |
| storing the document whole and passing it back | the occupants, the quests' status and logs, the journal, and with a map the characters' places and the map state, as play left them |
| the clock's unit, and its tick in a document's first call | the time, as trips and `advance time` pass it |
| request IDs | whether a request already applied |

## Starting

`"new_live_state": true`, with a world source, starts a document: every
place the world declares is taken with its seeds, and every quest as the
source states it. With a map, every
character is taken where the source puts it, with the source's party,
visited places, hazards and closures. With a clock, the clock is taken as
the source declares it. From the occupants example
(`examples/occupants/world.nql`), which has a map:

```json
{
  "format": "nql-live-state",
  "version": 5,
  "revision": 0,
  "places": ["loc:valley/B01", "loc:valley/B02", "loc:valley/B03", "loc:valley/B04"],
  "occupants": [
    {"id": "occ:valley/B02/mira", "name": "Ranger Mira", "location": "loc:valley/B02",
     "kind": "person", "count": 1, "attitude": "friendly", "aliases": ["Mira"]},
    {"id": "occ:valley/B03/goblins", "name": "Goblins", "location": "loc:valley/B03",
     "kind": "creatures", "range": {"min": 2, "max": 4}, "attitude": "hostile", "type": "goblin"}
  ],
  "characters": [
    {"id": "char:ada", "location": "loc:valley/B01"},
    {"id": "char:bram", "location": "loc:valley/B01"}
  ],
  "map": {
    "party": ["char:ada", "char:bram"],
    "visited": ["loc:valley/B01", "loc:valley/B02", "loc:valley/B03"],
    "hazards": [],
    "closed": []
  },
  "clock": {"unit": "second", "tick": 28800},
  "requests": [],
  "checksum": "sha256:e68fba8a4742a71e67cb5c79a7d9e197a8c4ca6169f7dfe2b6e8339e00d5c6e6"
}
```

A host creates the document when it has none, so a new game needs no
setup step.

## Each call

The host passes the document back as `"live_state"`, with the world source.

**Places:**

- A place the document holds takes its occupants from the document. The
  source's seeds for it are ignored for good, so renaming, recounting or
  removing a seed changes nothing in play.
- A place the document does not hold yet, such as one in a newly installed
  module, takes its seeds, and the document holds it from then on.
- A place is taken whole, never one occupant at a time, so a renamed seed
  cannot duplicate a group already in play.
- Seed a module only once its place IDs are final. With a map, its places'
  hazards and closures are taken with them (below).

**Quests:**

- A quest the document holds takes its status and log from the document.
  The log the source states for it is ignored for good.
- A quest the document does not hold yet, such as one in a newly installed
  module, takes the log the source states, unstarted without one, and the
  document holds it from then on. Declaring a campaign's quests in play
  once, by their logs, is how a host converts them.
- A held quest the world no longer declares, such as one in a module the
  host does not declare this call, stays in the document unchanged. The
  engine ignores it: an action or a view naming it is refused as unknown
  until the world declares it again.
- The names, `requires` and `side of` always come from the source.

**Characters and the map,** in a world with a map:

- A character the document holds stands where the document says. The
  source's `at` is ignored.
- A character the document does not hold yet, such as a new companion,
  stands where the source says, and the document holds it from then on.
- A held character the world no longer declares is dropped, unless it is
  in the party (Refusals).
- The document's party, visited places, hazards and closures replace the
  source's.
- A closure of a route the world does not declare in a call, such as a
  route a host declares only in the requests that use it, is held: the
  document keeps it unchanged, and it applies again when the world declares
  the route again. A request that closes or opens a route, or its retry,
  is compiled against the world, so it declares the route.
- A place the document does not hold yet, such as one in a newly installed
  module, also takes the source's `hazard` on it and each `closed` route
  that touches it, the route from an older place included. Play cannot
  have changed them yet. From then on the document holds them, and the
  source's lines for that place are ignored, as its seeds are. Its
  `visited` line is not taken: a place is visited by travel. In the call
  that introduces the place, marking the same hazard or closing the same
  route is answered `E_NO_CHANGE`.
- A world without a map leaves characters and the map to the source, as
  before. A world that declares a map for the first time takes them from
  the source once.
- A world that stops declaring the map, such as an older host's, keeps the
  held characters and map state in the document, unchanged, and ignores
  them: its characters stand where its source says, and their moves are not
  recorded. When a world declares the map again, the held state comes back
  as it was. A held party member such a world does not declare is held too.

**The clock,** in a world with one:

- A document that holds a clock runs the world at its tick. The tick the
  source declares is ignored, so a host may declare any tick, `at 0`
  included, and never writes the time back.
- A condition the source declares for `ticks N` runs N ticks from the
  document's tick, as the source counted them from its own. A host writes
  each timed effect with the ticks it has left: its expiry less the
  document's tick. Status shows such an effect's `applied_tick` as the
  document's tick in each call, as a fresh world always did; its
  `expires_tick` stays put. No receipt names `applied_tick`. A source
  without a clock cannot declare `ticks` (E_CLOCK at compile).
- A source clock in another unit is refused (Refusals): the engine never
  converts ticks.
- A document that does not hold a clock yet, such as one from an engine
  before version 4, takes the source's clock once, tick included, and holds
  it from then on. The first call with such a document must declare the
  real time.
- A world without a clock leaves the held one unchanged, and it applies
  again when the world declares a clock in the same unit.
- The clock never moves back. To correct it, advance it with `advance
  time`; a host that must go back starts a new document (below).

**The journal:**

- A document that holds a journal runs the world with it. A journal the
  source declares is ignored, so a host never declares one beside a
  document.
- A document that does not hold one yet, such as one from an engine before
  version 5, takes the source's once, none for most hosts, and holds it
  from then on. Declaring a campaign's entries once is how a host converts
  them (docs/JOURNAL.md).
- The places and people an entry names are history: they are not checked
  against the world, so a module the host stops declaring keeps its
  entries.
- An entry's ID stays taken while the entry is held, so the world cannot
  declare a character, item, place, occupant or quest with it (Refusals).

The routes and the sheets always come from the source.

The response carries the next `live_state` on every ok response,
`view_error` included. A refused call carries none, and the host keeps the
copy it has.

After the fight in `examples/occupants/fight.nql`, sent as request
`turn:12`:

```json
{
  "format": "nql-live-state",
  "version": 5,
  "revision": 1,
  "places": ["loc:valley/B01", "loc:valley/B02", "loc:valley/B03", "loc:valley/B04"],
  "occupants": [
    {"id": "occ:valley/B02/mira", "name": "Ranger Mira", "location": "loc:valley/B02",
     "kind": "person", "count": 1, "attitude": "friendly", "aliases": ["Mira"]},
    {"id": "occ:valley/B03/goblins", "name": "Goblins", "location": "loc:valley/B03",
     "kind": "creatures", "count": 0, "attitude": "hostile", "type": "goblin",
     "tally": [{"outcome": "defeated", "count": 2}, {"outcome": "fled", "count": 1}]}
  ],
  "characters": [
    {"id": "char:ada", "location": "loc:valley/B01"},
    {"id": "char:bram", "location": "loc:valley/B01"}
  ],
  "map": {
    "party": ["char:ada", "char:bram"],
    "visited": ["loc:valley/B01", "loc:valley/B02", "loc:valley/B03"],
    "hazards": [],
    "closed": []
  },
  "clock": {"unit": "second", "tick": 28800},
  "requests": [
    {"id": "turn:12", "revision": 1, "digest": "7ea7f3bd8e3f92291d0748494c71ce88316350305deede8d54f19d72d5122a9d"}
  ],
  "checksum": "sha256:3b16d6b65a0eb56a70e42a07f4d288691af7ed57438504cd721065527c30cd19"
}
```

With the goblins resolved, the mill no longer stops a trip. The trip in
`examples/occupants/trip.nql`, `travel party to "loc:valley/B04";`, sent
as request `turn:13`, takes 1,200 seconds through the outpost and the mill.
The document then holds both characters at the watchtower and the
watchtower visited:

```json
{
  "format": "nql-live-state",
  "version": 5,
  "revision": 2,
  "places": ["loc:valley/B01", "loc:valley/B02", "loc:valley/B03", "loc:valley/B04"],
  "occupants": [
    {"id": "occ:valley/B02/mira", "name": "Ranger Mira", "location": "loc:valley/B02",
     "kind": "person", "count": 1, "attitude": "friendly", "aliases": ["Mira"]},
    {"id": "occ:valley/B03/goblins", "name": "Goblins", "location": "loc:valley/B03",
     "kind": "creatures", "count": 0, "attitude": "hostile", "type": "goblin",
     "tally": [{"outcome": "defeated", "count": 2}, {"outcome": "fled", "count": 1}]}
  ],
  "characters": [
    {"id": "char:ada", "location": "loc:valley/B04"},
    {"id": "char:bram", "location": "loc:valley/B04"}
  ],
  "map": {
    "party": ["char:ada", "char:bram"],
    "visited": ["loc:valley/B01", "loc:valley/B02", "loc:valley/B03", "loc:valley/B04"],
    "hazards": [],
    "closed": []
  },
  "clock": {"unit": "second", "tick": 30000},
  "requests": [
    {"id": "turn:12", "revision": 1, "digest": "7ea7f3bd8e3f92291d0748494c71ce88316350305deede8d54f19d72d5122a9d"},
    {"id": "turn:13", "revision": 2, "digest": "3699c13d9c9cdb0058f3bf5c08a750ac8f487df5f9eef5805e347ee2169a4987"}
  ],
  "checksum": "sha256:0e37676e99810c715eeee1121b39ca2c17584879271d2b617b9ee1cb0143705e"
}
```

The document's clock says 30,000, as the receipt's `TimeAdvanced` does.
The host sends the source's `clock "second" at 28800;` unchanged on the next
call: its tick is ignored.

From the quest example (`examples/quests/world.nql`), which has no map,
after `examples/quests/start.nql` sent as request `turn:1`. The herbs are
as the source states them; the mill and the tower were started by the
request:

```json
{
  "format": "nql-live-state",
  "version": 5,
  "revision": 1,
  "places": ["loc:valley/B01", "loc:valley/B02", "loc:valley/B03", "loc:valley/B04"],
  "occupants": [],
  "quests": [
    {"id": "quest:valley/herbs", "status": "started", "log": [
      {"kind": "started", "text": "Mira needs feverfew from the riverbank for her fever", "tick": 27000}]},
    {"id": "quest:valley/mill", "status": "started", "log": [
      {"kind": "started", "text": "Ranger Mira asked the party to drive the goblins from the mill", "tick": 28800}]},
    {"id": "quest:valley/tower", "status": "started", "log": [
      {"kind": "started", "text": "the watch must hear of the raids before nightfall", "tick": 28800}]}
  ],
  "clock": {"unit": "second", "tick": 28800},
  "requests": [
    {"id": "turn:1", "revision": 1, "digest": "5edc16aeb41614afda13dce2287c8f4d474a9a16e724008949f1d0cb102a9833"}
  ],
  "checksum": "sha256:45f398c8d96278dead33b436fffe80abb83836cededd9be3b7874da4a9b83cae"
}
```

| Field | Meaning |
| --- | --- |
| `format`, `version` | `nql-live-state`, 5 |
| `revision` | how many requests have changed what the document holds |
| `places` | the places the document holds, sorted |
| `occupants` | every occupant record, sorted by ID, each at one of those places |
| `characters` | with a map, or held from one: where each character stands, sorted by ID |
| `map` | with a map, or held from one: `party`, `visited`, `hazards` and `closed`, each sorted |
| `quests` | each held quest's `status` and `log`, sorted by ID; absent with none |
| `clock` | the world's `unit` and `tick` after the last call; absent without a clock |
| `journal` | `next` and the `entries`, in seq order (docs/JOURNAL.md); absent until one is recorded or declared |
| `requests` | the last 64 requests that changed what the document holds, oldest first |
| `checksum` | SHA-256 of the document without it |

The checksum covers the content, not the bytes: a host may store the
document with any JSON encoder.

## Retries

A request that changes what the document holds is kept, with its ID, the
revision it made and a digest of its actor and compiled operations. That
means an occupant action, a quest action or a journal entry, with a map also a trip, a
move or a map edit, and with a clock anything that passes time, such as
`advance time`. The last 64 are kept.

- **The same ID with the same content** is answered as already applied:
  ok, phase `executed`, `historical` true, no receipt, the views of now, and
  the document unchanged. A retry after a crash never applies twice. This
  holds for a request that created something: the occupants it created are
  in the document, and the items in the host's source, and they are not
  refused as duplicates.
- **The same ID with other content,** or from another actor, is refused:
  E_REQUEST_REUSE, as the engine refuses it on a kept checkpoint.
- **A request that changes nothing the document holds** is not kept. The
  document comes back unchanged, checksum included, so a host may skip
  writing it. A request that only creates items is one: retried after the
  host wrote the items into its source, it is refused at compile with
  E_DUPLICATE at the item's ID, and nothing is applied.
- **An ID older than the last 64** is no longer known, and the request runs
  again.

A request is kept by what it did, not by where it ended. A request that
changes an occupant and changes it back is still kept, as a kept world
keeps it.

## The host's write order

A request answered as already applied runs nothing and has no receipt. So
anything the host writes from a receipt must be written once, whatever
happens between its writes:

- a sheet, when a timed condition runs out on the way;
- anything else the same request changed on a sheet.

Write each response in three steps:

1. Write the whole response, receipt and next document, to a pending file,
   atomically.
2. Apply the receipt to the host's own records, writing absolute values
   (the events' `after`), then replace the document.
3. Delete the pending file.

On restart, a pending file is applied again from step 2. Absolute values
make that safe. A host that cannot do this keeps occupant actions, quest
actions, trips, moves and map edits in requests of their own, apart from
sheet changes.
A trip or a wait cannot be split from the conditions that run out during
it, so a host with timed conditions needs the pending file.

## Refusals

A refused document fails the call in phase `live_state`, with nothing
applied. A host can test each refusal offline from this table:

| Code | Field | Subject | Expected | Actual | Means |
| --- | --- | --- | --- | --- | --- |
| E_LIVE_STATE | `format` | | `nql-live-state` | the format | not a live state |
| E_LIVE_STATE | `version` | | `1 to 5` | the version | a version this engine does not read |
| E_LIVE_STATE | `checksum` | | `the checksum the engine wrote` | `absent` or `mismatch` | the document was changed outside the engine |
| E_LIVE_STATE | `place` | the place | `a place the world declares` | `not declared` | a place it holds that the world no longer declares |
| E_LIVE_STATE | `party` | the member | `a character the world declares` | `not declared` | a party member a world with the map no longer declares |
| E_LIVE_STATE | `clock` | | `unit` and the held unit | `unit` and the source's | the source's clock counts in another unit |
| E_LIVE_STATE | `clock` | the condition | `a lifetime that ends after the held tick` | `overflow` | a condition's ticks run past the last tick from the held one |
| E_QUEST | `ahead` | the held quest | `a declared quest` | the quest | a held log names, as completed ahead of it, a quest the world no longer declares |
| E_DUPLICATE | `journal` | the entry | `unique entry ID` | the ID | the world declares a character, item, place, occupant or quest with a held entry's ID |
| E_LIVE_STATE | `places`, `occupants`, `characters`, `location`, `party`, `visited`, `hazards`, `closed`, `quests`, `clock`, `journal`, `revision`, `requests`, `request`, `digest`, `version` | | | | the document's own order and bounds |
| E_OCCUPANT, E_DUPLICATE | as for occupants | | | | a record the world refuses, or a new place's seed with an ID already in play |

The rows marked as the document's own order and bounds can only come from
a document changed outside the engine with its checksum recomputed.

When the source alone is refused, the fault is the source's: phase
`compile` or `genesis`, as without live state. `live_state` together with
`new_live_state`, or either one with a checkpoint or without a world, is an
`input` error. A field the engine does not write is not a document (exit 2,
phase `input`).

## When the document is refused

The checksum detects damage; it is never the identity of anything. Never
edit the document by hand; change occupants and the map with actions. If a
document is refused, the host steps down so play never stops:

1. A refusal naming the world (`place`, `party`, `clock` with a unit,
   E_QUEST `ahead`, or E_DUPLICATE `journal`) means the source dropped or
   changed something the document holds or names. Declare it again, or
   rename what now takes a held entry's ID, and retry with the same
   document.
2. Otherwise, write each new document atomically, keeping the previous
   one, and retry with the previous one.
3. If that is refused too, set the file aside and start again with
   `new_live_state`: every occupant goes back to its seed, and the quests,
   the characters, the map, the clock and the journal to the source.
4. Report what was lost.

## Versions

- This engine reads versions 1 to 5, and writes version 5. A version 1 to
  4 document loads as it is: what it holds is unchanged, and the source
  gives the rest once: the journal, for versions 1 to 3 the clock, for
  versions 1 and 2 the quests, and for version 1 the characters and the
  map. The response carries version 5.
- An older engine refuses a newer document: `version`, or an unknown field
  when the document holds what the older version cannot, such as a map,
  quests, a clock or a journal. Never run an older engine on a newer
  document.
- Without `live_state`, nothing changes.

## Limits

- The occupant limits hold (docs/LOCATIONS.md): at most 32,768 occupants per
  world, resolved ones included.
- The quest limits hold (docs/QUESTS.md) for the quests the world
  declares. Held quests the world no longer declares stay in the document
  besides, a few hundred bytes each.
- At most 64 recent requests.
- The journal limits hold (docs/JOURNAL.md): at most 1,024 entries, about
  400 KB at typical sizes.
- Closures held for routes the world does not declare stay in the document
  besides, a few dozen bytes each, until a request opens them.
- Version 5 holds the occupants, the quests' status and logs, the clock,
  the journal and, with a map, the characters' places and the map state. The routes,
  the sheets, the timed effects and the quests' names and requirements
  come from the source on every call. Hazards from hostile occupants come
  from the document's occupants. A document of an older version takes the
  new parts from the source once.
