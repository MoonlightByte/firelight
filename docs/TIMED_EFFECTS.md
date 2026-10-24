# Timed effects for nql-apply hosts

This guide is for a host that keeps its own game state and calls `nql-apply`
per request. It covers timed spells, potions and similar effects: how to
declare them, how to advance time, how they end, and how to read the result.
Grammar details are in [GRAMMAR.md](GRAMMAR.md).

Everything here is opt-in. A world without a `clock` line and without Condition
types behaves exactly as it would otherwise.

## The pieces

An effect is a Condition instance of a Condition type. The type says what the
effect does; the instance says who has it, how strong it is and when it ends.

```nql
rules { transfer unequips; wear any; }
clock "second" at 86400;
location "loc:inn" named "Inn";
character "char:mira" named "Mira" at "loc:inn" {
    stat "defense" = 15;
    resource "hp" = 18 min 0 max 18;
}
condition type "spell:shield-of-faith" { instances unique; modifier "ac" stat "defense" add 2; }
condition type "spell:aid" { instances replace; modifier "hp" resource "hp" maximum add magnitude; }
condition type "potion:barkskin" { instances replace; modifier "ac" stat "defense" add magnitude; }
condition type "blessing:rest" { instances unique; }
condition "effect:sof:mira" of "spell:shield-of-faith" to "char:mira" ticks 300;
```

- `clock "<unit>" at <tick>;` sets world time. The unit is only a label; the
  engine never converts it. Use seconds so rounds (6), minutes (60) and hours
  (3600) are all exact.
- A type has an instance policy:
  - `unique`: a second instance of that type on the same target is refused (E_RULE).
  - `replace`: a new instance ends the old one first.
  - `coexist`: instances stack.
  - Add `modifier_selection latest;` to a `coexist` type when only the newest
    instance's stat modifiers should count ("does not stack").
- Modifiers, up to 16 per type:
  - `modifier "<id>" stat "<stat>" add <N>;` adds to a stat while the effect is active.
  - `modifier "<id>" resource "<res>" maximum add <N>;` raises a Resource
    maximum while the effect is active, or drains it when N is negative.
  - `modifier "<id>" stat "<stat>" at least <N>;` or `at most <N>;` holds the
    stat at N or above, or at N or below, after every addition: Gauntlets of
    Ogre Power set Strength 19, Grappled sets Speed 0. A ceiling wins over a
    floor.
  - `modifier "<id>" stat "<stat>" roll advantage;`, `roll disadvantage;` or
    `roll fail;` sets how checks on the stat, and on every stat that reads it,
    roll. It has no amount. docs/CHARACTER_STATS.md covers checks.
  - Any amount may use `magnitude` instead of a number. Each instance then
    supplies its own positive or negative `magnitude N`, which must not be zero
    for a Resource maximum.
- Instance clauses:
  - `ticks N` makes the instance end N ticks after it was applied.
  - Without `ticks` it lasts until removed.
- `ends_at_minimum "<res>";` on a type ends the effect when that Resource
  reaches its minimum (for example concentration-like upkeep).
- `held_at_minimum "<res>";` on a type makes it present, with no instance,
  while that Resource is at its minimum (Unconscious at 0 hit points). Its
  modifiers must be fixed bounds or roll modes.

## Actions

```nql
apply condition "effect:aid:mira" of "spell:aid" to "char:mira" magnitude 5 ticks 28800;
heal "char:mira" resource "hp" by 5;
apply condition "effect:bless:mira" of "blessing:rest" to "char:mira";
advance time by 360;
remove condition "effect:bless:mira" from "char:mira";
```

- Operations in one request run in order, and the request commits or fails as
  a whole. The heal above runs after Aid has raised the maximum, so it can use
  the extra room.
- `advance time by N;` moves the clock forward by N, a positive number. It ends
  every timed instance whose expiry tick is reached, earliest first.
- `remove condition` ends an instance now. Use it for "until the next long
  rest" effects: apply them without `ticks` and remove them in the rest request.
- An instance ID can be used once in a world's lifetime: removed and expired IDs
  stay reserved (E_DUPLICATE). Give every application a fresh ID, for example
  one derived from your request ID.

## Per-instance strength (magnitude)

Use `add magnitude` when the same effect varies in strength. Aid grants +5
maximum HP at 2nd level and +5 more per higher slot. Barkskin-style bonuses
vary with the source. One type covers every strength:

```nql
condition type "spell:aid" { instances replace; modifier "hp" resource "hp" maximum add magnitude; }
apply condition "effect:aid:mira" of "spell:aid" to "char:mira" magnitude 10 ticks 28800;
```

A type that uses `magnitude` requires it on every instance, and a type that
does not use it refuses it (E_CONDITION_AMOUNT).

## Resource maximum effects

Aid-style effects store the change on the character:

- Apply raises the maximum. Current HP is unchanged: send the heal yourself.
- Every ending lowers the maximum by the same amount and clamps current HP down
  to it. That covers expiry, removal, replacement and floor release. Mira at
  23/23 whose Aid (+5) ends is 18/18; at 7/23 she is 7/18.
- An ending is never refused. The maximum never drops below the Resource
  minimum. If the clamp takes current to the minimum, it counts as damage to
  the minimum: `ends_at_minimum` effects on that Resource end too.
- Replacing an instance (`instances replace`) ends the old one before applying
  the new one. Replacing Aid +5 with Aid +10 at 23/23 goes to 18/18, then 18/28.
- Refused at apply:
  - a missing Resource (E_RESOURCE);
  - a temporary HP buffer pool (E_RULE);
  - Battle health (E_RULE);
  - a `modifier_selection latest` type (E_RULE);
  - a zero amount (E_CONDITION_AMOUNT);
  - overflow (E_OVERFLOW).

## Max HP drains

A negative amount drains the maximum, for effects such as a wraith's Life
Drain or a curse:

```nql
condition type "curse:life-drain" { instances coexist; modifier "drain" resource "hp" maximum add magnitude; }
damage "char:mira" resource "hp" by 7;
apply condition "effect:drain:mira" of "curse:life-drain" to "char:mira" magnitude -7;
```

- Apply lowers the maximum and clamps current down to it. Mira at 23/23,
  damaged by 7 and drained by 7 as above, is 16/16; drained by 7 at 23/23 she
  would also be 16/16, and at 10/23 she is 10/16.
- A drain may take at most `maximum - minimum - active raises` on that
  Resource; a larger one is refused (E_CONDITION_AMOUNT) and the fault's
  `actual` shows the numbers. Size it from status first:
  `min(requested, maximum - minimum - your active raises)`. So while Aid is
  active, a drain cannot take max HP below the minimum plus Aid; "dies at max
  HP 0" is the host's call.
- Every ending gives the amount back to the maximum and leaves current
  unchanged: heal explicitly. Mira at 16/16 whose drain of 7 ends is 16/23.
- Endings reverse exactly in any order. Aid +5 on 20/20, then a drain of 15,
  then Aid ends, then the drain ends: the maximum is back at 20.
- If the drain's clamp takes current to the minimum, `ends_at_minimum` effects
  on that Resource end (reason `minimum`), before the drain is applied. A drain
  whose own type ends at that minimum, or whose anchor that release ends, is
  refused (E_CONDITION).
- One instance's modifiers on one Resource must all raise or all drain
  (E_CONDITION_AMOUNT).
- The maximum plus the active drains must fit int64, so a raise, an `expand` or
  a genesis that would break this is refused (E_OVERFLOW).
- `ended_conditions[].modifiers` carries the signed amount:
  `{"id": "drain", "resource_maximum": "hp", "amount": -7}`, so
  `maximum before the ending = after + amount`.
- Genesis: declare the drained maximum as status reported it, like Aid.

## Reading the response

Ask for `status` and `explain` in the same request as the actions. The views
show the state after the request.

- `status[].clock`: `{"unit": "second", "tick": 86760}`.
- `status[].conditions[]`: each active instance. Timed ones carry their
  lifetime; remaining time is `expires_tick - clock.tick`.

  ```json
  [{"clock_lifetime": {"applied_tick": 86400, "expires_tick": 115200}, "instance": "effect:aid:mira", "type": "spell:aid"},
   {"instance": "effect:bless:mira", "type": "blessing:rest"}]
  ```

- `status[].held_conditions[]`: each type held at a Resource minimum, such as
  `{"type": "state:unconscious", "resource": "hp"}`. It has no instance and
  needs no storing: the Resource brings it back.
- `status[].resources.hp`: `{"current": 23, "minimum": 0, "maximum": 23}`. The
  maximum includes active Resource maximum effects.
- `explain` with `{"character": "char:mira", "stat": "defense"}`: `effective`
  includes active stat modifiers. Each one is a contribution with
  `condition_origin {instance, modifier}` and `amount`. A bound is listed under
  `bounds` instead, with `applied` true when it set the total. Roll modifiers
  in force are listed under `roll_sources`, and `roll` is their net mode.
- `ended_conditions`: every instance the request ended, in order. The field is
  omitted when nothing ended. A historical retry returns the same list.

  ```json
  [{"instance": "effect:sof:mira", "type": "spell:shield-of-faith", "target": "char:mira",
    "reason": "clock", "deadline": 86700,
    "modifiers": [{"id": "ac", "stat": "defense", "amount": 2}]}]
  ```

  | reason | meaning | extra field |
  | --- | --- | --- |
  | `clock` | its ticks ran out | `deadline`, the expiry tick |
  | `removed` | an explicit or Ability removal | |
  | `replaced` | a `replace` instance took its place | `by`, the new instance |
  | `anchor` | its anchor instance ended | `by`, the anchor |
  | `minimum` | an `ends_at_minimum` Resource reached its minimum, or its `ends_on_condition` type became held there | `minimum_resource` |
  | `condition` | an `ends_on_condition` type arrived | `by`, the arriving instance |
  | `expired` | its Adventure round lifetime ended | |
  | `battle` | a Battle lifetime ended | |

  `modifiers` lists what the instance was giving when it ended, magnitudes
  resolved. Each entry is `{id, stat, amount}` or `{id, resource_maximum,
  amount}`, and the list is `[]` for a type without modifiers. Bounds and roll
  modes are not listed; the stat's `bounds` show what holds it now. With the stats
  in the same response, "Shield of Faith ended, AC 17 -> 15" needs no second
  request.

The full event trace is in `receipt.events`: `ConditionApplied`,
`ConditionRemoved` (with `clock_expiration` or another reason, and
`resources` for maximum changes) and `TimeAdvanced {before, after}`.

## Host loop (stateless genesis)

A host that rebuilds the world for every request:

1. Keep game time as a datetime and convert it to whole seconds since a fixed
   campaign epoch. That number is the clock tick T.
2. For each active effect, keep its instance ID, type, target, magnitude and
   expiry tick.
3. Per request, write the world with `clock "second" at T;`, the Condition
   types, and each active effect as
   `condition "<id>" of "<type>" to "<char>" [magnitude N] ticks <expires - T>;`.
   Write untimed effects without `ticks`.
4. Declare every Resource exactly as status last reported it. A declared
   effect is taken as already applied: its maximum is already in the declared
   value and is not added again. Mira with Aid active is declared at max 23,
   not 18.
5. Send the actions: apply, heal, `advance time by <elapsed seconds>`, remove.
6. From the response, store the new clock tick, each remaining effect's
   `expires_tick`, the Resources and stats, and report `ended_conditions`.

A host that passes `live_state` (docs/LIVE_STATE.md) keeps the clock in the
document instead: T is the document's `clock` `tick`, the tick it declares is
ignored, and `ticks <expires - T>` runs from the document's tick. It stores
no clock tick in step 6.

With this loop, instance IDs only need to be unique within one request and
the active set, because each genesis starts a fresh world. A host that instead
keeps the returned `checkpoint` must never reuse an ended ID.

## Errors worth handling

| code | typical cause |
| --- | --- |
| E_CLOCK | `ticks` or `advance time` in a world without `clock`, a nonpositive tick count, or more than 64 active timed instances |
| E_RULE | a second `unique` instance, or a Resource maximum on a buffer pool, Battle health or a `latest` type |
| E_DUPLICATE | an instance ID that is active or already ended |
| E_CONDITION_AMOUNT | magnitude missing or unexpected, a zero Resource maximum amount, a raise and a drain on one Resource, or a drain beyond its room |
| E_RESOURCE, E_STAT | the target lacks the modified Resource or stat |
| E_LIMIT | a request whose expiries exceed its work budget |
| E_OVERFLOW | a raised Resource maximum, plus its active drains, would overflow int64 |

Static mistakes come back as `phase: "compile"` diagnostics with a source
position; state-dependent ones come back as `phase: "execute"` with `fault`.

## Limits

- 64 active timed instances per world.
- 16 modifiers per Condition type.
- One request does at most 128 units of work, and each expiry costs one.
  Advancing across many expiries at once can hit E_LIMIT; advance in steps.
- A world with a `clock` line is also held to the native entity bounds (for
  example at most 4096 characters).
