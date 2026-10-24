# World map and party travel

A world map is the engine's record of where places connect, how long each
link takes, where the party is and which places it has visited. An agent
names a destination with `travel party to`; the engine finds the way, moves
every party member, passes the time and marks the places visited. An agent
never writes the new location, the trip time or the visit list.

"Agent" means any caller that moves the party: a DM model, a travel menu, a
scripted scene. The agent decides where the party goes and what happens there;
the engine applies the trip atomically, refuses a trip the map does not allow,
and says why.

The files:

- `examples/map/world.nql`: a valley of four places and a keep.
- `examples/map/travel.nql`: one trip.
- `tests/world_map_test.go`: every number below, and the travel rule checked
  against a search of every path on thousands of random maps.

## Facts and totals

| The agent or host controls | The engine derives |
| --- | --- |
| the routes, each with its duration, and which are closed | the path a trip takes |
| who is in the party | that the party is together, and every member's new location |
| which places are hazards, and who is hostile where | where a trip stops early |
| the destination | the time that passes, through the world clock |
| | the places visited, including each new one |

## Setting up a world

A map needs a clock, because route durations are clock ticks:

```nql
clock "second" at 28800;
location "loc:valley/B01" named "Trailhead";
location "loc:valley/B02" named "Ranger Outpost";
location "loc:valley/B03" named "Old Mill";
location "loc:valley/B04" named "Watchtower";
location "loc:keep/A01" named "Keep Gate";
character "char:ada" named "Ada" at "loc:valley/B01" { }
character "char:bram" named "Bram" at "loc:valley/B01" { }
map {
 route "loc:valley/B01" to "loc:valley/B02" ticks 300 both;
 route "loc:valley/B02" to "loc:valley/B03" ticks 300 both;
 route "loc:valley/B01" to "loc:valley/B03" ticks 900 both;
 route "loc:valley/B03" to "loc:valley/B04" ticks 600 both;
 route "loc:valley/B04" to "loc:keep/A01" ticks 1800 both;
 party "char:ada";
 party "char:bram";
 visited "loc:valley/B01";
 visited "loc:valley/B02";
 visited "loc:valley/B03";
 hazard "loc:valley/B03";
}
```

- `route "<from>" to "<to>" ticks <n>;` is one direction. `both` adds the
  way back with the same duration. A link that is truly one way, such as a
  drop down a cliff, is declared without `both`.
- `party` lists the characters that travel together.
- `visited` lists the places the party has been.
- `hazard` marks a place where a trip stops on its way, for example a
  rockslide. A place where a hostile occupant is present is a hazard too,
  without a declaration (docs/LOCATIONS.md).
- `closed "<from>" to "<to>";` marks a declared route that cannot be used,
  such as a barred gate. Close both directions to block both.

Location IDs are free text. Qualifying them with the area or module, as above,
keeps two modules' `B01` apart.

## A trip

From `travel.nql`:

```nql
travel party to "loc:valley/B04";
```

The quickest way to the watchtower runs through the outpost and the mill: 300
+ 300 + 600 = 1,200 seconds, against 900 + 600 = 1,500 straight to the mill.
The mill is a hazard, so the party stops there after 600 seconds. Both
members move to the mill, the clock goes from 28,800 to 29,400, and any timed
Condition that runs out on the way ends at its own tick, as with `advance
time`.

The receipt's last event is `PartyTraveled`, with the request's `travel` and
the `trip`:

```json
"trip": {
  "from": "loc:valley/B01",
  "stops": ["loc:valley/B02", "loc:valley/B03"],
  "ticks": 600,
  "halted": true
}
```

`stops` lists the places reached in order; the last is where the party now
is. `halted` says a hazard stopped it before the destination. `visited` lists
the places visited for the first time, here none.

The agent resolves the mill and clears the hazard with `clear hazard
"loc:valley/B03";`. The same request then goes on: one stop, 600 seconds, and
the watchtower is visited:

```json
"trip": {
  "from": "loc:valley/B03",
  "stops": ["loc:valley/B04"],
  "ticks": 600,
  "visited": ["loc:valley/B04"]
}
```

Leaving a hazard never stops the party; only a hazard further on does. A
hazard at the destination is simply reached.

## The travel rule

- Every place the party passes through before the destination must be
  visited, or be the place it leaves. The destination may be new. The party
  walks through known places to reach a new neighbour, and cannot cross
  unknown country in one request. A place is visited by travel, or by
  `mark visited` when the party knows it without walking there, as after a
  guide's directions or an imported save.
- A closed route is never used.
- The quickest path wins. Equal times go to fewer stops, then to the path
  whose stops, compared in order, have the smaller IDs, so the same map
  always gives the same trip.
- Time passes by the ticks actually travelled. When the agent narrates a
  detour or a careful approach, it adds `advance time by <n>;` in the same
  request.
- The trip is part of the request: if anything in the request fails, no one
  moves, no time passes and nothing is marked visited.

## Refusals

Each is `E_MAP`, with the reason in `actual`, so an agent can correct its
request instead of retrying it:

| Field | Actual | Meaning |
| --- | --- | --- |
| `travel actor` | the actor | the request's actor is not in the party |
| `travel party` | where the member stands | a member is not at the actor's place; the subject names the member |
| `travel` | the place | the party is already there |
| `travel path` | `no route` | no routes lead there at all |
| `travel path` | `unvisited stop` | the way passes places the party has not visited |
| `travel path` | `closed route` | the way needs a closed route |
| `travel path` | `closed route and unvisited stop` | both |

## Reading the map

nql-apply takes `"map": ["char:ada"]` and returns, for each character:

- `location`: where it is.
- `exits`: each route out of it, with `ticks` and whether the far end is
  `visited`, a `hazard`, or the route `closed`.
- `destinations`: every place a trip can reach from here, sorted by ID, with
  the full path's `ticks`, its number of `stops`, whether the place is
  `visited`, and `halts_at`, the hazard where a trip toward it would stop
  first.

For the example world at the start:

| Destination | ticks | stops | visited | halts_at |
| --- | ---: | ---: | --- | --- |
| loc:valley/B02 | 300 | 1 | yes | |
| loc:valley/B03 | 600 | 2 | yes | |
| loc:valley/B04 | 1200 | 3 | no | loc:valley/B03 |

The keep gate is not listed: the way passes the watchtower, which the party
has not visited. The destinations are exactly the requests travel accepts, so
a host can offer them to the agent as its choices.

A world without a map refuses the view with `E_MAP` `map`.

## Hosts that rebuild the world per call

A host that keeps the returned checkpoint keeps everything. A host that
writes a fresh world from its own store for each call passes `live_state`
(docs/LIVE_STATE.md). The document then holds where every character stands,
the party, the visited places, the declared hazards, the closures and the
clock. A module installed later declares its own hazards and closures in
the source like any other: its places are new to the document, which takes
them once. The host stores only the routes, built from the host's link
data, not from the agent, so a narrated change cannot redraw the map.

Without `live_state`, the host stores all of it:

- Each party member's location: the `location` from `status`, or the last of
  the trip's `stops`.
- The clock: the receipt's `TimeAdvanced` `after`, or the checkpoint's clock.
- The visit list: add the trip's `visited`, and the place of each
  `mark visited`.
- The party, hazards and closures: travel never changes them. `join party`,
  `leave party`, `mark hazard`, `clear hazard`, `close route` and `open route`
  do, each with an event naming the change for the host to write back.
- The routes: built from the host's link data, not from the agent, so a
  narrated change cannot redraw the map.

## Hosts with their own travel checks

| Host record | Engine |
| --- | --- |
| a list of links per place | `route` declarations, `both` for two-way links |
| an estimated travel time per move | the route's `ticks` |
| current-location copies in several files | each character's location, one writer |
| visits inferred from summaries | the `visited` list and the trip's `visited` |
| a path search or validator before each move | `travel party to`, which refuses with a reason |
| a prompt rule to travel only through known places | the travel rule, enforced |
| a list of places with hostile creatures | hostile occupants, or `hazard` |

The separate checks then retire: the host shows the `map` view and stops
writing locations and visit flags itself.

## Changing the map in play

| Action | Event |
| --- | --- |
| `join party "<character>";` / `leave party "<character>";` | `PartyJoined` / `PartyLeft` |
| `close route "<from>" to "<to>";` / `open route "<from>" to "<to>";` | `RouteClosed` / `RouteOpened` |
| `mark hazard "<place>";` / `clear hazard "<place>";` | `HazardMarked` / `HazardCleared` |
| `mark visited "<place>";` | `VisitMarked` |

Each event carries `map_edit`, the change. Each is refused with
`E_NO_CHANGE` when it is already so, and a route must be declared (`E_MAP`
`route`). Clearing a place that is a hazard only because a hostile occupant is
there is `E_MAP` `hazard`, naming the occupant; resolving it clears the place.
Marking a place visited moves no one and passes no time; there is no way to
unmark one.

## Limits

- A map holds at most 32,768 directed routes. A route takes 1 to
  1,000,000,000 ticks.
- A world with a map has no `battle` or `adventure` block, which travel by
  their own routes.
- One request's moves and time count against its 128 work units: a party
  travels with one unit per member plus one for the time, so a party of more
  than 127 cannot travel in one request.
- Routes change only with the world; the party, hazards and closures change
  in play (below). With `live_state`, a closed route stays closed across
  calls that do not declare it (docs/LIVE_STATE.md).
- Travel does not check who is able to walk; the agent decides who comes.

Measured with nql-apply on a 4,096-location map with 32,768 routes, every
place visited and a party of four: about 160 ms per call, of which the compile
takes about 140 ms. A world of 36 places and 87 routes answers a travel call
in about 3 ms.
