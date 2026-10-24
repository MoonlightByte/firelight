# NQL M1 grammar and semantics

Source is UTF-8. Keywords are lowercase and case-sensitive; `//` begins a line comment. Strings use JSON escapes, including paired Unicode surrogates; invalid UTF-8, invalid escapes, and lone surrogates are rejected. String identity values must be nonblank and contain no control characters. Display names are not identities. Integers are decimal signed int64 literals; an optional `+` or `-` is accepted. No arithmetic expressions exist.

World and action files are separate entry points. Both consume the whole input and stop at the first diagnostic. Lines/columns are 1-based, columns count Unicode code points, and a tab counts as one column. A world source is at most 16 MiB and an action source at most 1 MiB; see [WORLD_SIZE.md](WORLD_SIZE.md). Diagnostics include source, line, column, code, expected, actual, and explanation; semantic positions identify the implicated literal/declaration, not necessarily a complete span.

```ebnf
world       = { rules | location | character | item | effectDecl | relationDecl } ;
rules       = "rules", "{", transferRule, wearRule, "}" ;
transferRule= "transfer", ("keeps_worn" | "unequips"), ";" ;
wearRule    = "wear", ("any" | "owner_only"), ";" ;
location    = "location", string, "named", string, ";" ;
character   = "character", string, "named", string, "at", string,
              "{", { stat | resource }, "}" ;
stat        = "stat", string, "=", int64, ";" ;
resource    = "resource", string, "=", int64, "min", int64, "max", int64, ";" ;
item        = "item", string, "named", string,
              "{", [ owner ], [ wearer ], "}" ;
owner       = "owner", string, ";" ;
wearer      = "wearer", string, ";" ;
ref         = ("character" | "item" | "location"), string ;
effect      = string, "from", ref, "on", ("character" | "worn"), string,
              "stat", string, "add", int64 ;
effectDecl  = "effect", effect, ";" ;
relation    = ref, string, ref, "=", int64 ;
relationDecl= "relation", relation, ";" ;

actions     = action, { action } ;
action      = ( "transfer", string, "from", string, "to", string
              | "equip", string, "on", string, [ "mode", string ]
              | "unequip", string
              | ("damage" | "heal"), string, "resource", string, "by", int64
              | "apply", "effect", effect
              | ("remove" | "expire"), "effect", string
              | "move", string, "to", string
              | "relate", relation ), ";" ;
```

Rules/owner/wearer clauses may appear in either order inside their block. Exactly one rules block with both choices is required. Empty worlds are allowed, but any references must resolve. World declarations support forward references. Repeated entity IDs, stat/resource fields, rules, owner/wearer clauses, effect IDs, and directed relationship keys are rejected. Stat/resource IDs are local authored labels; effect IDs form a separate namespace. A relationship key is `(typed from, kind, typed to)`.

Implicit operand types are determined by the semantic verb: transfer/equip/unequip take an item; move takes a character and location; resource operations take a character. The compiler checks actual entity kinds using the catalog, not ID prefixes. Explicit `ref` syntax is used when multiple entity kinds are valid.

One action file is one atomic transaction, with 1-128 operations. Transfer requires the stated current owner and a different recipient. Equip requires an unworn item and a permitted wearer; unequip requires a worn item. Movement and relationship changes reject an unchanged value. Positive damage/healing can succeed at a bound with zero actual delta; the requested amount and before/after resource facts remain explicit in the event.

Character effects require their target's declared stat. Worn effects target items; when unworn they are inactive and can refer to a stat a later wearer must have. Runtime checks compatibility against the candidate's current wearer. Source attribution never determines activation. Removing/expiring effects requires an existing active record ID; inactive worn records still count as existing records.

Runtime checks structural prerequisites and game policies at each operation. It checks final effective numeric bounds on the complete candidate. Removing a negative effect can therefore cause atomic rejection, while removing compensating positive and negative effects in one transaction can succeed. Derived summation uses exact intermediate arithmetic, then checks the final int64 result. Resource arithmetic also avoids overflow before clamping.

Inspection and task queries are Go/CLI read operations, not additional action syntax. They cannot be compiled into writable facts. The compiler does not execute actions or evaluate host authorization.

Events have typed payloads, deterministic IDs `event:<revision>:<1-based-index>`, 0-based operation indexes, request ID, actor, and revision. Rule-triggered events share their parent operation index. Replay accepts at most 1,024 batches, each with at most 256 events. It reconstructs operations from each event group's terminal semantic event, reruns the typed transition, and requires exact complete-trace agreement. Retained original requests provide consistency and idempotency receipts, not an alternative transition input. An invalid later batch returns no partially reconstructed world.

Replay is local, trusted-input reconstruction, not an authenticated ledger, durable event store, or compatibility guarantee across engine/rule versions. Revision starts at zero, advances once per successful new request, and cannot wrap uint64. Event IDs are local to this initial world/history; M1 has no global world identifier.

## Later native creation extension

The historical M1 grammar above remains its original scope. Native Character
creation reuses its character block in a direct action: `create character ... {};`.
Creation resolves references in order and writes exact initial records; constraints
and host seeds are described in docs/PROTOCOL.md.
Creation is unavailable inside Abilities and does not imply membership or training.

Direct Item creation also reuses its declaration block:

```nql
create item "item:new" named "Ribbon" {
 description "Blue ribbon with a stitched bridge.";
 owner "char:a"; quantity 1;
};
```

`description` is optional immutable UTF-8 metadata in both Item forms. It grants
no mechanic. CreateItem requires a globally unused ID, positive initial effective
stock, an existing optional Character owner, and initially unworn state. An
optional equipment definition must already exist; a later Equip uses the ordinary
slots/training/time evaluator. Sequential action resolution sees earlier births.
No creation occurs inside Abilities.

## Item catalog extension

See [ITEM_CATALOG.md](ITEM_CATALOG.md) for executable examples and host write-back.

```ebnf
itemType = "item", "type", string, "named", string, "{", { itemTypeField }, "}" ;
itemTypeField = ("description" | "kind" | "subtype" | "equipment"), string, ";"
              | ("magical" | "consumable"), ("true" | "false"), ";"
              | "price", string, int64, "per", int64, ";"
              | "charges", int64, ";"
              | "initial", (int64 | "dice", chargeDice), ";"
              | "recharge", "every", uint64, [ "at", uint64 ],
                ("all" | "by", int64 | "dice", chargeDice), ";"
              | "exhausted", ("destroy" | "roll", int64, [ "destroy", int64 ],
                [ "regain", int64, "dice", chargeDice ]), ";" ;
chargeDice = int64, "sides", int64, [ "plus", int64 ] ;
catalogItem = "item", string, "from", string, "{", { instanceField }, "}" ;
catalogCreate = "create", catalogItem, ";" ;
instanceField = ("owner" | "wearer" | "mode"), string, ";"
              | ("quantity" | "recoverable"), int64, ";"
              | "charges", int64, [ "of", int64 ], [ "since", uint64 ],
                [ "seed", uint64, uint64 ], ";"
              | "custody", ref, ";"
              | "container", ("true" | "false"), ";" ;
```

Each field may appear once, in any order. Kind is required; other omitted
classification fields default to empty/false. Definitions have a separate ID
namespace and allow forward references. The `from` forms do not accept name,
description or equipment-definition overrides. Mode remains instance state,
subject to existing initially-unworn creation rules. World declarations end
with `}`; actions end with `};`. Existing `named` forms are unchanged.
The compiler retains intent; the engine derives display/equipment data.

`uint64` is an unsigned decimal integer up to 18446744073709551615. Charged
items add `expend`. See [ITEM_CHARGES.md](ITEM_CHARGES.md).

```ebnf
expend = "expend", int64, "charges", "of", string, "by", string, ";" ;
```

## Native Battle authored actions and rolled Resource consequences

The native Battle profile may include `action "ability-id";` entries after its
required policy fields. Each names a declared Ability with an `action actor ...
phase ...;` policy. Names are resolved after all declarations. The list admits a
vocabulary; authored requirements still determine eligibility for the supplied
actor and targets. Owner/wearer and equipment-only Ability semantics are unchanged.

An eligible Ability invoked during an active Battle consumes the current actor's
one action and ends that turn in the same transaction. An exploration invocation
has no implicit turn or elapsed time. The terminal `AbilityInvoked` event retains
its original invocation and, when applicable, the encounter before/after. Checks
use the existing recorded random stream. Historical successful requests are not
reevaluated for current costs or eligibility.

One top-level rolled Resource step is supported in a native opted-in Ability:

```nql
ability "recorded-care" named "Recorded care" {
 parameter "actor" character;
 parameter "target" character;
 action actor param "actor" phase either;
 require colocated param "actor" with param "target";
 spend param "actor" resource "focus" by 1;
 roll heal param "target" resource "hp" dice 2 sides 8 bonus from param "actor" {
  constant 0;
  term stat "wisdom" offset -10 divide 2;
 };
}
```

The kind is `heal` or `damage`; the target and bonus source are Character
literals or parameters. Resource selectors, dice count (1..100), sides (2..1000),
and the existing scalar recipe are immutable authored data. The evaluated bonus
belongs to the explicit bonus source. The recipe uses existing non-derived
effective-stat terms and mathematical floor division. Dice plus bonus uses exact
intermediate arithmetic, clamps negative totals to zero, then requires int64.
A positive result uses the existing Resource clamp operation. Zero records the
roll, retains costs and consumes an active turn without a zero Resource operation.
Generic rolled damage does not infer attack, resistance, spell, range or refund
rules. Those require appropriate authored semantics and host admission.

A `heal` step may declare a floor after `sides`, as in `dice 1 sides 6 min 1`.
The floor is a positive int64 literal. After dice plus bonus is clamped at
zero, a smaller total is raised to the floor, so a floored heal never records a
zero result; a larger total is unchanged. The floor applies to the step's whole
total, not to each die: one die per step gives a per-die floor. `min 0` is
rejected (omit the floor instead), and a `damage` step declares no floor.

Human faces may be retained on the original invocation (body indexes are zero-based):

```nql
invoke "recorded-care" {
 "actor" = character "char:actor";
 "target" = character "char:target";
 roll 1 { face 4; face 5; };
};
```

Normal model proposals omit `roll` input. The trusted human-input host stages a
read-only dice question, then retains the complete exact face group before
execution. Missing, extra, out-of-range or wrong-step faces reject; NPCs cannot
supply faces and use NQL's existing PCG instead. `ResourceRollResolved` preserves
faces, source, bonus explanation, requested/actual amount and random counters.
The original invocation remains the receipt/replay identity. Mutating returned
faces or explanations cannot mutate authority. No payment or live random draw
occurs while asking for input. Save/resume preserves a pending question; a
historical successful retry returns the complete original result.

A human controller may decline faces for one step and let NQL roll it:

```nql
invoke "recorded-care" {
 "actor" = character "char:actor";
 "target" = character "char:target";
 roll 1 engine;
};
```

`roll BODY_INDEX engine;` applies to a rolled Resource step or an `actor_roll`
Check. The step is drawn from the recorded Battle random stream exactly as for
an agent, and its result records `engine_drawn` beside the human source. Faces
for the same step, an agent's opt-out and a step that takes no faces reject.
The opt-out is part of the original invocation: asking for input treats the
step as answered and makes no live draw, exact replay draws the same faces, and a
historical successful retry returns the original result. Histories that use
it need this runtime or a later one.

The current rolled consequence cannot occur inside a Check branch or share an
Ability with a branching Check. This is a bounded grammar contract, not a claim
that all spell or item mechanics are delivered. Existing direct operations and
Alpha checks retain their accepted behavior.

A native Battle Action can explicitly request controller-selected Check dice:

```nql
check "inscription" die 20 actor_roll plus stat param "actor" "skill:history" against 10 {
 success { apply condition param "record" of "lore:bridge-builder" to param "actor"; }
 failure { }
}
```

`actor_roll` is optional and legal only in a native Battle Action. A human
supplies one raw face through the existing `roll BODY_INDEX { face N; };`
invocation field and pending dice channel, or declines with
`roll BODY_INDEX engine;` as above; an agent uses the existing Battle random
stream and may not supply faces. The shared Check evaluator records
source, draw, modifier, total, threshold and the selected branch. An unsuccessful
comparison is a committed outcome, including earlier costs. A rejected later
step rolls the whole request back. Neither asking for input nor historical retry
charges or rolls again. There is still at most one Check and no nested Check or
rolled Resource step in its branches. Without `actor_roll`, native and Alpha
checks retain their prior automatic RNG behavior. A bonus is never inferred;
it requires the separate explicit policy below.

An actor-roll Check can now opt into one optional failure bonus:

```nql
check "inscription" die 20 actor_roll plus stat param "actor" "skill:history" against 10 {
 boost condition "feature:tactical-mind" resource "second-wind" cost 1 die 10 on_success;
 success { apply condition param "record" of "lore:bridge-builder" to param "actor"; }
 failure {}
}
```

The names remain authored content. The required Condition and sufficient Resource
must belong to the action actor on the ordered candidate. Base success requires
no bonus; ineligible base failure proceeds normally. An eligible failed human
Check requests explicit use or decline, retaining its base face and provisional
comparison. Use supplies one raw bonus face; decline supplies none. The existing
input channel retains both before one complete execution. A typed/source final
invocation records `boost BODY_INDEX use face N;` or `boost BODY_INDEX decline;`.
Models cannot provide a human's choice or faces through native host admission.

An NPC may preselect `boost BODY_INDEX use;` as a contingency, with no supplied
face. NQL draws the bonus from the existing stream only after eligible failure.
No selection means decline; base success ignores the unused contingency. A used
bonus that still fails is a committed failure without payment. If it succeeds,
existing exact Spend pays the cost, then the success branch runs. The accepted
comparison is frozen before payment consequences; a later error rolls everything
back. `CheckResolved` records original draw/modifier, base total, granting
Condition IDs, bonus, final total/outcome and payment, with the ordinary separate
ResourceSpent event. This is one bonus, not a reroll, reaction engine or healing.
Both base and augmented totals must fit int64. A boost reserves two additional
authored work slots (bonus and potential payment) within existing bounds.

The native host uses `check-boost-v1` pending records only when an immutable
definition contains a boost. An accepted final answer that leads to rejection
is retained and closes the question; it cannot be changed into another roll.
Older question versions retain their prior meanings. Save/resume and named Load
preserve accepted base evidence while applying the existing branch identity rules.

## Authored native range requirement

```nql
require within param "actor" with param "target" distance 5 unit "feet";
```

Both operands are Characters; the nonnegative literal distance uses the explicit
Battle grid unit. This reuses weapon reach's Chebyshev grid metric. A different
Character needs an actual current encounter position in the same location;
colocation alone does not prove touch/range. Self has distance zero. Unknown
positions reject before costs, rolls or turns. No line-of-sight, obstacle or
touch-consent rule is implied. Exploration placement is not supplied by this
predicate; it remains an integration dependency, not an inferred location fact.
# Authored equipment prerequisites

Action requirements can query canonical equipment occupancy and conditional
training without storing another loadout or eligibility result:

```nql
require free_slot param "actor" slot "hand" count 1;
require worn_training param "actor" definition "gear:shield" mode "held" condition "training:shield";
```

`free_slot` requires the named declared slot to have at least the positive
literal count available. It shares the Equipment projection's occupancy
calculation. `worn_training` requires the declared Condition type only when the
subject actually wears the selected declared definition/mode. Ownership alone
does not activate it. Both use the current ordered candidate and existing
action rejection/receipt/replay path. These names declare no universal spell
or armor rule; content chooses the required slots, modes and Conditions.
# Native authored travel (package continuation)

`require at param "actor" location "loc:bridge";` checks a Character's current
canonical location against a declared literal Location. Literal Character
operands also work. It uses the same action predicate evaluator in both profiles;
source positions, closed shapes and ordinary historical retry rules apply.

The optional `route` clauses in the existing Battle block declare public,
directed topology; they do not change Alpha Adventure travel:

```nql
route "loc:hall" to "loc:road" ticks 600 victory required;
route "loc:road" to "loc:hall" ticks 600 victory optional;
```

There are at most64 routes, with distinct existing Location endpoints, a unique
direction and positive int64 duration in the shared Clock unit. `required`
requires a recorded completed victory at the departure location; `optional`
does not. It does not promise that a location remains safe forever.

`travel party to "loc:road";` is a direct action and an existing Ability body
form, both lowering to PartyTravel. On the native Battle profile only its
party-root actor may request it, outside an active encounter. Every current
member selected by the authored membership relation must be standing and at the
root's location. All move in sorted ID order, then the authored duration passes
with ordinary Clock expiry. The complete operation, events and receipt commit
once, under the existing work/event/archive bounds. A later failure rolls back
the entire request. Host permission/companion consent is separately required.
No encounter, loot, reward, carrying, recovery or tactical position is inferred.

Native recovery also admits `restore "char:a" resource "hp";` and the Ability
form `restore param "target" resource "hp";` (Character literal or parameter,
literal Resource selector, no amount). It restores current to canonical maximum,
preserves minimum/maximum and emits ResourceRestored even when already full.
Existing life semantics apply: living downed Characters can recover; dead ones
cannot resurrect. Restoration is not a rest scheduler. An authored rest composes
time/eligibility and restoration in one normal transaction. Flat check branches
may contain this same primitive; no separate evaluator or request is created.

## Maintained Conditions (native rebuild extension)

A Condition can be the stable root of other Conditions. Root removal releases
its direct dependents first, by ID, then the root. Dependent removal alone does
not release the root or siblings. No nested anchors, self-reference or cycles.
Every removed lifetime remains reserved. Effects still use the single Condition
contributor evaluator, and the whole causing operation publishes once.

```nql
condition type "state:maintaining" {
    instances replace;
    ends_at_minimum "hp";
}
condition type "ward" {
    instances coexist;
    modifier "defense" stat "defense" add 2;
}
// Inside an Ability with character actor/target and condition_id root/ward:
apply condition param "root" of "state:maintaining" to param "actor" ticks 60;
apply condition param "ward" of "ward" to param "target" anchor param "root";
```

`replace` replaces the prior same-type/target instance and its dependents; it
does not merge identities. Existing `unique` rejects duplicates and `coexist`
retains them. `ends_at_minimum` names a Resource on the Condition target. It
must be above its minimum at application; any debit reaching minimum releases
the Condition and dependents before its Resource event, even if a later step
heals the character. No particular HP, spell or concentration rule is built in.

Optional `ends_on_condition "blocked";` names a different declared Condition
type on the same target. The maintained Condition cannot be applied while that
blocker is present; this check precedes any replacement or automatic removal.
Applying the blocker removes the maintained instances and their anchored groups,
in stable ID order, before the final ConditionApplied event. Removal annotates
the triggering instance as `ending_condition`. Genesis obeys the same absence
law; forward type references are allowed. Removing the blocker does not restore
ended Conditions or release their lifetime IDs. An invalid incoming anchor or
later failed step rolls back every consequence. No recursive application occurs.

Direct/initial Condition syntax also accepts `anchor "instance-id"`. Genesis
permits forward anchor references; runtime application resolves against the
ordered candidate. Ability anchor operands accept a literal Condition ID or
`param "name"` of `condition_id`. Native host arguments remain creation IDs:
author a new root and reuse its creation parameter in the same invocation.
This is not an admission change for historical IDs as invocation arguments.

Replacement, explicit removal, Resource-floor release and clock/round expiry
regenerate all removal events in replay; event annotations identify the root,
replacement or minimum Resource. Existing work/event/archive bounds apply.
Damage-triggered saving checks and bonus-action economies are separate rules
work; this extension alone does not claim full concentration or spell support.

A Condition type may opt into `modifier_selection latest;`. The omitted default
continues adding every instance's modifiers. With `latest`, one whole instance
per type/target contributes numeric modifiers; other types and equipment remain
independent. Every instance and generated Effect remains recorded, including
suppressed ones. Stat explanations expose `suppressed` contributions and their
winning instance `by`; active-effect projections omit suppressed contributors.
Presence requirements still see all instances. Removing or expiring the winner
reveals the next still-existing instance, subject to normal numeric validation.

Latest is ordered by applied revision, numeric outer operation, Ability step and
numeric branch path. Equal provenance (including genesis) uses stable instance ID
lexical order as an explicit tie policy. It does not use request names, source
positions, duration or modifier strength. This is an authored selection rule,
not a universal rule for stacking or overlapping game effects.

## Condition Resource maximum modifiers

A Condition type modifier may raise or drain a Resource maximum instead of a
stat:

```nql
condition type "spell:aid" {
    instances replace;
    modifier "aid" resource "hp" maximum add magnitude;
}
condition type "curse:life-drain" {
    instances coexist;
    modifier "drain" resource "hp" maximum add magnitude;
}
apply condition "effect:aid:a" of "spell:aid" to "char:a" magnitude 5 ticks 28800;
heal "char:a" resource "hp" by 5;
apply condition "effect:drain:a" of "curse:life-drain" to "char:a" magnitude -7;
```

The amount is a nonzero int64 above the int64 minimum, fixed or the instance's
`magnitude`: positive raises, negative drains. One instance's modifiers on one
Resource must all raise or all drain (E_CONDITION_AMOUNT), because its ending
would otherwise depend on modifier order. Stat and Resource maximum modifiers
may be mixed within the 16-modifier limit; one magnitude serves both. Unlike a
stat modifier, the maximum is stored, not a derived Effect.

A raise increases the target's canonical maximum on application and leaves
current unchanged; the heal above is explicit and, running later in the same
request, sees the raised maximum. Any removal (explicit, clock or round expiry,
replacement, anchor release, floor release, Battle ending) lowers the maximum by
the same amount and clamps current down to it.

A drain lowers the maximum before the instance joins the active set and clamps
current down to it. It may take at most `maximum - minimum - active raises` on
that Resource (E_CONDITION_AMOUNT otherwise), so `maximum - minimum` always
covers the active raises and every removal reverses exactly. A drain clamp that
brings current to the minimum is a debit: it releases `ends_at_minimum`
Conditions on that Resource before the ConditionApplied event. Because the
instance has not joined yet, that release cannot end it; if its own type ends at
that minimum or its anchor was released, validation refuses the application
(E_CONDITION). Any removal gives the drained amount back to the maximum and
leaves current unchanged. Replacement reverses the old instance before the new
one applies.

Removal never fails on this account, so an effect can always end. A raise's
lowered maximum stops at the Resource minimum, which only an inconsistent
declaration can reach. A clamp that brings current down to the minimum is a
debit: it releases `ends_at_minimum` Conditions on that Resource, before the
terminal removal event, as damage would. The maximum plus the active drains on a
Resource must fit int64, so every drain can be given back: a raise, an `expand`
and genesis are refused (E_OVERFLOW) when they would break that. Refusals happen
only at application or genesis: a missing Resource (E_RESOURCE), a buffer pool
(E_RULE), Battle health (E_RULE; its minimum drives Battle life),
`modifier_selection latest` (E_RULE; a stored maximum cannot follow a changing
winner), a zero or int64-minimum amount, mixed signs or a drain beyond its room
(E_CONDITION_AMOUNT) and overflow (E_OVERFLOW).

ConditionApplied and ConditionRemoved carry `resources`, one record per modifier
in ID order: modifier, Resource, signed amount in force, and complete
before/after Resource facts. Replay regenerates them exactly. Genesis Condition
declarations are already applied: declare the maximum including every active
instance's modifier, raise or drain, as status reports it, and never apply it
again.

## Explicit custody and location-bound storage

```nql
item "prayer" named "Prayer scroll" {
    owner "char:a";
    custody character "char:b";
}
item "chest" named "Hall chest" {
    container true;
    custody location "loc:hall";
}
place "prayer" from character "char:b" to item "chest" by "char:b";
place "prayer" from item "chest" to character "char:b" by "char:b";
```

Custody is optional; omission is unknown and historical operations never infer
it. Ownership, wearing and custody remain distinct. A location-bound container
is one unworn Item without equipment, anchored to its Location; contents derive
from their own custody. A container cannot be placed, consumed or nested.
`place` moves an entire positive-stock, unworn Item from its exact known custody.
Both endpoints and the handling Character must share a canonical Location.
Same-location handling is a coarse physical rule, not proof of touch distance,
visibility, consent or ownership. Host authorization remains separate.

Character/container endpoints accept their existing typed parameters in Ability
bodies and flat check branches; Location endpoints are literal. Item and handler
also accept literal/parameter forms. Example body:
`place param "item" from character param "actor" to item param "box" by param "actor";`
`require carried_item "prayer" by param "actor";` checks positive stock directly
carried by that Character; it does not establish a free hand or a particular
material's suitability. Transfer only changes ownership under its authored
equipment policy. Explicit placement supplies physical handing/storage.

Known custody must match wearer for Equip; retrieve first when stored. Unknown
custody retains historical Equip behavior. Unequip leaves custody unchanged.
Known-custody consumption requires direct custody at the stated owner. The
complete placement request publishes once, with typed ItemPlaced and original
intent/receipt; historical retries never repeat it. ItemsAt and the native
placed_items observation derive container/ground contents; holder inclusion in
EquipmentView covers borrowed unworn items. These are detached views. Existing
quantity bounds, archive/response limits and deterministic replay still apply.
Whole-stock placement does not implement partial-stock split/merge.

## Changing a worn item's mode

```nql
unequip "fighter-sword"; equip "fighter-sword" on "fighter" mode "two-handed";
```

`equip` takes an optional `mode`, a mode its item's definition declares
(E_EQUIPMENT `mode` at the token otherwise); without one the item is worn in
its definition's default mode. A worn item cannot be equipped again (E_WORN
`wearer`), so a Character changes how it holds or wears an item, such as a
Versatile Longsword in two hands, with `unequip` and `equip ... mode` in one
request. The request is one transaction:

- Custody is unchanged, and the new mode's slots are checked against capacity
  on the whole loadout. With a Shield in the other hand, two hands is refused
  (E_EQUIPMENT `capacity.hand`, expected 2, actual 3) and the item stays as it
  was.
- It records ItemUnequipped (`mode_before`) and ItemEquipped (`mode_after`).
  A mode's `unequip_ticks` and the new mode's `equip_ticks` pass as for any
  unequip and equip; a mode without them takes no time.
- The equipment view and the attack options view then show the item in its
  new mode.

# Battle turn Resource allowances

Battle rules may opt into up to eight distinct existing, non-health Resources:

```nql
turn_restore "turn:bonus";
```

Put this clause inside the existing `battle` declaration. The Resource must be
declared on at least one Character. At the beginning of the first and each next
standing actor's turn, restore that actor's matching Resources to their maxima,
through the existing RestoreResource semantics. Missing Resources are not created.
No restore occurs on inspection, save/resume or an action that leaves the turn
open. Declaration order determines restoration event order.

An Action can opt into a literal exact cost from one declared turn Resource:

```nql
action actor param "caster" phase either
    turn_cost resource "turn:bonus" by 1 finish false;
```

During a Battle, the actor must be standing and hold the current turn. The
positive exact cost runs before body step0 in the same candidate. It does not use
or reset the normal action's `ActionUsed` flag. `finish false` leaves the turn
open unless the encounter ends or the actor becomes unable to act; `finish true`
advances it. Outside Battle, this prefix costs nothing and the ordinary authored
body applies unchanged. Without `turn_cost`, all existing Action behavior remains.

Payment and restoration use the existing Resource events. The original invocation
remains the terminal event/receipt identity; the prefix never shifts body indices
or human roll indices. Human dice preview applies the same prefix and bounded
body work on a detached candidate. A failed body rolls back payment, Condition
cleanup, random state and turn progression together. Historical successful retries
return their original receipt without charging again. No reaction permission,
resource creation, alternate counter, or out-of-turn action is implied.

# Damage-triggered Condition checks

An optional Battle rule names a Condition type and existing target selectors:

```nql
condition_check "maintained" after_damage resource "hp"
    stat "save:constitution" sides 20 minimum 10 divide 2 maximum 30;
```

At most16 unique (Condition type,Resource) policies are allowed. The type must
exist; targets with that Condition must possess the named Resource and Stat.
Sides are2..1000, minimum/divisor positive int64, maximum at least minimum.
After positive actual Damage and Resource-floor cleanup, surviving matching
instances are checked in ID order. Threshold is actual damage divided by the
divisor (integer floor), bounded by minimum/maximum. The existing effective Stat
is added exactly to the raw face; success is total >= threshold. Natural faces
have no special rule here. Failure removes that exact anchored group. Spend,
Heal and zero actual damage do not trigger a check.

The affected target's existing Battle controller selects manual or recorded dice
(PartyRoot is the manual fallback outside initiative). Missing manual input is
a typed candidate need, with no published damage/RNG/turn. The trusted host
appends ordered `ConditionRollInput` evidence to the original Damage,
WeaponAttack or Invoke operation; models cannot submit these faces through NQL
source. Exact IDs/targets/ranges/order and full consumption are checked. Evidence
is bounded to128 entries/64KiB JSON and shares existing work/event/random bounds.
Check events retain the Condition, policy, actual damage, controller, raw draw,
modifier, threshold, result and random call positions. Replay derives manual
inputs from those events and re-evaluates the entire original transaction.

Native input questions retain the original request actor even if its responder
is the player being attacked. Save/resume retains pending input; accepted raw
faces followed by late failure become a durable rejected attempt, not another
chance to answer. Bounded host QuestionEvidence accompanies the resolved attempt
for dialogue recovery, outside core semantic request identity. Old worlds omit
these optional fields. This is an authored single-stat/single-die rule, not a
claim of general spell, reaction, advantage or bonus-die coverage.

## Condition checks outside Battle

```nql
condition_check "state:concentration" after_damage resource "hp" stat "save:constitution" sides 20 minimum 10 divide 2 maximum 30;
```

A world without a Battle declares the same policies at top level, with the
same bounds, as the Blueprint's `condition_checks`. With a Battle they are
E_RULE, since its rules hold its own. The compiler reports an undeclared type
(E_CONDITION_TYPE) and a repeated type and Resource (E_DUPLICATE) at the type,
an empty Resource or stat at it (E_ID), a die or DC bound out of range at the
number (E_RULE), and a seventeenth policy at it (E_LIMIT).

After positive actual Damage and Resource-floor cleanup, each surviving
instance of a checked type on the damaged Character, in ID order, makes a
StatCheck: the policy's stat and die against DC max(minimum, min(maximum,
damage / divisor)), with the stat's roll modes, exactly as `check` resolves.
An instance whose `ends_on_condition` type is held at the Resource's minimum,
or included in a type held there, has already ended. The player's faces come
with the damage:

```nql
damage "char:bard" resource "hp" by 12 faces "char:bard:concentration" 9;
```

`faces "<instance>" <face> [<face>]` may repeat, one clause per instance in
check order, and lowers to the operation's `condition_rolls`, each with the
damaged Character as target. Each face is 1 to 1000 (E_ROLL at the face), and a
clause has at most two (E_ROLL at `faces`). `faces` in a Battle world is E_RULE,
since there the trusted host supplies them; only `damage` takes the clause. An
Ability's damage outside Battle takes faces from its Invoke operation's
`condition_rolls` in IR.

An instance's check takes the next faces that name it, at most two, and they
must match the net mode and the die (E_ROLL, whose subject is the instance and
whose message ends with the DC). Without faces the Adventure or dice random
state rolls; with neither the check is E_ROLL, as for `check`. Faces that no
check takes are E_ROLL ("faces only for a triggered check"), and nothing
commits.

The check is a `StatCheckResolved` event before the Damage event. Its
`stat_check` adds `condition`, the instance, and `damage`, the actual damage.
Failure removes the instance and its anchored group, the instance's removal
annotated `check_failed`, which nql-apply's `ended_conditions` reports as reason
`check`. Replay takes the supplied faces back from the check events' intents.
docs/CHARACTER_STATS.md declares concentration this way.

# Authored Location separation bounds

Inside Battle rules, optionally declare a maximum separation in `grid` units:

```nql
location_span "loc:hall" maximum 30;
```

At most128 unique existing Locations can have a positive int64 span. An Action's
`within` requirement uses actual current encounter distance first. If exact
distance is unknown, a shared Location span no greater than the required range
is sufficient evidence. A30-foot span can prove within60; it cannot prove within5.
Different Locations or no sufficient span remain unknown. Self-distance stays0.
Spans are immutable authored bounds, never exact positions or visibility facts.
Begin rejects a formation whose Chebyshev extent exceeds that Location's span
before initiative input/RNG. No movement, sight, range fallback to names, or
second writable spatial state is introduced. Old worlds omit the optional field.

# Exact partial stock

```nql
split "item:rations" quantity 2 into "item:portion:1" by "char:handler";
place "item:portion:1" from character "char:handler" to item "item:chest" by "char:handler";
```

`split` is a direct operation. The positive int64 amount must be strictly less
than current quantity; use Place for the whole stock. Source is unworn, not a
container, with known custody at the handler's Location. An instance-owned worn
Effect targeting the stock blocks splitting; shared Equipment definitions are
retained. Neither ownership nor permission is inferred from custody.

The portion gets a new persistent Item ID and copies source properties. Its
`split_from` field records the immediate parent, assigned only by Split; direct
CreateItem cannot forge it. Source retains the positive remainder. No merge by
name or descriptive equality occurs. Parent IDs remain present after depletion;
initialization validates existing acyclic references. The typed ItemSplit event
retains complete before/after/created facts and handler/quantity; exact replay
reconstructs the original operation. This is consistent history, not proof that
trusted genesis or supplied historical input is authentic.

Compiler makes the fresh Item type available to later statements in the same
request, without simulating owner, custody or quantity. Native use requires an
ID under `next_item_prefix`; failed native attempts still advance allocation,
whereas successful historical retries return their original complete receipt.
Existing entity, operation, event and archive bounds apply. Old source/archives
omit the new fields; keep a matching runtime to read histories using Split.

### Permanent resource capacity growth (native advancement dependency)

```nql
expand "character:a" resource "hp" by 6;
// Inside an Ability or its existing flat Check branches:
expand param "target" resource "hp" by param "gain";
```

The character and Resource must exist; the selector is literal and the amount
is a positive int64. `expand` increases the canonical maximum exactly, rejecting
overflow. Current and minimum are unchanged. It never heals, restores slots,
revives a character, or creates a new Resource. A later operation in the same
candidate sees the new maximum; any later rejection rolls everything back.
ResourceExpanded has a typed capacity payload with requested growth and complete
before/after Resource facts. Existing invocation provenance, work bounds, cloning,
receipts and replay apply. Class/level eligibility and consent belong to authored
rules and the host, not to a built-in class system or arbitrary sheet postimages.
The native gameplay host does not yet admit a new advancement route merely
because this typed operation exists.

### Effective-stat action eligibility

```nql
require stat_between param "actor" stat "experience" min 300 max 9223372036854775807;
require stat_between "char:a" stat "level" min 1 max 1;
```

This bounded action prerequisite accepts a character literal or parameter, a
literal Stat selector, and inclusive signed-int64 literal bounds (`min <= max`).
It reads the existing effective-stat evaluator, including applicable Conditions,
equipment and authored derived recipes, against the ordered candidate at Ability
entry. It does not read a base-only value or perform a roll. Earlier operations
in the same request are visible; failure publishes none of the request.
Missing Stat/type references fail with the normal semantic/Ability context.
A successful historical retry returns its original receipt before checking
current eligibility. Both Adventure and Battle profiles use this same predicate;
the existing requirement/work bounds and all other phase/control rules remain.
XP, level and their permitted contributors are authored content, not core laws.

### Ordinary Battle action continuation

```nql
action actor param "actor" phase encounter finish false;
```

An ordinary Battle Action may explicitly keep its current turn window after
the body. It still consumes the one normal action; later normal actions reject.
Other legal options (for example a separately paid bonus action) and explicit
EndTurn can follow. Downed actors and terminal outcomes settle normally.
Absent `finish`, the historical ordinary-action auto-finish remains. `finish
true` explicitly selects it. The optional policy is Battle-only, mutually
exclusive with `turn_cost` (which already has its own finish setting), and
does not affect exploration execution. No new action allowance is granted.

### Recorded capacity rolls

```nql
roll expand param "target" resource "hp" dice 1 sides 10 min 1 bonus from param "target" {
 constant 0;
 term stat "constitution" offset -10 divide 2;
};
```

The existing top-level rolled Resource consequence also supports `expand`.
Its required positive literal `min` floors the requested maximum increase;
it does not change the Resource's own minimum. The same raw dice group and
recipe are recorded. A final sum outside int64 rejects; otherwise the positive
floor applies, then exact Expand changes maximum only. Current, minimum and life
state stay unchanged. The roll result's `actual` is maximum delta for this kind,
and its additional `minimum` field records the authored floor. A heal result
records `minimum` only when its step declares a floor; damage results omit it
and keep their zero-floor/current semantics.

This reuses the same one-group bound, Action admission, raw human input or
recorded NPC random stream, candidate transaction, questions, and replay. It
does not define a class, choose the player's advancement method, grant a rest,
or turn a fixed increase into a historical die roll. Native host admission
allows growth only through exploration actions with the affected party members'
permission. It cannot be used as a direct arbitrary stat/Resource edit.

### Authored capacity formulas

```nql
expand param "target" resource "hp" by formula from param "target" min 1 {
 constant 4;
 term stat "constitution" offset -10 divide 2;
};
```

An Ability may choose this formula instead of Expand's literal/int-parameter
amount. Target and source are character literals/parameters; Resource, positive
minimum and bounded ScalarRecipe are authored literals. The existing evaluator
reads non-derived effective Stat inputs at this body step, including earlier
candidate changes. It checks the final raw recipe result fits int64, then floors
it at the authored positive minimum. The existing exact maximum-only expansion
rejects overflow and preserves Resource current/minimum and life outcomes.

The optional typed event explanation records source, raw result, minimum and
recipe terms; it is a deterministic calculation, never a die roll. Original
invocation, receipts, replay, detachment and work bounds apply normally. Formula
is currently top-level only and rejects inside a Check branch. Literal/int-
parameter Expand still works in its existing flat branches. No direct formula
operation or general arithmetic expression is introduced.

### Selected extra-action allowances

Within Battle rules:

```nql
allowance "turn:extra" weapon true { action "ability:permitted"; };
```

An allowance names a canonical Resource with minimum0; it cannot be health or
also named by `turn_restore`. At most8 unique allowances, each admitting weapons
and/or up to128 unique registered ordinary Battle Ability IDs. Bonus/TurnCost
and exploration-only actions cannot be admitted. This explicit vocabulary belongs
to authored rules; no class name or Magic category is an engine law.

```nql
invoke "ability:permitted" allowance "turn:extra" { "actor" = character "char:a"; };
battle attack "encounter" round 1 cursor 0 by "char:a" using "grant"
 target "char:b" finish false allowance "turn:extra" {};
```

An explicit selection spends exactly1 through the existing Spend operation before
body/weapon evaluation, preserves the normal ActionUsed fact, and remains in the
original intent/receipt. It requires the actor's active turn. Omission preserves
all old normal-action semantics. It never falls back to another payment source.

Existing Restore/Heal may grant a balance under authored policy; for example a
limited-use Ability can spend its persistent use and restore its own allowance
atomically. At turn start/end and terminal settlement, positive current allowance
is cleared to0 through the existing non-health Damage transition (including
Condition consequences). A positive clearing amount is representable because
minimum is0. No separate counter or evaluator exists. All work remains bounded.
Input preview evaluates the same payment candidate; failed or incomplete input
publishes nothing. Historical receipts, replay and detached nested rules retain
these selections. Old JSON omits the new optional fields.


### Selected weapon hit Conditions and Battle lifetimes

A Battle declaration may contain up to32 uniquely named, immutable policies:

```nql
hit_condition "sap" definition "longsword" mode "one" attack "slash"
 requires "mastery:longsword" apply "state:sapped";
```

Both Condition types and the exact equipment attack must exist. The consequence
uses fixed Condition modifiers (no magnitude argument). The optional attack
selection follows `finish` and any `allowance`:

```nql
battle attack "encounter" round 1 cursor 0 by "char:a" using "grant-id"
 target "char:b" finish false condition "sap" as "fresh-instance" {};
```

The actor must have the required training and the selected grant must match the
policy. Active or retired instance identities reject before input. A hit applies
the existing Condition with the attacking Character as source, in the same
candidate transaction, including a hit dealing zero damage. A miss applies no
Condition and reserves no lifetime identity. No policy is selected implicitly.

```nql
condition type "state:sapped" {
 instances coexist;
 until_source_turn;
 ends_after_attack;
}
```

These optional policies require an active Battle containing the target.
`until_source_turn` additionally requires a participating Character source and
forbids Clock/round durations. It ends at the source's next turn start, including
a skipped downed turn. `ends_after_attack` ends after the target's next completed
weapon attack roll, including misses and advantage/disadvantage cancellation.
Terminal settlement removes all such Conditions. Existing Condition group
removal, effects, lifetime reservations and work bounds apply. Typed ending
records retain trigger and Battle window. Future non-weapon attack executors
must honor this policy before admission; no spell-attack support is implied.

Native host creation IDs use the current request allocation. Resume retains
pending IDs and raw faces. Named Load branches its pending creation operands to
the new allocation while retaining selected mechanics and accepted faces. Its
existing fork record identifies the source generation containing original IDs.
Committed historical requests/receipts are never rewritten.

## Exact Condition references in Abilities

`condition_id` remains a creation identity. `condition_ref` is a distinct typed
argument naming an existing Condition instance, resolved against the ordered
candidate rather than rebound by a native saved-game branch. For example:

```nql
ability "untrain" named "Remove selected training" {
 parameter "actor" character;
 parameter "old" condition_ref;
 remove condition param "old" of "training:chosen" from param "actor";
}
invoke "untrain" {
 "actor" = character "char:tess";
 "old" = condition_ref "training:tess:chosen";
};
```

Definition and invocation are separate source units. The definition requires a
literal declared type after `of` and a typed target. Instance/target operands may
be literals or matching parameters. The same removal is permitted in a flat Check
branch. It removes precisely the selected instance and the existing anchored
dependents, preserving other same-type instances and all retired identity truth.
A missing instance, wrong target/type or later failure rejects the whole request.

Direct `remove condition "instance" from "character";` retains its behavior;
optional `of "type"` before `from` adds the same guard. The optional guard is part
of request identity and the root event's `expected_type`, including replay. An
omitted guard is not inferred into old request/event records. Successful historical
retry returns its original receipt without resolving the now-removed instance.
Native allocation changes only creation IDs; existing `condition_ref` values stay
unchanged on pending Save/Resume/named Load. This is not removal by type or a
mutable reference to an arbitrary object.


## Direct stock addition

`add "item:quiver" to "char:eirik" by 10;` adds a positive int64 amount to
an existing stock, including a depleted stock at zero. The stated character must
be its current owner; containers, incompatible known custody and worn items are
refused. Unspecified legacy custody follows consume's existing policy. Overflow
is refused with E_QUANTITY. One ItemAdded event retains item, owner, requested
amount and before/after quantity in its `addition` record (the same shape as
consumption). Identity and other item facts stay unchanged. The enclosing request
is atomic and historical retries never add twice. This verb is direct action
syntax only, not an Ability step or host authorization.

## Authored stock consumption

The existing exact stock debit may appear in an Ability body or a flat Check
branch. Item, owner and amount accept their typed literal/parameter forms:

```nql
consume "item:vial" from param "actor" by 1;
consume param "stock" from param "owner" by param "units";
```

Amount is a positive int64. Binding produces the existing ConsumeItem operation;
ownership, custody, wearing and sufficient quantity are checked against the ordered
candidate. Depletion retains the original ItemID. This does not imply healing,
a new item kind, an automatic transfer, or host permission. Authors compose any
consequences using existing operations, with one enclosing invocation/receipt and
atomic rollback. Direct consumption semantics are unchanged, including older
Items with unspecified custody. An authored `require carried_item ... by ...` imposes
the stricter explicit-carriage eligibility when needed.


## Native authored encounter declarations

Inside the existing `battle` block, after its required policy fields:

```nql
encounter "encounter:quay" named "Quay confrontation" at "loc:quay" {
 protected at 0 0;
 protected at 0 1;
 opposition "char:watcher" at 1 0;
};
```

These immutable definitions reuse native Battle admission, initiative, actions,
turns, outcomes and replay. They do not use Alpha's separate adventure encounter
semantics. At most 16 unique encounter IDs are declared. Each has a nonempty
UTF-8 label (up to 4096 bytes), existing Location, at least one protected square
and one existing Character opponent, and at most 16 total slots plus opponents.
Coordinates are -1024..1024, every square is distinct and must fit any authored
location span. Opponents are unique within a definition; labels may duplicate.

The current party fills protected squares: root first, then other members sorted
by stable identity. Opponents follow declaration order; these admissions fix tie
ranks and the order of agent random draws. Every party member must fit. Unused
protected slots remain reserved empty squares. The root is human-controlled;
other participants are agent-controlled. Current absence, inability, party changes
or battle/history status can make a valid declaration unavailable without making
unrelated World mutations invalid.

For a nonempty catalog, `battle begin` must match the selected ID and exact ordered
formation. Raw human initiative is still supplied through the existing input
contract. No member, opponent, controller, square, location or tie rank may be
substituted. Derived offers are advisory; the runtime rechecks the current candidate.
Successful historical retries still return their original complete receipt first.

An omitted catalog preserves historical direct Battle semantics and replay. The
native host offers no invented encounter for such a save, but retained pending
initiative and active battles continue. Declarations are new-genesis content, not
an edit or migration of existing saves. No spawn, respawn, scaling, cooldown or
procedural selection is inferred. Worked encounter examples are under `examples/`.

## Character-local resource buffers

A character may declare `buffer "temp-hp" before "hp";` beside its resource
fields. Both resources must already be declared in that character block (forward
references within the block are allowed). They must differ. Pool minimum is zero;
ordinary declared maximum remains its numeric bound. Relations are immutable,
one pool per target and one target per pool, without chains/cycles. Battle health,
turn-resource and action-allowance pools cannot themselves serve as buffer pools.
Action allowances cannot be buffer targets either: automatic turn clearing must
not be absorbed.
No identifier such as `hp` or `temp-hp` is built in.

```nql
character "char:eirik" named "Eirik" at "loc:hall" {
    resource "hp" = 20 min 0 max 20;
    resource "temp-hp" = 5 min 0 max 1000000;
    buffer "temp-hp" before "hp";
}
```

`grant "char:eirik" resource "temp-hp" by 8;` is a direct operation with a
positive int64 amount. Only declared buffer pools accept grants. Current becomes
`max(current, amount)`; smaller/equal grants still commit a ResourceGranted event.
A grant exceeding the declared maximum fails E_BOUNDS, rather than silently
clamping. Use a suitable declared bound (up to MaxInt64); this is not an unbounded
numeric domain. Grant is not yet admitted in Ability bodies.

Damage to the target first drains its pool, then clamps the remaining damage
against the target's existing minimum. The terminal DamageApplied event retains
the original request and target before/after; its optional `buffer` record retains
pool ID, before/after and absorbed amount. With hp20 and pool5, damage8 records
pool5->0, absorbed5 and hp20->17. Existing Ability and weapon damage use this same
path. Weapon-result `Actual` remains target-resource loss; absorption is reported
in the resource event. Damage-triggered condition checks count absorption plus
actual target loss. Life transitions use the target resource only.

Heal, spend and expand on a pool fail E_RULE. Restore on a pool clears it to zero;
restore/heal on the protected target remain ordinary operations and do not refill
its pool. Explicit damage to the pool can deplete it directly. Pool depletion and
clearing run existing minimum-triggered condition cleanup before the terminal
event; target cleanup follows pool cleanup. Such authored consequences can add
events, but pool/target changes themselves use one damage event and one commit.
Historical retries return the complete original receipt without granting/draining
again. Character status includes detached buffer declarations. Worlds without a
buffer clause preserve their existing behavior and serialized optional-field shape.

There is no automatic long-rest or source-expiration link in this primitive:
a trusted authored/host action explicitly restores the pool when required. This
is a bounded generic buffer, not a complete SRD temporary-hit-point rules package.

## Excluding Condition on derived-stat terms

```nql
derive stat "skill:arcana" {
 term stat "intelligence" offset -10 divide 2;
 term stat "level" offset 7 divide 4 requires "prof:skill:arcana";
 term stat "level" offset 7 divide 8 requires "feature:jack-of-all-trades" unless "prof:skill:arcana";
}
```

A recipe term in an equipment `derive stat` rule, item mode or capacity formula
may end with `unless "<condition type>"`, after any `requires`. The term counts
only while its character lacks that Condition type; with `requires` as well, it
needs the first type and lacks the second. The type must be declared and differ
from the term's `requires`; otherwise E_EQUIPMENT at the `unless` string (in an
Ability formula, the Ability error names operand `formula.term.<i>.unless`).
The recipe explanation reports the term inactive while the type is present. A
term without `unless` serializes exactly as before. docs/CHARACTER_STATS.md uses
this for Jack of All Trades.

## Slot-gated recipe terms

```nql
condition type "feature:unarmored-defense:monk" { instances unique; }
condition type "feature:unarmored-defense:barbarian" { instances unique; }
condition type "training:shield" { instances unique; }
equipment "nql-equipment-v1" {
 slot "hand" capacity 2;
 slot "body" capacity 1;
 slot "shield" capacity 1;
 derive stat "defense" {
  term stat "dexterity" offset -10 divide 2;
  term stat "wisdom" offset -10 divide 2 requires "feature:unarmored-defense:monk" unless slot "body" unless slot "shield";
  term stat "constitution" offset -10 divide 2 requires "feature:unarmored-defense:barbarian" unless slot "body";
 }
 definition "shield" named "Shield" {
  description "A shield.";
  default "held";
  mode "held" { occupy "hand" by 1; occupy "shield" by 1; modifier "shield" stat "defense" add 2 requires "training:shield"; }
 }
}
character "tess" named "Tess" at "yard" { stat "dexterity" = 16; stat "wisdom" = 16; stat "constitution" = 12; stat "defense" = 10; }
condition "tess:unarmored" of "feature:unarmored-defense:monk" to "tess";
condition "tess:shield" of "training:shield" to "tess";
item "tess-shield" named "Shield" { owner "tess"; wearer "tess"; definition "shield"; mode "held"; }
```

A recipe term in an equipment recipe (a `derive stat` rule, an item mode's
`base stat` recipe, or an equipment attack's hit or damage recipe) may end with
any number of `unless slot "<slot>"`, after its `requires` and `unless`. The
term counts only while no item its character wears occupies one of those slots,
as an attack gate reads them. The slots must be declared and unique, at most 8
(E_EQUIPMENT `unless_slots` at the slot). An Ability reads no worn equipment, so
a roll bonus or capacity formula term with a slot is refused (E_EQUIPMENT
`unless_slots`; in a formula the Ability error names operand
`formula.term.<i>.unless_slots`). The recipe explanation reports the term
inactive while such an item is worn. A term without a slot serializes exactly
as before.

Above, Tess is a Monk with a shield, so her Wisdom term is inactive: defense
10 + 3 + 2 = 15. Without the shield she has 10 + 3 + 3 = 16. Body armor with a
`base stat` recipe replaces this one anyway; the `body` slot also covers a worn
body item without one. The Barbarian's term names only `body`: a Barbarian
keeps the bonus with a shield.

## Bounded Condition modifiers

```nql
condition type "grappled" { instances coexist; modifier "speed" stat "speed" at most 0; }
condition type "gauntlets" { instances unique; modifier "strength" stat "strength" at least 19; }
condition type "belt" { instances unique; modifier "strength" stat "strength" at least magnitude; }
```

A stat modifier may end with `at least <amount>` or `at most <amount>` in place
of `add <amount>`; the amount is an int64 literal or `magnitude`. A stat's
effective value is its base, recipe terms and additive contributions summed
exactly, then raised to the highest `at least` amount in force, then lowered to
the lowest `at most` amount in force, so a ceiling wins over a floor. The
int64 check follows the bounds. Derived terms read bounded inputs.

A bound is not an Effect. It generates no Effect ID and is absent from
`active_effects`, from ConditionApplied and ConditionRemoved effect lists and
from nql-apply `ended_conditions` modifiers. The stat explanation lists every
bound in force under `bounds`: `instance`, `type`, `modifier`, `bound`
(`at_least` or `at_most`), `amount`, and `applied`, true for a bound that set
the total. Under `modifier_selection latest`, a suppressed instance's bounds
are omitted with its contributions. A Resource maximum modifier has no bound
form (E_RULE, field `bound`, for IR that sets one). A bound on a stat
serializes as `"bound"` beside `fixed` or `magnitude`; a modifier without one
serializes exactly as before.

## Added-die Condition modifiers

```nql
condition type "spell:bless" { instances coexist; modifier "bless" stat "bonus:saves" die 1 4; }
condition type "spell:guidance" { instances coexist; modifier "guidance" stat "skill:athletics" die 1 4; }
```

```nql
check "char:fighter" stat "skill:athletics" die 20 against 15 faces 10 added "spell:guidance" 3;
```

A stat modifier may end with `die <count> <sides>` in place of `add <amount>`:
a die every check of that stat adds after its d20 is kept (SRD 5.2.1 Bless and
Guidance: 1d4). The count is 1 to 10 and the sides 2 to 100 (E_RULE, field
`die`, at `die`). A die has no amount, bound or roll, and no Resource maximum
form (E_IR or E_RULE for IR that combines them); a held type carries none
(E_RULE, field `held_at_minimum`).

A die never changes the stat's value: it is not an Effect, has no
contribution, and is absent from `active_effects`. It reaches a check as a roll
mode does: by a modifier on the checked stat, or on a stat an active term of
its recipe reads, so a die on `bonus:saves` reaches every save. A Condition
type adds at most one die to a check, however many instances are in force (the
same effect does not combine with itself); the first in instance, type and
modifier order counts. Different types each add theirs. Under
`modifier_selection latest`, a suppressed instance's die is omitted. The stat
explanation lists the dice in force under `added_dice`: `instance`, `type`,
`modifier`, `stat`, `count` and `sides`, omitted when there are none.

A check's `added "<condition type>" <face>...` clauses, after `faces`, supply
the faces of a die in force; each type once, 1 to 10 faces of 1 to 100 (E_ROLL
at the clause or face). A die without them draws its faces from the same
random state as the d20, after the d20's faces, so a host may supply the d20
and leave the die to the world. At execution the faces must number the die's
count and lie on its sides (E_ROLL, field `added`, whose Expected names the
range or the count), and a clause for a type with no die in force is refused
(E_ROLL, field `added`, Actual the type). An automatic failure rolls no die
and refuses any clause. A damage-triggered check (Condition checks outside
Battle) has no clause: its dice draw. The total is the kept face, the stat's
effective value and every die's sum, exact with an int64 check. The check
record lists each die under `added`: the source fields, `faces`, `drawn` when
the world rolled them, and `sum`. A check with no die in force serializes
exactly as before.

## Held Conditions

```nql
condition type "unconscious" { instances coexist; held_at_minimum "hp"; modifier "speed" stat "speed" at most 0; }
```

`held_at_minimum "<resource>";` makes the type present, without an instance, on
every Character that declares that Resource while its current is at its
minimum. It has no ID, lifetime or event, begins and ends with the Resource,
and is listed in status `held_conditions` as `{type, resource}`, apart from
`conditions`. Its bounds name `held_at_minimum` in place of `instance`.

Presence tests see a held type as they see an instance: recipe `requires` and
`unless`, Ability `has_condition` and `lacks_condition` requirements, equipment
requirements and `ends_on_condition`. An instance whose `ends_on_condition`
names a held type is released by any debit that brings the Resource to its
minimum, before the Resource event, annotated `minimum_resource` like an
`ends_at_minimum` release. Such an instance cannot be applied, or declared at
genesis, while the type is held (E_CONDITION). Battle roll, damage and check
policies cite an instance, so they match instances only.

Instances of a held type still apply and remove as usual and follow its
instance policy; held presence is not an instance. Its modifiers must be fixed
bounds (E_RULE, field `held_at_minimum`, at the modifier): held presence has no
magnitude, and a bound cannot double count with an instance of the same type.
A type cannot declare both `held_at_minimum` and `ends_at_minimum` (E_RULE).
docs/CHARACTER_STATS.md uses both forms for the SRD conditions.

## Included Condition types

```nql
condition type "stunned" { instances coexist; includes "incapacitated"; }
```

`includes "<type>";`, repeatable, makes the named type present wherever this
type is: on the target of each instance, and on every Character where this
type is held. The presence tests that see a held type see an included type the
same way, and `ends_on_condition` sees it both when an including instance is
applied and when an including held type begins at a Resource's minimum.
Inclusion adds presence only: no instance, event or modifier. The included
type's modifiers reach a Character only through its own instances, and status
lists the including instance or held type alone. Damage-triggered checks, like
Battle roll and damage policies, match an instance's own type.

At most 8 per type (E_LIMIT). Each must be a valid type other than this one,
named once (E_CONDITION_TYPE), and declared (E_CONDITION_TYPE), and must
include no type itself (E_RULE), so inclusion is one level deep and cannot
cycle. The compiler reports each at the named type, and the limit at the
declaring one. In IR the list is the definition's `includes`, which the world
keeps sorted.

## Attacks from Condition types

```nql
condition type "srd:unarmed-strike" {
 instances unique;
 attack "unarmed" named "Unarmed Strike" {
  hit { constant 0; term stat "strength" offset -10 divide 2; term stat "proficiency" offset 0 divide 1; }
  damage "bludgeoning" flat { constant 1; term stat "strength" offset -10 divide 2; }
  melee reach 5 stat "strength";
 }
}
condition "tess:unarmed-strike" of "srd:unarmed-strike" to "tess";
```

A Condition type may declare attacks, with the grammar of an equipment mode's
`attack`: a feature, not an item, grants them. A Character on whom the type is
present has one attack grant per attack, evaluated for that Character: present
by an instance, by an including type, or held at a Resource's minimum, as
`requires` sees it. The grant comes and goes with the type's presence.

- The grant is listed in the equipment view's `attacks` beside the worn items'.
  Its `origin` is `{"condition": "<type>", "member": "<attack>"}`, with no item,
  definition or mode. Its ID is `condition:grant:<hash>` of the Character, the
  type and the attack, so the same type on two Characters gives two grants.
- `battle attack ... using "<grant>"` takes it as it takes an item's. A Battle
  `hit_condition` names an item's definition, mode and attack, so it never
  matches a Condition grant (E_BATTLE `hit_condition.rule`).
- A ranged attack needs the Battle's ranged rules, and its ammunition is the
  actor's stock, as for a bow.
- Battle's bound of 200 damage dice after a critical's multiplier covers a
  Condition type's melee and ranged attacks as it covers an item's (E_BATTLE
  `critical dice`).
- At most 8 attacks per type (E_EQUIPMENT `attacks`). Each is checked as an
  equipment attack (E_EQUIPMENT `attack`, `melee`, `requires`, `input`), and a
  world that declares any needs the equipment block (E_EQUIPMENT `attacks`,
  `Equipment rules`). The compiler reports each at the attack.
- In IR the list is the definition's `attacks`, each an equipment attack.

## Ammunition named by item type

```nql
ranged normal 150 maximum 600 stat "dexterity" ammunition from "srd:arrow";
```

```nql
item type "srd:arrow" named "Arrow" { kind "ammunition"; }
item "tess:arrows" from "srd:arrow" { quantity 20; owner "tess"; custody character "tess"; }
```

A ranged attack's ammunition is one original stock, `ammunition "<item>"`, or
any stock of an item type, `ammunition from "<item type>"`. A shared
definition, such as a catalog Longbow's, can then shoot each Character's own
arrows.

- The type must be declared and have no equipment (E_EQUIPMENT `ranged`; the
  compiler reports it at the type, field `ammunition type`). Whether it is the
  ammunition the weapon prints is the content's choice.
- A shot takes a stock whose original root is declared `from` the type: the
  stock itself, a split of it, or arrows recovered from it. The root must be
  plain stock with a quantity, and the shot stock positive, unworn, owned and
  carried by the shooter (E_BATTLE `ammunition`).
- The shot is recorded and recovered under that root, as a shot of
  `ammunition "<root>"` is, so two stocks of one type are two origins. The
  equipment view's `ammunition_origin` names the root for every such stock.
- In IR the profile has `ammunition_type` in place of `ammunition_origin`;
  exactly one of the two is set.

## Thrown attacks

```nql
attack "thrown" named "Dagger (thrown)" {
 hit { constant 0; term stat "strength" offset -10 divide 2; term stat "proficiency" offset 0 divide 1; }
 damage "piercing" dice 1 sides 4 { constant 0; term stat "strength" offset -10 divide 2; }
 thrown normal 20 maximum 60 stat "strength";
}
```

An equipment attack, or a Condition type's, may end with a thrown profile in
place of `melee` or `ranged`: `thrown normal <n> maximum <m> stat "<s>";`. The
weapon is its own projectile, so there is no ammunition. A weapon that can be
thrown declares a thrown attack beside its melee one, with the same hit and
damage recipes when the rules use the same ability for both.

- The grant is listed in the equipment view's `attacks` with
  `"thrown": {"stat", "normal", "maximum"}` and its numbers, like any grant.
- Battle does not execute a thrown attack: `battle attack ... using` its grant
  is refused (E_BATTLE `grant`). Throwing in Battle would move the weapon to
  the target's square and into recovery, which is not built.
- 1 <= normal <= maximum <= 1,000,000, and the wearer must have the stat
  (E_EQUIPMENT `thrown`, E_STAT). A profile is one of `melee`, `ranged` and
  `thrown`. The compiler reports a range at its number.
- In IR the attack and its grant have `thrown`.

## Gated attacks and die upgrades

```nql
condition type "feature:martial-arts" { instances unique; }
condition type "feature:martial-arts-d8" { instances unique; }
condition type "feature:martial-arts-d10" { instances unique; }
condition type "monk:unarmed" {
 instances unique;
 attack "unarmed-dex" named "Unarmed Strike (Dexterity)" requires "feature:martial-arts" unless slot "body" unless slot "shield" {
  hit { constant 0; term stat "dexterity" offset -10 divide 2; term stat "proficiency" offset 0 divide 1; }
  damage "bludgeoning" dice 1 sides 6 { constant 0; term stat "dexterity" offset -10 divide 2; }
   or dice 1 sides 8 requires "feature:martial-arts-d8" unless slot "body" unless slot "shield"
   or dice 1 sides 10 requires "feature:martial-arts-d10" unless slot "body" unless slot "shield"
  melee reach 5 stat "dexterity";
 }
}
condition "tess:unarmed" of "monk:unarmed" to "tess";
condition "tess:martial-arts" of "feature:martial-arts" to "tess";
condition "tess:martial-arts-d8" of "feature:martial-arts-d8" to "tess";
```

An attack, in an equipment mode or a Condition type, may carry a gate after
its name: `requires "<type>"` and any number of `unless slot "<slot>"`. Its
grant exists only while the gate admits the holder (the wearer, or the
Character the Condition type is on): the holder has the type, by an instance,
an including type or a held floor, as `requires` sees it elsewhere, and no
item it wears occupies a named slot. A shut attack is not listed, and
`battle attack ... using` its grant is refused (E_BATTLE `grant`).

After the damage recipe, `or dice <c> sides <s> requires "<type>"`, with its
own `unless slot` clauses, adds a die upgrade, such as a Monk's Martial Arts
die. The grant rolls the largest die whose gate admits the holder, among the
base and the upgrades, by count x sides, ties keeping the earlier. The bonus
recipe is the same for every die. The grant's `damage_count` and
`damage_sides` are the die chosen, and Battle rolls it. Above, Tess has
Martial Arts and the d8 tier, so her grant rolls 1d8; with a shield, the
attack is not listed.

- The engine lists grants and the better die of each; the player and the
  host still choose which attack to make. A two-handed Spear's 1d8 and an
  unarmed d6 are two attacks, both listed.
- nql-apply's `attack_options` view lists the grants of every attacking
  mode of each item a Character wears or carries, the modes it does not
  wear included, gated as here (ATTACKS.md, "Attack options").
- A gate's type must be declared, and its slots declared and unique, at most
  8 (E_EQUIPMENT `requires`, `unless_slots`). An upgrade must name a type
  (E_PARSE without one); at most 8 upgrades, each 1..100 dice of 2..1000
  sides (E_EQUIPMENT `upgrades`, `upgrade`). Battle's 200 critical dice
  bound counts the largest die. The compiler reports each at its token.
- In IR an attack and an upgrade carry `requires` and `unless_slots`, and the
  damage carries `upgrades` (`count`, `sides` and the gate).

## Roll modifiers

```nql
condition type "poisoned" { instances coexist; modifier "checks" stat "bonus:checks" roll disadvantage; }
condition type "unconscious" { instances coexist; held_at_minimum "hp"; modifier "save" stat "save:dexterity" roll fail; }
```

A stat modifier may end with `roll advantage`, `roll disadvantage` or `roll
fail` in place of an amount. It changes no total and generates no Effect. It
sets how a StatCheck on a stat rolls when the check's stat is the named stat or
an active term of its derived recipe reads the named stat, so a mode on a pool
reaches every total that adds the pool. A held type may carry roll modifiers
beside its fixed bounds. In IR, `"roll"` replaces `fixed` or `magnitude`: IR
with both is E_IR (field `amount`). A roll with a bound, or a mode other than
the three, is E_RULE (field `roll`); a Resource maximum modifier has no roll
(E_RULE, field `bound`).

The stat explanation lists the modifiers in force under `roll_sources`:
`instance` or `held_at_minimum`, `type`, `modifier`, `stat` (the named stat)
and `roll`, held types first, then by instance, type and modifier. Under
`modifier_selection latest` a suppressed instance's modes are omitted. `roll`
is the net mode: `fail` if any source fails; otherwise `advantage` or
`disadvantage` when only that one is present; omitted when neither is present
or both are, since they cancel.

## Stat checks and dice

```nql
dice seed 20 25;
```

```nql
check "char:fighter" stat "skill:athletics" die 20 against 15 faces 17 6;
check "char:rogue" stat "initiative" die 20 advantage;
```

`dice seed <int64> <int64>;` in a world declares a PCG random source, seeded
as a Battle's is; each seed is a nonnegative literal. At most one per world
(E_DUPLICATE), and not with an Adventure or a Battle, whose random state serves
instead (E_DICE, at `dice`). Genesis accepts zero dice state or the exact
initial continuation of the seed.

`check "<character>" stat "<stat>" die <sides> [against <int64>] [advantage |
disadvantage] [faces <int64> [<int64>]] [added "<condition type>" <int64>...]...;`
is an action, in this clause order. It lowers to the `stat_check` operation
`{character, stat, sides, dc, mode, faces, added}`; `added` is described under
"Added-die Condition modifiers". The character must declare the stat (E_STAT at the stat, E_REFERENCE
at the character); sides are 2 to 1000 (E_ROLL at the die size); each face is
1 to sides and there are at most two (E_ROLL at `faces`).

Execution explains the stat, combines the requested mode with its roll sources
as the explanation does, and requires the faces to match the net mode: none for
`fail`, two for `advantage` or `disadvantage`, otherwise one (E_ROLL, whose
Expected names the count and mode). Without faces it draws them from the
Adventure, Battle or dice random state, in that order (E_ROLL when there is
none, whose Expected names the count and adds "or a world random source"). The
kept face is the higher with advantage and the lower with disadvantage; the
total is the kept face plus the stat's effective value, exact with an int64
check (E_OVERFLOW), as is the margin, total minus DC. Success is total >= DC. A
`fail` draws nothing and has no total; with a DC it has success false and no
margin. Without a DC there is no success or margin.

The operation changes only the random state. Its event is `StatCheckResolved`
with `stat_check`: `intent` (the operation), `roll`, `roll_sources`, `drawn`,
`faces`, `kept`, `bonus` (the effective value), `total`, `success` and
`margin`; absent parts are omitted. Replay rebuilds the operation from
`intent`. nql-apply lists the request's results, in order, under `checks`.

## Step tables on recipe terms

```nql
derive stat "rank" { term stat "renown" offset 0 divide 10 step 0 = 1 step 5 = 2 step 20 = 3; }
```

A recipe term may end its arithmetic with a table: `step <int64> = <int64>`,
repeated after `divide` and before any `requires` or `unless`. The term's
floored quotient, floor((input + offset) / divisor), selects the value of the
last step whose input it reaches; below the first step the term is 0. Step
inputs strictly increase (E_EQUIPMENT at the input that does not), a term has
at most 32 steps (E_EQUIPMENT at the 33rd `step`), and a term has steps or a
`ceiling`, not both (E_EQUIPMENT at the first `step`). In IR the table is the
term's `steps`, each `{at, value}`; a term without steps serializes as before.
The recipe explanation shows the table in `term.steps` and the selected value
in `exact`. Every recipe takes steps: derived rules, item modes, and Ability
formulas and roll bonuses. docs/CHARACTER_STATS.md derives the SRD experience
thresholds this way.

## Alternative recipes

```nql
derive stat "defense" {
  term stat "dexterity" offset -10 divide 2;
  recipe "ac-set:mage-armor" base 13 requires "effect:mage-armor" unless slot "body" { term stat "dexterity" offset -10 divide 2; }
}
```

A derived rule may declare gated alternatives to its default recipe:
`recipe "<name>" base <int64> requires "<type>" {unless slot "<slot>"} { <terms> }`,
in any order among its terms. The default recipe is the declared base plus
the rule's terms, or a worn item mode's `base stat` override. A recipe is
eligible while its character has the `requires` Condition type and wears
nothing occupying one of its `unless` slots. The stat takes the recipe with
the highest total, base plus active terms, over the default and every
eligible recipe; ties keep the default, then the earlier recipe. Modifiers
add after the choice, so an alternative such as SRD Mage Armor never adds to
Unarmored Defense or to armor. Names are unique in their rule (E_DUPLICATE);
`base` and `requires` are required (E_PARSE); the type and slots must be
declared and the terms read non-derived stats (E_EQUIPMENT); a rule has at
most 64 recipes. In IR the rule's `recipes` holds `{name, base, requires,
unless_slots, terms}`. The explanation's `recipe` adds `name` and `requires`
when an alternative is chosen, and `candidates`, the default first, with
`name`, `eligible` and the total in `exact` for each eligible one. A rule
without recipes serializes and explains as before.

## Stat adjustments

```nql
adjust "char:bard" stat "xp" by 300;
```

`adjust "<character>" stat "<stat>" by <int64>;` is an action. It lowers to
the `adjust_stat` operation `{character, stat, amount}` and adds the signed
amount to the stat's declared base, for a running count such as experience
points. The amount is nonzero (E_AMOUNT at the amount), the character must
declare the stat (E_STAT at the stat, E_REFERENCE at the character), and the
stat must not be derived (E_RULE at the stat), since an agent never writes a
total. The new base must fit int64 (E_OVERFLOW), and every total that reads it
is checked with the rest of the transaction. The event is `StatAdjusted` with
`stat_adjustment`: `character`, `stat`, `amount`, and the `before` and `after`
bases. Replay rebuilds the operation from it. Status reports the new `base`,
which a host that rebuilds the world per call declares at its next genesis.
`adjust relation` keeps its form; Abilities do not take the stat form.

## Recoverable consumption and recovery

```nql
rules { transfer unequips; wear any; recovery 1 of 2; }
consume "item:arrows" from "char:ranger" by 6 recoverable;
recover "item:arrows" from "char:ranger";
```

`recovery <int64> of <int64>;` in the rules block declares the share of spent
stock that a recovery returns, rounded down. The numerator is between 0 and the
denominator, and the denominator between 1 and 1000 (E_RULE at `recovery`).
A world has one rule (E_DUPLICATE). A world with a Battle block refuses it
(E_RULE at `recovery`), since Battle recovers ammunition with `battle recover`.
In IR the rule is `recovery_numerator` and `recovery_denominator` in `rules`,
omitted when absent.

`consume ... by <n> recoverable;` debits the stock as `consume` does and adds n
to the item's `recoverable` count, for ammunition that can be picked up again.
Without the rule it is refused (E_RULE at `recoverable`), and a count past
int64 is refused with E_QUANTITY. The `consumption` record carries
`recoverable: true`, and replay rebuilds the flag. Ability `consume` steps
spend without it.

`recover "<item>" from "<owner>";` and `recover "<item>" from "<owner>" by <n>;`
lower to the `recover_item` operation `{item, owner, amount}`, with `amount`
omitted when absent. The share is floor(count * numerator / denominator). The
stock gains the whole share, or n of it when stated: n is between 0 and the
share, and 0 forfeits it (E_AMOUNT at the amount below 0, E_QUANTITY above the
share). The count returns to 0 either way. It is refused without the rule
(E_RULE at `recover`) or when the count is 0 (E_QUANTITY). The stated owner,
known custody and wearing are checked as for `add`, and overflow is refused
with E_QUANTITY. The event is `ItemRecovered` with `item_recovery`: `item`,
`owner`, `requested` (when stated), `spent` (the count it closes), `allowed`
(the share), and the `before` and `after` quantity. Replay rebuilds the
operation from it.

An item declares a pending count with `recoverable <int64>;`, for a host that
rebuilds the world per call. The count is nonnegative (E_QUANTITY at the
value), needs the rule, and is not declared on a container (both E_RULE at
the value). In IR it is the item's `recoverable`, omitted at 0. Split leaves
the count with the original stock, and transfer carries it to the new owner.
docs/AMMUNITION.md walks through the SRD rule.

## World map and party travel

```nql
clock "second" at 0;
map {
 route "loc:camp" to "loc:ford" ticks 600 both;
 route "loc:ford" to "loc:mill" ticks 300;
 party "char:ada";
 visited "loc:ford";
 hazard "loc:mill";
 closed "loc:mill" to "loc:camp";
}
```

A world declares at most one `map` block (E_DUPLICATE at the second `map`).
It needs a `clock`, since route durations are Clock ticks, and refuses a
`battle` or `adventure` block, which travel by their own routes (both E_MAP at
`map`). Inside the block, in any order, each statement ending in `;`:

- `route "<from>" to "<to>" ticks <n>;` declares a directed route between two
  existing Locations (E_REFERENCE or E_TYPE at the operand) that differ
  (E_MAP `different endpoints` at the second). The ticks are 1 to
  1,000,000,000 (E_MAP at the number). One route per direction (E_MAP `unique
  direction` at the duplicate). `both` also declares the reverse with the same
  ticks; a clash with it is reported at its `to`. At most 32,768 directed
  routes (E_MAP `at most 32768 routes`).
- `party "<character>";` names a Character that travels with the party, once
  each (E_MAP `unique member`).
- `visited "<location>";` and `hazard "<location>";` name a Location the party
  has visited, or where a trip stops on its way; once each (E_MAP `unique
  location`).
- `closed "<from>" to "<to>";` names a declared route that travel may not use
  (E_MAP `declared route`), once each (E_MAP `unique route`).

In IR the block is the blueprint's `map`: `routes` (`from`, `to`, `ticks`),
`party`, `visited`, `hazards` and `closed` (`from`, `to`), each kept sorted;
`map` is omitted when absent. The engine checks the same rules on a map handed
to it directly, as in a checkpoint. A world without a map is unchanged.

In a world with a map, `travel party to "<location>";`, as an action or an
Ability step, moves the party along the quickest open path. The actor is a
party member (E_MAP `travel actor`), every member stands at the actor's
Location (E_MAP `travel party`, at the member, with its Location as actual),
and the destination differs from it (E_MAP `travel`). A path uses routes that
are not closed, and every place on it before the destination is visited or is
the departure; the destination may be any Location. The quickest path by ticks
wins; equal ticks go to fewer stops, then to the path whose stops, compared in
order, have the smaller IDs. Without a path the request is refused with E_MAP
`travel path`, and `actual` names the reason: `no route` (no path even with
every route open and every place visited), `unvisited stop` (visiting places
would give one), `closed route` (opening routes would), or `closed route and
unvisited stop`.

The trip ends at the first hazard on the path before the destination, or at
the destination. Every member moves there in sorted ID order (`EntityMoved`),
then the trip's ticks pass as with `advance time` (`TimeAdvanced`, after any
Condition endings due on the way). The departure and the arrival become
visited. The terminal `PartyTraveled` event carries `travel` (the intent) and
`trip`: `from`; `stops`, the places reached in order, the last being where the
party is; `ticks`; `halted` when a hazard stopped it; and `visited`, the
places newly visited. Replay rebuilds the operation from `travel`. Each move
and the time count against the request's work bound (E_LIMIT). Any failure
rejects the whole request. Travel never changes hazards, closures or the
party; the map edits below do, and a place where a hostile occupant is present
is a hazard too.

nql-apply takes `"map": ["<character>", ...]` and returns `map`, one
projection per Character: `revision`, `clock`, `character`, `location`,
`exits` (each route out of the location: `to`, `ticks`, `visited`, `hazard`,
`closed`) and `destinations` (every Location travel reaches from there, sorted
by ID: `to`, the full path's `ticks` and `stops`, `visited`, and `halts_at`,
the hazard where a trip toward it stops, when there is one). A world without a
map refuses the view (E_MAP `map`). docs/WORLD_MAP.md is the host guide.

## Occupants and map changes in play

```nql
occupant "occ:ford/wolves" named "Wolves" at "loc:ford" { creatures 2 to 4; attitude hostile; type "wolf"; }
occupant "occ:camp/mira" named "Mira Tidewise" at "loc:camp" { person; attitude friendly; alias "Mira"; }
```

An occupant is someone or something at a Location that is not a Character: a
person without a sheet, or a group of creatures. A world with occupants needs
a `clock` (E_OCCUPANT `declared clock`, at the first `occupant`). The ID shares
the namespace of characters, items and Locations (E_DUPLICATE `unique
canonical ID`), the name is valid text (E_OCCUPANT `valid name`), and the
Location exists (E_REFERENCE or E_TYPE at it). In the block, each statement
ending in `;`:

- `person;`, `creatures <n>;`, `creatures <min> to <max>;` or `creatures;`,
  exactly one (E_OCCUPANT `person or creatures` at the ID when absent,
  E_DUPLICATE at a second). `creatures <n>` is the members present, 0 to
  1,000,000,000; with those who left, 1 to 1,000,000,000 (E_OCCUPANT at the
  number). A group not yet counted has a range, 1 <= min < max <=
  1,000,000,000 (E_OCCUPANT at min), or no number at all, and has no tally
  (E_OCCUPANT `a counted group`).
- `attitude friendly|indifferent|hostile;`, at most once; indifferent when
  absent.
- `type "<text>";`, at most once: what the occupant is, for the host's stat
  tables.
- `alias "<name>";`, other names it is known by: at most 16 (E_OCCUPANT at
  the 17th), each valid, different from the name and from each other
  (E_OCCUPANT at the alias).
- `tally <outcome> <n> [because "<reason>"];`, how members left and why:
  `defeated`, `fled`, `surrendered`, `departed`, `joined` or `died`, n from 1
  to 1,000,000,000. The reason is valid text of at most 200 bytes (E_OCCUPANT
  `valid text of at most 200 bytes` at it); `departed` always has one
  (E_OCCUPANT `a reason for departed` at the outcome). One entry per outcome
  and reason (E_OCCUPANT `one entry per outcome and reason`). A person with a
  tally entry has left; a person leaves once (E_OCCUPANT `a person leaves
  once`).

In IR an occupant is `{id, name, location, kind, count, range {min, max},
attitude, type, aliases, tally [{outcome, count, reason}]}`, `count`, `range`
and `reason` each omitted when absent, in the blueprint's `occupants`, sorted
by ID, with aliases sorted and the tally sorted by outcome, then reason. A person's `count`
is 1 while present and 0 once left. `occupants` is omitted when empty. The
engine checks the same rules on records handed to it directly, and at most
32,768 occupants per world, resolved ones included.

Actions, each refused with E_OCCUPANT, `actual` naming the reason, or
E_NO_CHANGE when it would change nothing:

| Action | Event | Refused when |
| --- | --- | --- |
| `create occupant "<id>" named "<name>" at "<location>" { ... };` | `OccupantCreated` | the record is invalid as above, or has a tally or no one present (`create`) |
| `move occupant "<id>" to "<location>";` | `OccupantMoved` | no one is present (`move`, `resolved`); already there |
| `count occupant "<id>" as <n>;` | `OccupantCounted` | a person, a counted group, or n outside the range (`count`) |
| `resolve occupant "<id>" as <outcome> [by <n>] [because "<reason>"];` | `OccupantResolved` | the group is uncounted, no one is present, or n is more than are (`resolve`); n < 1 is E_AMOUNT; a reason as for `tally`, `departed` without one at the outcome (`reason`) |
| `set occupant "<id>" attitude <attitude>;` | `OccupantAttitudeChanged` | the attitude is the same |
| `rename occupant "<id>" to "<name>";` | `OccupantRenamed` | the name is the same, or the aliases would pass 16 (`aliases`) |
| `return occupant "<id>" from <outcome> ["<reason>"] [by <n>];` | `OccupantReturned` | no entry matches (`return`, `none`), several do and no reason names one (`return`, `ambiguous`), or n is more than the entry holds (`return`, the number); n < 1 is E_AMOUNT; the reason is valid text of at most 200 bytes (`reason`) |

An unknown occupant is E_OCCUPANT `declared occupant` at its ID. `count` sets
the count and drops the range. `resolve` without `by` resolves every present
member; it lowers the count and adds to the tally entry with its outcome and
reason. An occupant with no one present stays at its last Location, with its
tally. `return` moves n members of one tally entry, all of it without `by`,
back to present at the occupant's Location; a person's count goes back to 1.
The reason names the entry; without one, `return` takes the outcome's entry
that has no reason, or the outcome's only entry. A reason is the agent's
text: the engine keeps it and never reads it. `rename` keeps the old
name as an alias and drops the new one from the aliases. Each event carries
`occupant`: `intent` (the operation), `before` (absent for a creation) and
`after`. Replay rebuilds the operation from `intent`. Occupant actions are not
Ability steps. A group declared without a number is counted at 1 to
1,000,000,000.

Map edits, in a world with a map (E_MAP `world map` at the verb otherwise):

| Action | Event | No change when |
| --- | --- | --- |
| `join party "<character>";` | `PartyJoined` | already a member |
| `leave party "<character>";` | `PartyLeft` | not a member |
| `close route "<from>" to "<to>";` | `RouteClosed` | already closed |
| `open route "<from>" to "<to>";` | `RouteOpened` | not closed |
| `mark hazard "<location>";` | `HazardMarked` | already declared |
| `mark visited "<location>";` | `VisitMarked` | already visited |
| `clear hazard "<location>";` | `HazardCleared` | not declared |

A route must be declared (E_MAP `declared route` at its from). Clearing a
place that is not declared but where a hostile occupant is present is E_MAP
`hazard`, `actual` naming the occupant: resolving it, or changing its
attitude, clears the place. Each event carries `map_edit`, the intent.

A Location where a hostile occupant is present (an uncounted group is) is a
hazard for travel and the map view, as a declared one is.

nql-apply takes `"locations": ["<location>", ...]` and returns `locations`,
one projection per Location: `revision`, `clock`, `location`, `characters`
there (`id`, `name`, sorted), `present` (occupants with anyone present) and
`resolved` (occupants with no one present), each the full record sorted by
ID; with a map, also `visited` and `hazard`. An unknown Location fails the
view. docs/LOCATIONS.md is the host guide. A host that writes a fresh world
for each call passes `live_state` and gets the occupants as play left them,
and with a map the characters' places and the map state; docs/LIVE_STATE.md
is its guide.

## Quests

```nql
quest "quest:valley/mill" named "Clear the Old Mill";
quest "quest:valley/tower" named "Warn the Watchtower" { requires "quest:valley/mill"; started impact "go" at 28800; }
```

A quest is a goal with a status: `unstarted`, `started`, `completed` or
`failed`. The ID shares the namespace of characters, items, Locations and
occupants (E_DUPLICATE `unique canonical ID`), and the name is valid text
(E_QUEST `valid name`). A world with quests needs no clock. The quest ends
in `;` or a block, each statement ending in `;`:

- `requires "<quest>";`, a quest that must be completed before this one
  completes: declared (E_QUEST `a declared quest`), not the quest itself
  (`another quest`), each once (`each quest once`), at most 16 (`at most 16
  requirements` at the 17th), and no cycle among all quests (`no cycle`).
- `side of "<quest>";`, at most once (E_DUPLICATE `one side of`): a declared
  quest other than itself, for grouping only.
- The log, at most 16 entries (E_QUEST `at most 16 entries` at the 17th),
  each optionally ending in `at <tick>`, an unsigned 64-bit clock tick
  (E_INTEGER):
  - `started [impact "<text>"]`, `failed [impact "<text>"]`;
  - `note impact "<text>"`;
  - `completed [ahead "<quest>" ... because "<reason>"] [impact "<text>"]`,
    the ahead quests declared and each once, at most 16;
  - `reopened because "<reason>"`.

  Texts and reasons are valid text of at most 512 bytes (E_QUEST `valid
  text of at most 512 bytes` at it). The entries follow the actions below
  from `unstarted` (E_QUEST `a log the actions allow` at the ID, `actual`
  naming the entry and the status before it), and the status is the one the
  log ends in.

In IR a quest is `{id, name, side_of, requires, status, log [{kind, text,
ahead, reason, tick}]}`, empty fields omitted, in the blueprint's `quests`,
sorted by ID, with `requires` and `ahead` sorted. `quests` is omitted when
empty. The engine checks the same rules on records handed to it directly,
the status against the log included, and at most 4,096 quests per world.

Actions, each refused with E_QUEST, `actual` naming the reason, or
E_NO_CHANGE when it would change nothing:

| Action | Event | Refused when |
| --- | --- | --- |
| `start quest "<id>" [impact "<text>"];` | `QuestStarted` | completed or failed (`status`); started is E_NO_CHANGE |
| `note quest "<id>" impact "<text>";` | `QuestNoted` | not started (`status`) |
| `complete quest "<id>" [ahead because "<reason>"] [impact "<text>"];` | `QuestCompleted` | failed (`status`); requirements open without `ahead` (`requires`, `actual` the open IDs joined by `, `); `ahead` with none open (`ahead`, `none`); completed is E_NO_CHANGE |
| `fail quest "<id>" [impact "<text>"];` | `QuestFailed` | completed (`status`); failed is E_NO_CHANGE |
| `reopen quest "<id>" because "<reason>";` | `QuestReopened` | neither completed nor failed (`status`) |

A `status` refusal's `expected` names the statuses the action takes. An
unknown quest is E_REFERENCE at its ID; a text or reason is checked as in
the log, at the token (E_QUEST `text` or `reason`). Each action adds one log
entry with the clock tick, when the world has a clock; `complete ...
ahead` records the open requirements and the reason. `reopen` returns to
`started`. When the log is full, the new entry drops the oldest note; with
no note to drop the action is refused (E_QUEST `log`, `at most 16 status
changes`, `full`). Each event carries `quest`: `intent` (the operation),
`before`, `after` and, when a note was dropped, `dropped`. Replay rebuilds
the operation from `intent`. Quest actions are not Ability steps.

nql-apply takes `"quests": ["<quest>", ...]` and returns `quests`, one
projection per quest in the order asked: `revision`, the record, `open`
(its requirements not completed, in order) and `ready` (neither completed
nor failed, with none open). An unknown quest fails the view.
docs/QUESTS.md is the host guide.

## Journal

```nql
journal "journal:valley/0001" departure seq 1 at "loc:valley/B01" tick 28800 with "char:ada" "char:bram" text "Set out before dawn.";
journal next 3;
```

A journal entry is a typed record of what happened where: `{seq, id,
kind, tick, place, participants, text}`. A declaration states an entry
back as a receipt wrote it, for a host that passes no `live_state` or
converts a campaign once:

- the ID, valid and unique, shares the namespace of characters, items,
  Locations, occupants and quests (E_DUPLICATE `unique entry ID`);
- the kind is `departure`, `arrival`, `event`, `discovery` or `note`;
- `seq <n>`, unsigned 64-bit (E_INTEGER), at least 1, each entry's above
  the one before (E_JOURNAL `increasing, from 1 and below next` at it);
- `at "<place>"`, a valid ID. It is history: it need not be declared;
- `tick <n>`, optional, unsigned 64-bit;
- `with "<id>" ...`, optional: valid IDs, each once (E_JOURNAL `valid IDs,
  each once` at the second), at most 16 (`at most 16 participants`). They
  are history too, and are sorted;
- `text "<text>"`, optional, one line of valid text of at most 512 bytes
  (E_JOURNAL `valid text of at most 512 bytes` at it, `actual` `control
  character` for a newline, a tab or another control character).

`journal next <n>;`, at most once (E_DUPLICATE `one journal next`), is the
seq the next entry takes: above every declared seq, and at least 1
(E_JOURNAL `at least 1`). Without it, the next seq is one past the last
declared entry; a host states it whenever entries were dropped. At most
1,024 entries (E_JOURNAL `at most 1024 entries`).

In IR the journal is `{next, entries [{seq, id, kind, tick, place,
participants, text}]}`, empty fields omitted, in the blueprint's
`journal`, omitted when the world has none. The engine checks the same
rules on records handed to it directly.

```nql
record journal "<id>" <kind> [at "<place>"] [with [party] ["<id>" ...]] [text "<text>"];
```

`record journal` adds one entry, event `JournalRecorded`:

- `seq` is the journal's next; it never repeats, even after a drop.
- `tick` is the clock's tick at that point in the request: an entry before
  `travel party to` has the departure tick, one after it the arrival's.
  Without a clock there is none.
- `place` is the one named, a declared Location (E_REFERENCE, or E_TYPE
  for another kind, at it). Without one it is the actor's: a Location
  actor, or where a character actor stands (E_JOURNAL `place`, `a place, or
  an actor standing at one`, otherwise).
- `party` names every party member at that point; it needs a map
  (E_JOURNAL `party`, `a world map`, at compile). Each listed ID that is a
  declared character or occupant is a participant. Any other valid ID is
  left out and named in the event's `dropped_participants`, sorted, never
  refused. At most 16 participants: the party first, sorted, then the
  listed names in order; those past the 16th are left out and named the
  same way. At most 16 names listed (E_JOURNAL `participants`, `at most 16
  participants`, at compile).
- An ID already taken is refused at execution (E_DUPLICATE `journal`
  `unique entry ID`), so a retry the host does not recognise never records
  twice.
- A full journal of 1,024 entries drops its oldest; the event's `dropped`
  holds it, and its ID is free again.

Each event carries `journal`: `intent` (the operation), `entry`, and when
present `dropped` and `dropped_participants`. Replay rebuilds the operation
from `intent`. `record journal` is not an Ability step.

nql-apply takes `"journal": {"id": [...], "after": <seq>, "since": <tick>,
"place": "<place>", "participant": "<id>", "kind": [...], "limit": <n>}`,
every field optional and all of them matching, and returns `journal`:
`revision`, `next`, and the entries in seq order. `limit` is 0 to 1,024, 0
for no limit; an unknown kind or another limit fails the view (E_JOURNAL
`kind` or `limit`). docs/JOURNAL.md is the host guide.

## Attacks outside Battle

```nql
attacks { defense "ac"; health "hp"; die 20; miss 1; critical 20; critical dice 2; }
```

The attack law, at most once (E_DUPLICATE `one attacks block`), its fields
in this order. `defense` and `health` are valid IDs (E_ATTACK `defense`,
`health`); the die has 2 to 1,000 sides (E_ATTACK `die`); the miss and
critical faces lie on it (E_ATTACK `miss`, `critical`) and differ
(`critical`, `a face other than the miss face`); `critical dice` is 1 to 8
(E_ATTACK `critical dice`). A world with a Battle cannot declare it
(E_ATTACK `attacks`). Each fault is at its token. In IR it is the
blueprint's `attacks`: `{defense, health, die_sides, miss_face,
critical_face, critical_multiplier}`.

```nql
attack "<attacker>" profile to_hit <n> damage <c>d<s>|flat[+<b>|-<b>] kind "<kind>" against "<target>" [advantage | disadvantage] faces <a> [<b>] [damage <n>] [critical damage <m>] [faces "<instance>" <f> ...];
```

`attack` scores one attack with the law and applies its damage, event
`AttackScored`. The dice are written `1d6`, the bonus as a signed integer
against them, `1d6+2` or `1d6-1` (E_PARSE `dice sides such as d8` for
another word). `flat` is damage without dice, count and sides 0, the bonus
alone: `flat+4` (docs/ATTACKS.md, Flat damage). An equipment attack grant's
`damage "<kind>" dice <c> sides <s> { <recipe> }` likewise takes
`damage "<kind>" flat { <recipe> }`. The attacker and the target are
characters (E_REFERENCE, E_TYPE); the target has the defense stat (E_STAT)
and the health Resource (E_RESOURCE). Apart from `flat`, the count is 1 to
64 (E_ATTACK `damage dice`), the sides 1 to 1,000 (`damage sides`), the
kind valid text (`damage kind`). One face without a mode, two with one
(E_ATTACK `faces`, `exactly 1 face` or `exactly 2 faces`), each on the die
(`faces in 1..<die>`, at the face).

- The kept face is the higher of two with advantage, the lower with
  disadvantage. The total is it plus `to_hit` (E_OVERFLOW `attack total`).
  The critical face hits and the miss face misses; another face hits when
  the total reaches the target's effective defense.
- `damage <n>` is the hit's damage dice as rolled, `critical damage <m>`
  the critical's, `critical dice` times as many; neither includes the
  bonus. The one the outcome needs lies from its dice count to the count
  times the sides (E_ATTACK `damage` or `critical_damage`, at execution).
  The other is named in `unused` and not checked. A hit with only the
  other total is refused (E_ATTACK `damage`, the clause needed, `absent`);
  with neither, the attack is only scored.
- The damage is the total plus the bonus, at least 0 (E_OVERFLOW `damage`
  past 64 bits). A positive damage is a Damage on the health Resource:
  buffers, Conditions ending at the minimum and damage-triggered checks run
  as for `damage`, and `faces "<instance>" <f>` supply those checks' faces,
  in check order. Faces no check takes are named in `unused_faces`, never
  refused (at most 128 faces, each 1 to 1,000: E_ATTACK `check faces`).

The Damage's events come first; `AttackScored` closes the operation with
`attack`: `intent` (the operation), `kept`, `total`, `defense`, `hit`,
`critical`, `natural_miss`, and on an applied total `damage_count`,
`damage_rolled`, `raw_damage` (exact) and `damage`, plus `unused` and
`unused_faces`. Replay rebuilds the operation from `intent`. In IR the
operation is `attack`: `{attacker, profile {to_hit, damage_count,
damage_sides, damage_bonus, damage_kind}, target, mode, faces, damage,
critical_damage, check_faces [{condition, face}]}`; the operation carries
no `condition_rolls`. Several in one request apply in order. nql-apply
lists the request's scored attacks in `attacks`. docs/ATTACKS.md is the
host guide.

### Damage traits

```nql
resist "piercing"; vulnerable "bludgeoning"; immune "poison";
damage trait "spell:warding" resistance "bludgeoning";
```

The first form goes in a character body (declared or created in play), the
second at world level. A character's treatment words are `resist`,
`vulnerable` and `immune`; a policy's are `resistance`, `vulnerability`
and `immunity` (E_PARSE for another word). The kind is valid text, at the
kind (E_ATTACK `damage trait`, `valid damage kind`). A character lists each
kind once per treatment (`each kind once per treatment`, at the kind) and
at most 32 of each (`at most 32 of each treatment`, at the word). A policy
names a declared Condition type (`declared Condition type`, at it), is
unique (`unique policy`, at the kind), and a world has at most 16
(`at most 16 policies`, at `damage`). Both need the `attacks` law (E_ATTACK
`damage trait`, `world attacks law`: at a character's first trait word, or
at the policy's `damage`), so a Battle world has neither. In IR a
character's are `damage_traits [{treatment, kind}]` and the blueprint's
`damage_traits [{condition, treatment, kind}]`.

An attack whose damage is applied treats the raw damage, floored at 0,
with every trait of the profile's kind on the target and every policy of
that kind whose Condition type an instance on the target has: halved,
rounding down, if any resistance applies; then doubled if any
vulnerability; then 0 if any immunity. `AttackScored` lists the sources in
`treatments [{character | condition, treatment}]`: own traits in the order
resistance, vulnerability, immunity, then the instances by ID, each in the
same order.

```nql
damage "<character>" resource "<resource>" by <n> kind "<kind>" [faces "<instance>" <f> ...];
```

A `damage` with a kind treats <n> with the same traits and law before the
Damage applies it, outside an attack: buffers, then the Resource, then the
damage-triggered checks, all on the treated amount. The kind is valid text
(E_ATTACK `kind`, `valid damage kind`, at the kind; an empty `kind ""` is
refused there too), and a world with a Battle refuses it (E_ATTACK `kind`,
`world without Battle`). Only `damage` takes the clause. In IR it is the
Damage operation's `kind`; heal, spend, grant or expand with a kind are
E_TYPE `kind`. `DamageApplied`'s `resource` gains `treated: {kind, amount,
treatments, unused_faces}`: the kind and <n> as sent, the sources as for an
attack (empty when none applied), and the check faces no check took, never
refused. `requested` is the treated amount. A treated amount of 0 is
recorded alone, with nothing changed and no check. Replay rebuilds the
operation from `treated`, its unused faces after the ones its checks took.
A `damage` without a kind is unchanged.

### Saves

```nql
save "<source>" profile stat "<stat>" dc <n> [damage <c>d<s>[+<b>|-<b>] kind "<kind>" success half | none] against "<target>" [advantage | disadvantage] [faces <a> [<b>]] [damage <n>] [faces "<instance>" <f> ...];
```

`save` scores one saving throw with the attack law's die and applies its
effect, event `SaveScored`. It needs the `attacks` law (E_SAVE `law`). The
source and the target are characters (E_REFERENCE, E_TYPE); the target
declares the stat (E_STAT, at the stat) and, with a damage profile, the
health Resource (E_RESOURCE, at the target). The DC is any int64. The
damage profile is optional and bounded as an attack's (E_SAVE `damage
dice`, `damage sides`, `damage kind`, each at its token); `success` is
`half` or `none` (E_PARSE for another word; E_SAVE `success` in IR). At
most two faces, each on the die (E_SAVE `faces`, `at most 2 faces` or
`faces in 1..<die>`, at the face).

- The save is a stat check on the target's stat: the requested mode
  combined with the stat's roll sources, the kept face plus the effective
  value (E_OVERFLOW at the stat), success when the total reaches the DC.
  The engine takes the faces the net mode needs, in order (none for an
  automatic failure, one, or two with advantage or disadvantage); the rest
  are named in `unused_save_faces`. Too few are refused at execution
  (E_ROLL `faces`, the count the mode needs).
- The effect is `full` on a failed save and the profile's `success` on a
  successful one. `damage <n>` is the damage dice as rolled. When the
  effect needs it, it lies from the count to the count times the sides
  (E_SAVE `damage`, at execution); for `none` it is named in `unused` and
  not checked. Without a damage profile a total is refused (E_SAVE
  `damage`, `a damage profile`, at the total); without a total the save is
  only scored.
- The damage is the total plus the bonus, at least 0, halved rounding down
  for `half`, then treated with the target's traits of the kind as for an
  attack (E_OVERFLOW `damage`). A positive damage is a Damage on the health
  Resource, with `faces "<instance>" <f>` for its checks as on an attack;
  faces no check takes are named in `unused_faces` (E_SAVE `check faces`
  for the bounds).

The Damage's events come first; `SaveScored` closes the operation with
`save`: `intent` (the operation), `check` (a stat check result), `effect`
when the profile has damage, and on an applied total `damage_rolled`,
`raw_damage` (exact), `treatments` and `damage`, plus `unused`,
`unused_save_faces` and `unused_faces`. Replay rebuilds the operation from
`intent`. In IR the operation is `save`: `{source, profile {stat, dc,
damage {count, sides, bonus, kind, success}}, target, mode, faces, damage,
check_faces [{condition, face}]}`. Several in one request apply in order.
nql-apply lists the request's scored saves in `saves`. docs/SAVES.md is the
host guide.
