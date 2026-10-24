# Attacks outside Battle

An `attack` scores one attack and applies its damage, for any combatant, in
a world without a Battle. The host sends the typed numbers: the attack's
profile, the faces rolled, and the damage dice as rolled. The engine keeps
the face that counts, decides hit, miss and critical, adds the bonus, and
applies the damage through the same Damage a `damage` action runs:
temporary hit points first, Conditions that end at the minimum, and the
checks damage triggers, such as concentration. Before that it applies the
target's damage traits: resistance, vulnerability and immunity.

"Host" means any caller that runs the game: a DM model, a script, a menu.
The host decides who attacks whom, with what, and with which mode; reach,
range and cover are its call, since there is no grid outside Battle. The
engine never draws a die, so a world needs no `dice` for attacks.

The files:

- `examples/attacks/world.nql`: a per-call world in a host's shape, with a
  map and no Battle. Ada, AC 16, holds concentration and 5 temporary hit
  points; a goblin, AC 15, has 7 hit points; a skeleton, AC 13, has 13 hit
  points, its stat block's damage traits, and a warding spell on it.
- `examples/attacks/swing.nql`: the goblin's scimitar at Ada, with both
  damage totals from its prerolls.
- `examples/attacks/player-score.nql` and `player-damage.nql`: Ada's
  critical, scored first and then sent again with the player's damage.
- `examples/attacks/traits.nql`: Ada's mace at the warded skeleton.
- `examples/attacks/rider.nql`: 7 bludgeoning at the same skeleton, outside
  an attack.
- `examples/attacks/flat.nql`: Ada's Unarmed Strike, damage without dice.
- `tests/attack_test.go`: every rule below, at its bounds, and thousands of
  random attacks against an independent model of the law.
- `tests/attack_options_test.go`: the attack options view, below.

## Facts and totals

| The host controls | The engine derives |
| --- | --- |
| who attacks whom, the profile, advantage or disadvantage | the face that counts: the higher of two with advantage, the lower with disadvantage |
| the faces rolled | the total, and hit, miss or critical against the target's effective defense |
| the damage dice as rolled, summed | the dice a critical needs, the range check, the bonus, the damage applied |
| the check faces for the target's Conditions | the checks the damage triggers, their DC, and their outcome |
| the target's traits and Conditions, in the world | the treatments the damage meets, and the damage applied |

## The law

```nql
attacks { defense "ac"; health "hp"; die 20; miss 1; critical 20; critical dice 2; }
```

A world declares the law once, in this order. `defense` is the stat every
target is scored against, read as its effective value; `health` is the
Resource damage lowers. The die has 2 to 1,000 sides; the miss face and
the critical face lie on it and differ; a critical rolls the damage dice
`critical dice` times, 1 to 8. A world with a Battle keeps its own rules
and cannot declare `attacks`.

## The attack

From `swing.nql`:

```nql
attack "occ:valley/B03/goblin-1" profile to_hit 4 damage 1d6+2 kind "slashing" against "char:ada" faces 17 damage 4 critical damage 7 faces "char:ada:concentration" 12;
```

`attack "<attacker>" profile to_hit <n> damage <c>d<s>|flat[+<b>|-<b>] kind
"<kind>" against "<target>" [advantage | disadvantage] faces <a> [<b>]
[damage <n>] [critical damage <m>] [faces "<instance>" <f> ...];`

- **profile:** the attack's numbers from the stat block or the sheet.
  `to_hit` and the bonus are any 64-bit integers; the bonus is signed and
  written against the dice, `1d6+2` or `1d6-1`. The count is 1 to 64, the
  sides 1 to 1,000, and the kind any valid text. `flat` is damage without
  dice (see Flat damage).
- **faces:** one face without a mode, exactly two with `advantage` or
  `disadvantage`, each on the law's die. Both stay in the event.
- **score:** the total is the kept face plus `to_hit`. The critical face
  always hits and the miss face never does; any other face hits when the
  total reaches the target's defense.
- **damage:** `damage <n>` is the sum of the damage dice as rolled for a
  hit, `critical damage <m>` the sum for a critical, with `critical dice`
  times as many dice. Neither includes the bonus: the engine adds it, once,
  a negative result is 0, and then the target's damage traits apply.
  - The total the outcome needs is range-checked: from the dice count to
    the count times the sides.
  - A total the outcome does not need is recorded in `unused`, never
    checked or refused, so a monster can send both totals from its
    prerolls in one call.
  - With neither total, the attack is scored and nothing is applied: the
    player's first step.
  - A hit whose total is missing, while the other is given, is refused.
- **check faces:** `faces "<instance>" <f>` are the target's dice for a
  check the damage triggers on that Condition, as on `damage`, in check
  order (by instance ID); a check takes the next faces that name it. Faces
  no check takes are recorded in `unused_faces`, never refused: on a miss,
  or when an earlier swing already ended the Condition. A host can send a
  concentrating target's prerolled face with every swing. A check that runs
  without faces rolls the world's dice, and a world without dice refuses it.

Several attacks in one request, Multiattack for instance, apply in order
and atomically: the second sees the first's damage, an attack on a target
at 0 still scores, and one refused attack refuses the request.

## The receipt

An attack's events are the Damage's, when damage is applied, then one
`AttackScored`, which closes the operation; replay rebuilds the attack
from its `intent`. nql-apply also lists every scored attack in the
response's `attacks`, in operation order. The goblin's swing:

```json
{
  "intent": {"attacker": "occ:valley/B03/goblin-1",
             "profile": {"to_hit": 4, "damage_count": 1, "damage_sides": 6, "damage_bonus": 2, "damage_kind": "slashing"},
             "target": "char:ada", "faces": [17], "damage": 4, "critical_damage": 7,
             "check_faces": [{"condition": "char:ada:concentration", "face": 12}]},
  "kept": 17, "total": 21, "defense": 16, "hit": true, "critical": false, "natural_miss": false,
  "damage_count": 1, "damage_rolled": 4, "raw_damage": "6", "damage": 6,
  "unused": ["critical_damage"]
}
```

| Field | Means |
| --- | --- |
| `intent` | the attack as sent |
| `kept`, `total` | the face that counts, and it plus `to_hit` |
| `defense` | the target's effective defense |
| `hit`, `critical`, `natural_miss` | the outcome; a natural miss is the miss face, whatever the total |
| `damage_count` | the dice behind the total used, the critical applied |
| `damage_rolled` | the total used |
| `raw_damage` | it plus the bonus, exact |
| `treatments` | every source of a resistance, vulnerability or immunity the damage met |
| `damage` | the amount the Damage was asked for, after the treatments; absent when the attack applied nothing |
| `unused` | `damage` or `critical_damage`, the totals the outcome did not need |
| `unused_faces` | the check faces no check took |

Before it, the Damage's events: the check on Ada's concentration (12 + 2
against DC 10, held) and `DamageApplied`, where her 5 temporary hit points
absorb 5 of the 6 and her hit points go from 24 to 23.

## The player in two steps

The host does not know which damage dice to ask for until the swing is
scored. `player-score.nql` sends the swing alone:

```nql
attack "char:ada" profile to_hit 5 damage 1d8+3 kind "slashing" against "occ:valley/B03/goblin-1" faces 20;
```

```json
{
  "intent": {"attacker": "char:ada",
             "profile": {"to_hit": 5, "damage_count": 1, "damage_sides": 8, "damage_bonus": 3, "damage_kind": "slashing"},
             "target": "occ:valley/B03/goblin-1", "faces": [20]},
  "kept": 20, "total": 25, "defense": 15, "hit": true, "critical": true, "natural_miss": false
}
```

A critical: the host asks the player for two d8, and sends the same swing
with `critical damage 11` (`player-damage.nql`), which applies it:

```json
{
  "intent": {"attacker": "char:ada",
             "profile": {"to_hit": 5, "damage_count": 1, "damage_sides": 8, "damage_bonus": 3, "damage_kind": "slashing"},
             "target": "occ:valley/B03/goblin-1", "faces": [20], "critical_damage": 11},
  "kept": 20, "total": 25, "defense": 15, "hit": true, "critical": true, "natural_miss": false,
  "damage_count": 2, "damage_rolled": 11, "raw_damage": "14", "damage": 14
}
```

The bonus is added once, not doubled, and the goblin's 7 hit points go to
0. The first call applied nothing: a host that keeps its world across
calls finds the second scored the same way.

## Damage traits

A character declares its own traits in its body, from the stat block:

```nql
resist "piercing"; vulnerable "bludgeoning"; immune "poison";
```

and a world gives a Condition type a treatment on whoever holds it:

```nql
damage trait "spell:warding" resistance "bludgeoning";
```

The kind is any valid text, matched exactly against the profile's `kind`.
A character lists each kind once per treatment, at most 32 of each; a world
declares at most 16 policies, each once, for declared Condition types. Both
need the `attacks` law, since only an attack applies them: a Battle has its
own damage policies.

The law is Battle's, applied to the raw damage floored at 0: halved,
rounding down, if any resistance applies; then doubled if any vulnerability
applies; then 0 if any immunity applies. Two resistances halve once.
`treatments` lists every source the damage met: the target's own traits,
in the order resistance, vulnerability, immunity, then each Condition
instance on it under a policy, by instance ID, in the same order.

`traits.nql`, Ada's mace at the warded skeleton:

```nql
attack "char:ada" profile to_hit 5 damage 1d6+3 kind "bludgeoning" against "occ:valley/B03/skeleton-1" faces 14 damage 4;
```

```json
{
  "intent": {"attacker": "char:ada",
             "profile": {"to_hit": 5, "damage_count": 1, "damage_sides": 6, "damage_bonus": 3, "damage_kind": "bludgeoning"},
             "target": "occ:valley/B03/skeleton-1", "faces": [14], "damage": 4},
  "kept": 14, "total": 19, "defense": 13, "hit": true, "critical": false, "natural_miss": false,
  "damage_count": 1, "damage_rolled": 4, "raw_damage": "7",
  "treatments": [{"character": "occ:valley/B03/skeleton-1", "treatment": "vulnerability"},
                 {"condition": "occ:valley/B03/skeleton-1:warding", "treatment": "resistance"}],
  "damage": 6
}
```

4 + 3 = 7; the ward's resistance halves it to 3, the skeleton's
vulnerability doubles that to 6, and its hit points go from 13 to 7. When
the ward ends, the same swing does 14. A miss or a score-only swing meets
no treatment.

### Damage outside an attack

A rider or a hazard deals damage without an attack. A `damage` with a kind
meets the same traits and law. The host sends the amount as it lands, and
the engine treats it, then applies it like any Damage: temporary hit
points first, and the checks damage triggers on the treated amount. Damage
that a save decides is a `save` (docs/SAVES.md), which applies the save's
effect before the traits. `rider.nql`:

```nql
damage "occ:valley/B03/skeleton-1" resource "hp" by 7 kind "bludgeoning";
```

The receipt's `DamageApplied` records the kind, the amount as sent and the
sources in `treated`; `requested` is the treated amount:

```json
{
  "character": "occ:valley/B03/skeleton-1", "resource": "hp", "requested": 6,
  "treated": {"kind": "bludgeoning", "amount": 7,
              "treatments": [{"character": "occ:valley/B03/skeleton-1", "treatment": "vulnerability"},
                             {"condition": "occ:valley/B03/skeleton-1:warding", "treatment": "resistance"}]},
  "before": {"current": 13, "minimum": 0, "maximum": 13},
  "after": {"current": 7, "minimum": 0, "maximum": 13}
}
```

- A treated amount of 0, from immunity or a resistance on 1, is recorded
  with `requested` 0 and changes nothing: no temporary hit points, no
  check, no world dice drawn.
- The host cannot know beforehand whether the treated damage triggers a
  check, so check faces no check takes are recorded in `treated`'s
  `unused_faces` and never refused, as for an attack. A `damage` without a
  kind still refuses them.
- `treatments` is empty, not absent, when no trait of the kind applies.
- A world with a Battle refuses a kind: its damage keeps Battle's own
  policies.

## Flat damage

Some damage has no dice: an Unarmed Strike does 1 plus the Strength
modifier, a Blowgun 1. Its profile is `damage flat+<b>`, count and sides 0
in the event and in JSON, and the bonus is the damage. `flat.nql`:

```nql
attack "char:ada" profile to_hit 5 damage flat+4 kind "bludgeoning" against "occ:valley/B03/goblin-1" faces 20 damage 0 critical damage 0;
```

```json
{
  "intent": {"attacker": "char:ada",
             "profile": {"to_hit": 5, "damage_count": 0, "damage_sides": 0, "damage_bonus": 4, "damage_kind": "bludgeoning"},
             "target": "occ:valley/B03/goblin-1", "faces": [20], "damage": 0, "critical_damage": 0},
  "kept": 20, "total": 25, "defense": 15, "hit": true, "critical": true, "natural_miss": false,
  "damage_rolled": 0, "raw_damage": "4", "damage": 4,
  "unused": ["damage"]
}
```

- The totals are the sum of no dice, so each is 0 (range `0..0`). A
  critical has no dice to multiply, and the strike does 4; `damage_count`
  is 0 and omitted.
- A flat attack follows every other rule: with neither total it is only
  scored, and a hit whose total is missing while the other is given is
  refused. A host may send `damage 0 critical damage 0` with every flat
  attack.
- `flat` with no bonus does 0. A count of 0 with any sides other than 0,
  or sides 0 with any count other than 0, is refused as before (E_ATTACK
  `damage dice` or `damage sides`, the bounds unchanged).
- An equipment attack grant takes the same form: `damage "<kind>" flat {
  <recipe> }` in place of `dice <c> sides <s>`, its recipe the damage.
  Battle asks for no damage faces for it and draws none for an agent.

## Refusals

Every refusal applies nothing of the request.

| Code | Field | Expected | Actual | Means |
| --- | --- | --- | --- | --- |
| E_ATTACK | `law` | `world attacks law` | `absent` | the world declares no `attacks` |
| E_ATTACK | `faces` | `exactly 1 face` or `exactly 2 faces` | the count | the face count does not match the mode |
| E_ATTACK | `faces` | `faces in 1..<die>` | the face | a face off the die |
| E_ATTACK | `mode` | `omitted, advantage or disadvantage` | the mode | another mode handed to the engine directly |
| E_ATTACK | `damage dice`, `damage sides`, `damage kind` | the bound | the value | a profile out of bounds |
| E_ATTACK | `damage` or `critical_damage` | `<count>..<count x sides>` | the total | the total the outcome needs is off its dice |
| E_ATTACK | `damage` | `damage` or `critical_damage` | `absent` | a hit whose total is missing while the other is given |
| E_REFERENCE, E_TYPE | `reference` | `character` | `missing`, or the kind | an attacker or target that is not a character |
| E_STAT | the defense stat | `declared stat` | `missing` | the target has no defense stat |
| E_RESOURCE | the health Resource | `declared resource` | `missing` | the target has no health Resource |
| E_ATTACK | `check faces` | `at most 128`, or `valid instance and face 1..1000` | the count, or the face | check faces out of bounds |
| E_ROLL | `faces` | per the check | the faces | a check that runs without faces in a world without dice, or faces that do not fit its mode |
| E_OVERFLOW | `attack total` or `damage` | `int64` | the value | a total past 64 bits |
| E_ATTACK | `damage trait` | `world attacks law` | `absent` | a trait or policy in a world without the law |
| E_ATTACK | `damage trait` | `resistance, vulnerability or immunity`, `valid damage kind`, `each kind once per treatment`, `at most 32 of each treatment` | the value | a character's own trait |
| E_ATTACK | `damage trait` | `declared Condition type`, `unique policy`, `at most 16 policies`, or as above | the value | a world's policy |
| E_ATTACK | `kind` | `valid damage kind` or `world without Battle` | the kind | a `damage` kind that is not valid text, or next to a Battle |
| E_TYPE | `kind` | `damage` | `another resource operation` | a kind on heal, spend, grant or expand handed to the engine directly |

At compile, the fault is at the token: the count, the sides, the kind, the
face, the target; a trait's kind, or its word for a count or a missing law;
a policy's Condition type or kind, or its start. A law fault is at its field (E_ATTACK `die`, `miss`,
`critical`, `critical dice`, `defense`, `health`, or `attacks` next to a
Battle).

## Grants with die upgrades

The profile's numbers come from the Character's grants in the equipment view
(`equipment.attacks`): `to_hit`, `damage_count`, `damage_sides`,
`damage_bonus` and `damage_kind`. An attack may roll a larger die while a
feature is present, such as a Monk's Martial Arts die (see "Gated attacks and
die upgrades" in GRAMMAR.md). The grant then lists the larger die, and the
host sends that die's count and sides.

The engine lists each attack with its better die; it does not choose between
attacks. A Monk holding a Spear two-handed has a 1d8 Spear attack and a d6
unarmed attack at level 1, both listed. The player picks one, and the host
sends that grant's numbers.

## Attack options

The equipment view lists the attacks of what a Character wears, in the mode
it wears each item. A sheet also needs the weapons it carries and the other
ways it can hold them: the Javelins on its back, a Longsword two-handed.
nql-apply takes `"attack_options": ["<character>"]` and returns, for every
item the Character wears or carries itself, every mode that declares
attacks, with that mode's grants (each `[...]` holds grant records):

```text
"attack_options": [{
  "character": {"id": "fighter", "name": "Fighter"},
  "revision": 0,
  "options": [
    {"item": "fighter-javelins", "definition": "javelin", "mode": "held",
     "slots": [{"slot": "hand", "amount": 1}], "worn": false, "grants": [...]},
    {"item": "fighter-sword", "definition": "longsword", "mode": "held",
     "slots": [{"slot": "hand", "amount": 1}], "worn": true, "grants": [...]},
    {"item": "fighter-sword", "definition": "longsword", "mode": "two-handed",
     "slots": [{"slot": "hand", "amount": 2}], "worn": false, "grants": [...]}
  ]
}]
```

That is a Fighter, Strength 16 and proficiency 2, in Chain Mail with a
Shield, a Longsword in one hand and 8 Javelins carried
(`tests/attack_options_test.go`). Each grant is the record the equipment
view lists, with the same ID the member has once the item is worn in that
mode: the Longsword +5 1d8+3, two-handed +5 1d10+3, and the Javelin +5
1d6+3, in melee and thrown 30/120.

- **Which items.** Those the Character wears, and those in its own custody.
  An item it only owns, at a place, in another Character's custody or in a
  container, is not listed. Armor, shields and gear without an attacking
  mode have no entry, and a Character with none has `"options": []`.
- **Worn.** `worn` is true for the mode the item is worn in now. That
  option's grants are the equipment view's grants for the item.
- **Slots are reported, not checked.** `slots` is the mode's slot use. The
  two-handed Longsword is listed while the Shield fills the other hand; the
  equipment view's `slots` say what is free.
- **As the Character stands.** Gates, recipes and die upgrades read the
  Character now. A Monk's Martial Arts grant is shut in every mode while it
  wears a shield. The listed mode is not worn for the purpose: its own slots
  and modifiers do not apply.
- **Order.** By item, then the definition's modes; grants by ID.
- **Condition grants**, such as an Unarmed Strike from a Condition type,
  stay in the equipment view.
- **Changing the mode.** `unequip` and `equip ... mode` in one request
  record the new grip (GRAMMAR.md, "Changing a worn item's mode"); the
  option for that mode is then the worn one.
- **Read-only.** It reads the world after any request commits, like the
  other views. An unknown Character fails it as it fails the equipment view.
  Without the field the response is unchanged. An engine without the view
  refuses the field (exit 2, unknown field).

## What the engine does not decide

- Who may attack whom: reach, range, cover, line of sight. The host.
- Advantage and disadvantage: the host's mode. Conditions do not impose
  one outside Battle.
- Save halving: a `save` applies it (docs/SAVES.md); a plain `damage`
  takes the amount as sent.
- Ammunition: spend it with `consume` in the same request.
