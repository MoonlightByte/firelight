# Location rosters: occupants

An occupant is someone or something at a place that is not a Character: a
person without a character sheet, or a group of creatures. The engine keeps
every occupant's place, how many are there, its attitude and how members
left. An agent changes them with typed actions, one fact at a time; it never
rewrites a place's roster.

"Agent" means any caller that runs the game: a DM model, a script, a menu.
The agent decides what happens: the goblins are defeated, the ranger walks to
the watchtower. The engine applies the change atomically, refuses one the
record does not allow and says why, and keeps the history.

The files:

- `examples/occupants/world.nql`: the valley of the map guide with goblins at
  the mill and a ranger at the outpost.
- `examples/occupants/fight.nql`: the goblins counted and resolved.
- `tests/occupants_test.go`: every number below, and the rules checked
  against a separate model on thousands of random requests.

## Facts and totals

| The agent or host controls | The engine derives |
| --- | --- |
| who and what is where, and its attitude | the members still present |
| how many a group turns out to be, within its authored range | that a count stays within it |
| how members leave: defeated, fled, surrendered, departed, joined, died; and why | the tally per outcome and reason, and that no more leave than are there |
| who comes back, from which entry | the members present again, and that no more come back than left |
| new names | the old names, kept as aliases |
| | that a place with a present hostile occupant stops a trip |

## Declaring occupants

From `world.nql`:

```nql
occupant "occ:valley/B03/goblins" named "Goblins" at "loc:valley/B03" { creatures 2 to 4; attitude hostile; type "goblin"; }
occupant "occ:valley/B02/mira" named "Ranger Mira" at "loc:valley/B02" { person; attitude friendly; alias "Mira"; }
```

- `person;` is one individual. `creatures <n>;` is a group of n.
  `creatures <min> to <max>;` is a group not yet counted, as an authored
  roster often gives it ("2d4 wolves", "2 to 4 goblins"). `creatures;` is a
  group whose number nobody has written down yet.
- `attitude` is `friendly`, `indifferent` (the default) or `hostile`.
- `type` says what it is, for the host's stat tables. The engine does not
  read it.
- `alias` lists other names the agent may use for it. At most 16.
- `tally <outcome> <n> [because "<reason>"];` records members who left, and
  why, so a host can declare an occupant in any state the engine can reach.
  A `departed` entry always says why.

Occupant IDs share the namespace of characters, items and places. Qualifying
them with the module and place, as above, keeps two modules' "Goblins" apart
and gives each authored entry a stable ID.

A world with occupants needs a clock. It needs no map; with one, hostile
occupants become hazards (below).

## Changing occupants

```nql
count occupant "occ:valley/B03/goblins" as 3;
resolve occupant "occ:valley/B03/goblins" as defeated by 2;
resolve occupant "occ:valley/B03/goblins" as fled;
```

The agent decides there were three goblins, two were killed and one ran. The
receipt holds `OccupantCounted` and two `OccupantResolved` events, each with
the `intent` and the record `before` and `after`. The goblins now have
`count` 0 and `tally` defeated 2, fled 1. They stay at the mill as a record,
so the next look at the mill shows what happened there instead of a fight
waiting to be fought again.

| Action | What it does |
| --- | --- |
| `create occupant "<id>" named "<name>" at "<place>" { ... };` | someone new arrives |
| `move occupant "<id>" to "<place>";` | the occupant goes elsewhere |
| `count occupant "<id>" as <n>;` | an uncounted group is counted, within its range if it has one |
| `resolve occupant "<id>" as <outcome> [by <n>] [because "<reason>"];` | n members leave, all present without `by`; a departure says why |
| `set occupant "<id>" attitude <attitude>;` | the attitude changes |
| `rename occupant "<id>" to "<name>";` | a new name; the old one becomes an alias |
| `return occupant "<id>" from <outcome> ["<reason>"] [by <n>];` | n members of one tally entry come back, the whole entry without `by` |

A person who joins the party for good becomes a Character: the agent creates
the character, adds it with `join party`, and resolves the occupant as
`joined`.

## Why they left, and coming back

Each tally entry is one outcome with one reason, the agent's own words. The
engine keeps the reason, shows it in the receipt and the view, and never
reads it. A reason is optional, except for `departed`: an outcome that says
nothing by itself about what happened always says why.

```nql
resolve occupant "occ:valley/B02/mira" as departed because "rode north to fetch the healer";
```

Mira's record now has `count` 0 and the tally entry `{"outcome": "departed",
"count": 1, "reason": "rode north to fetch the healer"}`. Two departures for
different reasons are two entries, so counts stay exact per reason: "2
departed: bribed to leave; 1 departed: scared off by the fire".

Someone who left can come back as the same occupant, with its ID, name,
aliases and the rest of its tally:

```nql
return occupant "occ:valley/B02/mira" from departed;
```

The receipt holds `OccupantReturned`. The member is present again at the
occupant's place; `move occupant` in the same request puts it elsewhere. A
request may name the entry by its reason, `from departed "bribed to leave"`;
without one it takes the outcome's entry that has no reason, or the outcome's
only entry, and is refused when that leaves a choice. Any outcome can be
reversed, `died` included, but only by naming it, so undoing a death is
always deliberate and on record. A hostile group that comes back makes its
place a hazard again.

Every action in a request commits together or not at all, and an occupant
created earlier in a request can be changed later in it.

## Refusals

Each is `E_OCCUPANT` with the reason in `actual`, or `E_NO_CHANGE`:

| Field | Actual | Meaning |
| --- | --- | --- |
| `occupant` | `missing` | no occupant has that ID |
| `move` | `resolved` | no one is left to move |
| `count` | `person`, `counted`, or the number | only an uncounted group is counted, within its range |
| `resolve` | `uncounted` | count the group first, in the same request if need be |
| `resolve` | `none` | no one is left |
| `resolve` | the number | more than are present |
| `aliases` | the number | a rename would pass 16 aliases |
| `create` | `resolved` | a new occupant has someone present and no tally |
| `reason` | `absent` | a departure without a reason |
| `reason` | `invalid`, or its length | a blank reason, or one over 200 bytes |
| `return` | `none` | no tally entry matches |
| `return` | `ambiguous` | the outcome has several entries; name one by its reason |
| `return` | the number | more than the entry holds |

`E_NO_CHANGE` answers a move to the same place, the same attitude or the same
name. `E_AMOUNT` answers `by 0`. A departure without a reason is refused when
the request is compiled, at `departed`.

## Hazards from hostile occupants

In a world with a map, a place where a hostile occupant is present is a
hazard: a trip stops there on its way, the map view marks it, and the
`halts_at` of a destination beyond it names it. An uncounted group is
present. When the occupant is resolved, or is no longer hostile, the place
stops being a hazard, with no other call:

```nql
travel party to "loc:valley/B04";
```

From the trailhead this stops at the mill after 600 seconds. After
`fight.nql`, the same request goes on to the watchtower.

Declared hazards stay for other dangers: a rockslide, a trap.

## Changing the map in play

| Action | What it does |
| --- | --- |
| `join party "<character>";` / `leave party "<character>";` | who travels together |
| `close route "<from>" to "<to>";` / `open route "<from>" to "<to>";` | one declared direction is barred or freed; close both to block both |
| `mark hazard "<place>";` / `clear hazard "<place>";` | a declared hazard |

Each is refused with `E_NO_CHANGE` when it is already so. Clearing a place
that is a hazard only because a hostile occupant is there is `E_MAP`
`hazard`, naming the occupant. Routes themselves change only with the world.

## Reading a place

nql-apply takes `"locations": ["loc:valley/B03"]` and returns, for each
place:

- `location`: its ID and name.
- `characters`: the characters there.
- `present`: the occupants with anyone there, each the full record.
- `resolved`: the occupants with no one left there, with their tally.

Every occupant comes with its whole tally, reasons included, so a host can
show the agent what became of each group at the place.
- With a map, `visited` and `hazard`.

After the fight, the mill reads:

```json
{
  "location": {"id": "loc:valley/B03", "name": "Old Mill"},
  "visited": true,
  "hazard": false,
  "characters": [{"id": "char:ada", "name": "Ada"}, {"id": "char:bram", "name": "Bram"}],
  "present": [],
  "resolved": [{
    "id": "occ:valley/B03/goblins", "name": "Goblins", "location": "loc:valley/B03",
    "kind": "creatures", "count": 0, "attitude": "hostile", "type": "goblin",
    "tally": [{"outcome": "defeated", "count": 2}, {"outcome": "fled", "count": 1}]
  }]
}
```

The view also carries `revision` and `clock`. One call can ask for any
number of places, every place in the world included; they are read together,
at one revision. An unknown place fails the call when it carries no
request. After a request commits, the response stays ok with its receipt,
`view_error` names the place, and the places before it still come back.

## Hosts that rebuild the world per call

A host that keeps the returned checkpoint keeps everything. A host that
writes a fresh world for each call passes `live_state`, and the engine keeps
the occupants for it (docs/LIVE_STATE.md). Without it, the host writes each
changed occupant back from the receipt: the last event for an occupant has
its `after` record, which maps one to one onto a declaration:

| Record | Declaration |
| --- | --- |
| `kind` person, `count` 1 | `person;` |
| `kind` person, `count` 0, `tally` | `person; tally <outcome> 1;` |
| `kind` creatures, `count` n | `creatures <n>;` |
| `kind` creatures, `range` | `creatures <min> to <max>;` |
| `kind` creatures, neither | `creatures;` |
| `attitude`, `type`, `aliases`, `tally` | `attitude`, `type`, one `alias` and one `tally` per entry, with `because "<reason>"` when it has one |

The same holds for the map's `party`, `closed` and `hazards`.

## Hosts with their own rosters

| Host record | Engine |
| --- | --- |
| a list of creatures per place, with a number, a dice text, a min/max or nothing | `creatures <n>`, `creatures <min> to <max>` or `creatures` |
| a list of named people per place | `person` occupants, one ID each |
| state written into a name ("Wounded Ranger") | an attitude, a tally, or a rename with the old name kept |
| a removal when creatures are beaten, flee or are banished | `resolve ... as <outcome>`, with the record kept |
| a removal after a parley, a bribe or a story turn, with no fight | `resolve ... as departed because "<reason>"` |
| someone removed who later turns up again | `return occupant`, the same ID |
| a search across places by name to move someone | `move occupant` by ID |
| a list of places with hostile creatures | derived from the occupants |

## Limits

- At most 32,768 occupants per world, resolved ones included. A group has at
  most 1,000,000,000 members over its life. At most 16 aliases each.
- Occupant actions are not Ability steps.
- The engine does not read `type`; fighting stays with the agent, which
  records the result with `resolve`.
