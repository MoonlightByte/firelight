# Saves outside Battle

A `save` scores one saving throw and applies its effect, for any target, in
a world without a Battle. The host sends the typed numbers: the stat the
target rolls, the DC, the damage and what a successful save does to it,
the faces rolled, and the damage dice as rolled. The engine rolls nothing.
It keeps the face that counts under the target's roll mode, decides the
save, and applies the effect: the full damage on a failed save, and half
or none on a successful one. Then come the target's damage traits, and the
same Damage a `damage` action runs: temporary hit points first, Conditions
that end at the minimum, and the checks damage triggers, such as
concentration.

The effect on a success is the spell's or the stat block's, typed by the
host from its data, never from a model's wording: Sacred Flame does no
damage on a successful save, Fireball half.

The files:

- `examples/saves/world.nql`: a per-call world in a host's shape, with a
  map, the attack law and no Battle. Ada holds concentration and 5
  temporary hit points; a giant spider has 26 hit points; an imp resists
  fire; webbing restrains a bandit, which gives it disadvantage on
  Dexterity saves.
- `examples/saves/sacred-flame.nql`: Eirik's Sacred Flame at the spider.
- `examples/saves/burning-hands.nql`: a cultist's cone over the imp and Ada.
- `examples/saves/save-first.nql`: a Sacred Flame at the bandit, scored
  before any damage is rolled.
- `tests/save_test.go`: every rule below, at its bounds, and thousands of
  random saves against an independent model of the law.

## Facts and totals

| The host controls | The engine derives |
| --- | --- |
| who forces the save on whom, the stat, the DC, advantage or disadvantage | the roll mode, with the target's Conditions; the faces it needs, and the face that counts |
| the faces rolled | the total, success and margin |
| the damage profile and the effect of a success | the effect: full, half or none |
| the damage dice as rolled, summed | the range check, the bonus, the halving, the damage applied |
| the check faces for the target's Conditions | the checks the damage triggers, their DC, and their outcome |
| the target's traits and Conditions, in the world | the treatments the damage meets |

## The law

A save uses the world's `attacks` law (docs/ATTACKS.md): its die is the
save's die, its `health` the Resource damage lowers, and its damage traits
the ones a save's damage meets. A world without the law refuses a save.

## The save

From `sacred-flame.nql`:

```nql
save "char:eirik" profile stat "save:dexterity" dc 15 damage 2d8 kind "radiant" success none against "occ:valley/B03/spider-1" faces 19 damage 13;
```

`save "<source>" profile stat "<stat>" dc <n> [damage <c>d<s>[+<b>|-<b>]
kind "<kind>" success half | none] against "<target>" [advantage |
disadvantage] [faces <a> [<b>]] [added "<condition type>" <f>...]... [damage
<n>] [faces "<instance>" <f> ...];`

- **profile:** the stat the target rolls, any stat it declares, and the DC,
  any 64-bit integer. The damage, optional, is as an attack's: a count of 1
  to 64, sides of 1 to 1,000, a signed bonus and any valid kind. `success`
  is the effect of a successful save on it: `half` or `none`.
- **the roll:** the save is a stat check on the target's stat, with the
  law's die and the DC: the requested mode combined with the stat's roll
  sources. Any automatic failure fails; advantage and disadvantage cancel.
  Success is the total reaching the DC. A natural 1 or 20 has no rule of
  its own.
- **faces:** at most two, each on the law's die. The engine takes those the
  mode needs, in order: none for an automatic failure, the first for a
  plain roll, both with advantage or disadvantage. The rest are recorded in
  `unused_save_faces`, so a host can send two prerolled faces without
  knowing the target's mode. Too few faces for the mode are refused.
- **added dice:** a die in force on the save's stat (docs/GRAMMAR.md
  "Added-die Condition modifiers", Bless) is added after the kept face. The
  Save never draws, so each die in force needs its `added` faces, sent as
  `added_faces` (E_ROLL, field `added`, naming the die); a clause for a type
  with no die in force is refused, as is any clause on an automatic failure.
  Static limits are as for a check (E_SAVE, field `added faces`).
- **the effect:** on a failed save, the full damage; on a successful one,
  `half` or `none`.
- **damage:** `damage <n>` is the sum of the damage dice as rolled. The
  engine adds the bonus, floors the result at 0, halves it, rounding down,
  for `half`, and then applies the target's damage traits.
  - The total is range-checked when the effect needs it: from the count to
    the count times the sides.
  - For `none`, the total is recorded in `unused`, never checked.
  - Without a total, the save is scored and nothing is applied. `effect`
    still says what a total would do (section "Save first").
  - Without a damage profile, the save only returns its verdict: the host
    applies a control spell's Conditions from `success`. A total is then
    refused.
- **check faces:** `faces "<instance>" <f>` are the target's dice for a
  check the damage triggers, as on an attack: in check order, and those no
  check takes are recorded in `unused_faces`.

Several saves in one request apply in order and atomically. One damage roll
for several targets is the same `damage <n>` on each save.

## The receipt

A save's events are the Damage's, when damage is applied, then one
`SaveScored`, which closes the operation; replay rebuilds the save from its
`intent`. nql-apply also lists every scored save in the response's `saves`,
in operation order. The spider saves:

```json
{
  "intent": {"source": "char:eirik",
             "profile": {"stat": "save:dexterity", "dc": 15,
                         "damage": {"count": 2, "sides": 8, "bonus": 0, "kind": "radiant", "success": "none"}},
             "target": "occ:valley/B03/spider-1", "faces": [19], "damage": 13},
  "check": {"intent": {"character": "occ:valley/B03/spider-1", "stat": "save:dexterity", "sides": 20, "dc": 15, "faces": [19]},
            "faces": [19], "kept": 19, "bonus": 3, "total": 22, "success": true, "margin": 7},
  "effect": "none", "unused": ["damage"]
}
```

| Field | Means |
| --- | --- |
| `intent` | the save as sent |
| `check` | the save as a stat check on the target: `roll`, `roll_sources`, `faces`, `kept`, `added`, `bonus`, `total`, `success`, `margin`, as for a `check` |
| `effect` | `full`, `half` or `none`, when the profile has damage |
| `damage_rolled` | the total used |
| `raw_damage` | it plus the bonus, exact |
| `treatments` | every source of a resistance, vulnerability or immunity the damage met |
| `damage` | the amount the Damage was asked for, after the effect and the treatments; absent when the save applied nothing |
| `unused` | `damage`, when the effect did not need it |
| `unused_save_faces` | the faces the roll mode did not need |
| `unused_faces` | the check faces no check took |

`burning-hands.nql`, one roll of 10 for both targets:

```nql
save "occ:valley/B03/cultist-1" profile stat "save:dexterity" dc 13 damage 3d6 kind "fire" success half against "occ:valley/B03/imp-1" faces 12 damage 10;
save "occ:valley/B03/cultist-1" profile stat "save:dexterity" dc 13 damage 3d6 kind "fire" success half against "char:ada" faces 4 damage 10 faces "char:ada:concentration" 15;
```

```json
[
  {
    "intent": {"source": "occ:valley/B03/cultist-1",
               "profile": {"stat": "save:dexterity", "dc": 13,
                           "damage": {"count": 3, "sides": 6, "bonus": 0, "kind": "fire", "success": "half"}},
               "target": "occ:valley/B03/imp-1", "faces": [12], "damage": 10},
    "check": {"intent": {"character": "occ:valley/B03/imp-1", "stat": "save:dexterity", "sides": 20, "dc": 13, "faces": [12]},
              "faces": [12], "kept": 12, "bonus": 3, "total": 15, "success": true, "margin": 2},
    "effect": "half", "damage_rolled": 10, "raw_damage": "10",
    "treatments": [{"character": "occ:valley/B03/imp-1", "treatment": "resistance"}],
    "damage": 2
  },
  {
    "intent": {"source": "occ:valley/B03/cultist-1",
               "profile": {"stat": "save:dexterity", "dc": 13,
                           "damage": {"count": 3, "sides": 6, "bonus": 0, "kind": "fire", "success": "half"}},
               "target": "char:ada", "faces": [4], "damage": 10,
               "check_faces": [{"condition": "char:ada:concentration", "face": 15}]},
    "check": {"intent": {"character": "char:ada", "stat": "save:dexterity", "sides": 20, "dc": 13, "faces": [4]},
              "faces": [4], "kept": 4, "bonus": 1, "total": 5, "success": false, "margin": -8},
    "effect": "full", "damage_rolled": 10, "raw_damage": "10", "damage": 10
  }
]
```

The imp saves: half of 10 is 5, and its resistance halves that to 2, the
save first as the rules order it. Ada fails and takes 10: her 5 temporary
hit points absorb 5, her hit points go from 24 to 19, and her concentration
holds (15 + 2 against DC 10).

## Save first

A host can score the save before asking for damage dice: a saved Sacred
Flame needs none. `save-first.nql` sends the save alone, at the restrained
bandit, with two prerolled faces:

```nql
save "char:eirik" profile stat "save:dexterity" dc 15 damage 2d8 kind "radiant" success none against "occ:valley/B03/bandit-1" faces 18 7;
```

```json
{
  "intent": {"source": "char:eirik",
             "profile": {"stat": "save:dexterity", "dc": 15,
                         "damage": {"count": 2, "sides": 8, "bonus": 0, "kind": "radiant", "success": "none"}},
             "target": "occ:valley/B03/bandit-1", "faces": [18, 7]},
  "check": {"intent": {"character": "occ:valley/B03/bandit-1", "stat": "save:dexterity", "sides": 20, "dc": 15, "faces": [18, 7]},
            "roll": "disadvantage",
            "roll_sources": [{"instance": "occ:valley/B03/bandit-1:web", "type": "state:restrained",
                              "modifier": "save:dexterity", "stat": "save:dexterity", "roll": "disadvantage"}],
            "faces": [18, 7], "kept": 7, "bonus": 1, "total": 8, "success": false, "margin": -7},
  "effect": "full"
}
```

The webbing gives disadvantage, so the lower face counts and the bandit
fails. `effect` is `full`: the host asks for 2d8 and sends the same save
with `damage <n>`, which applies it. The first call applied nothing.

## Refusals

Every refusal applies nothing of the request.

| Code | Field | Expected | Actual | Means |
| --- | --- | --- | --- | --- |
| E_SAVE | `law` | `world attacks law` | `absent` | the world declares no `attacks` |
| E_SAVE | `damage dice`, `damage sides`, `damage kind` | the bound | the value | a damage profile out of bounds |
| E_SAVE | `success` | `half or none` | the value | another effect handed to the engine directly |
| E_SAVE | `damage` | `a damage profile` | `absent` | a total without a damage profile |
| E_SAVE | `damage` | `<count>..<count x sides>` | the total | the total the effect needs is off its dice |
| E_SAVE | `mode` | `omitted, advantage or disadvantage` | the mode | another mode handed to the engine directly |
| E_SAVE | `faces` | `at most 2 faces` or `faces in 1..<die>` | the count, or the face | faces out of bounds |
| E_ROLL | `faces` | the count the mode needs | the count | too few faces for the roll mode |
| E_SAVE | `check faces` | `at most 128`, or `valid instance and face 1..1000` | the count, or the face | check faces out of bounds |
| E_REFERENCE, E_TYPE | `reference` | `character` | `missing`, or the kind | a source or target that is not a character |
| E_STAT | the stat | `declared stat` | `missing` | the target does not declare the stat |
| E_RESOURCE | the health Resource | `declared resource` | `missing` | a target of damage without the health Resource |
| E_OVERFLOW | the stat, or `damage` | `int64` | the value | a total, a margin or a damage past 64 bits |

At compile, the fault is at the token: the stat, the count, the sides, the
kind, the effect, the mode, the face, the total, the target; otherwise at
`save`.

## What the engine does not decide

- Who must save, and against what: the host, from its spell or stat block
  data. The engine never reads a spell's words.
- The DC: the host's number, from the caster's sheet or the stat block.
- A control spell's Conditions: the host applies them from `success`.
- Spell slots: spend them with `spend` in the same request.
