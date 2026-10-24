# Character stats

The engine computes the numbers on a character sheet: ability modifiers,
proficiency bonus, ability checks, saving throws, skills, initiative, passive
Perception, spell save DC, spell attack bonus and the XP the next level needs.
They are derived from facts that an agent controls: level, experience points,
ability scores, proficiencies, features and effects. An agent never writes a total. It changes a fact, and every total that
depends on that fact follows in the same transaction, with an explanation of
each term.

"Agent" means any caller that updates a character: a DM model, a level-up
routine, a sheet editor. The agent decides what changes; the engine applies the
change atomically, refuses what the world cannot hold, and does the arithmetic.

The rules follow SRD 5.2.1. The files:

- `examples/srd-stats/world.nql`: the rules and three sample characters.
- `examples/srd-stats/level-up.nql`, `proficiency.nql` and `conditions.nql`:
  three agent updates; `checks.nql`: checks and saves; `concentration.nql`:
  concentration; `xp.nql`: an experience award.
- `tests/character_stats_test.go`: every number below, worked by hand.

## Facts and totals

| The agent controls | The engine derives |
| --- | --- |
| `level`, `speed` and the ability scores `strength`, `dexterity`, `constitution`, `intelligence`, `wisdom`, `charisma` | `proficiency`, `mod:<ability>`, `check:<ability>`, `save:<ability>`, `skill:<skill>`, `initiative`, `passive:perception`, `spell-dc:<ability>`, `spell-attack:<ability>` |
| `xp`, by each award | `xp:next`, the XP the next level needs, and `xp:pending`, the levels the XP has earned beyond `level` |
| proficiency, expertise, feature and feat Conditions | which proficiency terms count |
| effects on the bonus pools `bonus:saves`, `bonus:checks`, `bonus:d20` | the totals that read those pools |
| SRD condition instances (`state:<name>`) and hit points | speed 0, Exhaustion penalties, Unconscious at 0 hit points, the conditions' advantage, disadvantage and automatic failures |
| a check the DM names: stat, DC, advantage or disadvantage, the player's faces | the roll mode, the kept face, the total, success and margin |

Skill IDs: `acrobatics`, `animal-handling`, `arcana`, `athletics`,
`deception`, `history`, `insight`, `intimidation`, `investigation`,
`medicine`, `nature`, `perception`, `performance`, `persuasion`, `religion`,
`sleight-of-hand`, `stealth`, `survival`, each with its SRD ability.

| Total | Declared base | Adds |
| --- | --- | --- |
| `proficiency` | 0 | floor((level + 7) / 4), the SRD table for levels 1 to 20 |
| `mod:<a>` | 0 | floor((score - 10) / 2) |
| `check:<a>` | 0 | modifier, `bonus:checks`, `bonus:d20` |
| `save:<a>` | 0 | modifier, proficiency if `prof:save:<a>`, `bonus:saves`, `bonus:d20` |
| `skill:<s>` | 0 | modifier, proficiency if `prof:skill:<s>`, proficiency again if `expertise:skill:<s>`, half proficiency if `feature:jack-of-all-trades` and not `prof:skill:<s>`, `bonus:checks`, `bonus:d20` |
| `initiative` | 0 | Dexterity modifier, proficiency if `feat:alert`, `bonus:checks`, `bonus:d20` |
| `passive:perception` | 10 | the Perception skill terms and `bonus:checks`; no `bonus:d20`, since a passive score is not a roll |
| `spell-dc:<a>` | 8 | modifier, proficiency |
| `spell-attack:<a>` | 0 | modifier, proficiency |

A derived rule reads only non-derived stats, so each total restates the
modifier and proficiency terms rather than reading `mod:<a>` or `proficiency`.
Half proficiency is floor((level + 7) / 8), exact for levels 1 to 20.

Engine checks read these totals directly: `check "x" die 20 actor_roll plus
stat param "actor" "skill:history" against 10` rolls with the full History
bonus. A check or save the DM calls for is one action; see
[Checks and saves](#checks-and-saves).

## Setting up a world

Copy the marked blocks of `world.nql`; [Conditions](#conditions) covers the
third, [Concentration](#concentration) the fourth and
[Experience points](#experience-points) the fifth. The first, between
`// BEGIN srd-stats:conditions` and its END line, declares 44 unique Condition
types:

```nql
condition type "prof:save:dexterity" { instances unique; }
condition type "prof:skill:arcana" { instances unique; }
condition type "expertise:skill:arcana" { instances unique; }
condition type "feature:jack-of-all-trades" { instances unique; }
condition type "feat:alert" { instances unique; }
```

The second, between `// BEGIN srd-stats:derive` and its END line, holds 45
derived-stat rules. They go inside the world's one `equipment` block, beside
any rules it already has:

```nql
derive stat "proficiency" { term stat "level" offset 7 divide 4; }
derive stat "skill:arcana" {
 term stat "intelligence" offset -10 divide 2;
 term stat "level" offset 7 divide 4 requires "prof:skill:arcana";
 term stat "level" offset 7 divide 4 requires "expertise:skill:arcana";
 term stat "level" offset 7 divide 8 requires "feature:jack-of-all-trades" unless "prof:skill:arcana";
 term stat "bonus:checks" offset 0 divide 1;
 term stat "bonus:d20" offset 0 divide 1;
}
```

`unless` turns a term off while the character has that Condition type (see
[GRAMMAR.md](GRAMMAR.md)).

A rule applies only to a character that declares its stat. Each character
declares every total at its base, and the facts the totals read. A caster
declares a spell pair for each casting ability:

```nql
character "char:bard" named "Bard" at "loc:hall" {
 stat "strength" = 8; stat "dexterity" = 14; stat "constitution" = 12; stat "intelligence" = 10; stat "wisdom" = 13; stat "charisma" = 17;
 stat "level" = 5; stat "speed" = 30;
 stat "xp" = 6500; stat "xp:next" = 0; stat "xp:pending" = 0;
 stat "bonus:saves" = 0; stat "bonus:checks" = 0; stat "bonus:d20" = 0;
 stat "proficiency" = 0;
 ...
 stat "initiative" = 0; stat "passive:perception" = 10;
 stat "spell-dc:charisma" = 8; stat "spell-attack:charisma" = 0;
```

The elided lines declare `mod:`, `check:` and `save:` for the six abilities and
the 18 `skill:` stats, all at 0. The sample characters declare 54 stats, 56
with one casting ability; `create character` allows up to 128.

Proficiencies are Condition instances, one per fact, with the ID
`<character>:<type>`:

```nql
condition "char:bard:prof:skill:performance" of "prof:skill:performance" to "char:bard";
condition "char:bard:expertise:skill:performance" of "expertise:skill:performance" to "char:bard";
condition "char:bard:feature:jack-of-all-trades" of "feature:jack-of-all-trades" to "char:bard";
```

The level 5 bard gets Performance 3 + 3 + 3 = 9, Athletics -1 + 1 = 0 (Jack of
All Trades) and spell save DC 8 + 3 + 3 = 14.

## Updating a character

An update is one request: a list of operations the agent chooses, applied all
together or not at all.

| Change | Operation |
| --- | --- |
| Gain a level | `apply effect "<character>:level:<n>" from character "<character>" on character "<character>" stat "level" add 1;` |
| Award experience points | `adjust "<character>" stat "xp" by <n>;` |
| Ability Score Improvement, a tome, a curse | `apply effect "<character>:<reason>" ... stat "<ability>" add <n>;` |
| Hit points for the level | `expand` the maximum and `heal` the same amount, by the agent's number |
| Gain a proficiency, expertise, feature or feat | `apply condition "<character>:<type>" of "<type>" to "<character>";` |
| Lose one | `remove condition "<character>:<type>" from "<character>";` |
| Level drain, or undo a mistaken level | `remove effect "<character>:level:<n>";` |
| One total only, such as a homebrew +2 Stealth | `apply effect ... stat "skill:stealth" add 2;` |

The fighter from level 1 to 5, with the level 4 Ability Score Improvement in
Strength:

```nql
apply effect "char:fighter:level:2" from character "char:fighter" on character "char:fighter" stat "level" add 1;
apply effect "char:fighter:level:3" from character "char:fighter" on character "char:fighter" stat "level" add 1;
apply effect "char:fighter:level:4" from character "char:fighter" on character "char:fighter" stat "level" add 1;
apply effect "char:fighter:asi:4" from character "char:fighter" on character "char:fighter" stat "strength" add 2;
apply effect "char:fighter:level:5" from character "char:fighter" on character "char:fighter" stat "level" add 1;
expand "char:fighter" resource "hp" by 32;
heal "char:fighter" resource "hp" by 32;
```

Afterwards the proficiency bonus is 3, Strength 18, Strength save 4 + 3 = 7,
Athletics 7, initiative 1 + 3 = 4 (Alert) and passive Perception 14, with no
total written by the agent. A later update adds skills:

```nql
apply condition "char:fighter:prof:skill:stealth" of "prof:skill:stealth" to "char:fighter";
apply condition "char:bard:prof:skill:athletics" of "prof:skill:athletics" to "char:bard";
```

The bard's Athletics becomes -1 + 3 = 2: proficiency replaces Jack of All
Trades there.

The engine refuses a request, and changes nothing, when:

| Code | Cause |
| --- | --- |
| E_RULE | a second instance of a proficiency the character already has |
| E_DUPLICATE | an instance ID already used |
| E_CONDITION | removing an instance the character does not have |
| E_CONDITION_TYPE | a type the world does not declare, such as a misspelt skill |
| E_STAT | a stat the character does not declare, such as a spell DC for a non-caster |

The engine does not decide what a character is entitled to. Class tables,
which skills a class may choose, when Ability Score Improvements come, hit
point rolls, and that expertise needs proficiency all stay with the agent. The
engine holds whatever the agent applies and keeps the arithmetic right.

## Effects

Items, spells and states change totals through a pool, a fact, or one total:

- `bonus:saves`: every save (Cloak of Protection).
- `bonus:checks`: every ability check, skill, initiative and passive
  Perception (Stone of Good Luck adds to both pools).
- `bonus:d20`: every d20 Test, but not passive scores or spell save DCs
  (Exhaustion: -2 a level).
- a fact such as `speed` or `strength`, or one total such as `skill:stealth`.

```nql
condition type "item:cloak-of-protection" { instances unique; modifier "ac" stat "defense" add 1; modifier "saves" stat "bonus:saves" add 1; }
```

An item that sets a score bounds the fact instead of adding to it:

```nql
condition type "item:gauntlets-of-ogre-power" { instances unique; modifier "strength" stat "strength" at least 19; }
condition type "item:belt-of-giant-strength" { instances unique; modifier "strength" stat "strength" at least magnitude; }
apply condition "char:fighter:gauntlets" of "item:gauntlets-of-ogre-power" to "char:fighter";
apply condition "char:bard:belt" of "item:belt-of-giant-strength" to "char:bard" magnitude 21;
```

The fighter's Strength 16 becomes 19, and the Strength modifier, check, save
and Athletics follow. A Strength already 19 or higher is unchanged, as the SRD
says. The belt's magnitude is the giant's score, 21 (hill) to 29 (storm); with
both items the higher floor wins. `at least` applies after every addition, so
a curse's -2 leaves Strength 19 while the gauntlets are on. Durations, endings
and magnitudes work as in [TIMED_EFFECTS.md](TIMED_EFFECTS.md).

## Conditions

The third marked block, between `// BEGIN srd-stats:srd-conditions` and its END
line, declares the 15 SRD conditions as `state:<name>` types. Their numeric
parts are modifiers:

| Condition | In the engine |
| --- | --- |
| Exhaustion | -2 to `bonus:d20` and -5 to `speed` per instance; one instance is one level |
| Grappled | `speed` at most 0 |
| Restrained | `speed` at most 0; disadvantage on Dexterity saves |
| Paralyzed, Petrified | `speed` at most 0; Strength and Dexterity saves fail; disadvantage on Initiative; include Incapacitated |
| Stunned | Strength and Dexterity saves fail; disadvantage on Initiative; includes Incapacitated |
| Unconscious | `speed` at most 0; Strength and Dexterity saves fail; disadvantage on Initiative; includes Incapacitated; held while `hp` is at 0 |
| Incapacitated | disadvantage on Initiative |
| Invisible | advantage on Initiative |
| Poisoned | disadvantage on ability checks, Initiative included |
| Blinded, Charmed, Deafened, Frightened, Prone | no numbers: presence only |

```nql
condition type "state:exhaustion" { instances coexist; modifier "d20" stat "bonus:d20" add -2; modifier "speed" stat "speed" add -5; }
condition type "state:grappled" { instances coexist; modifier "speed" stat "speed" at most 0; }
condition type "state:poisoned" { instances coexist; modifier "checks" stat "bonus:checks" roll disadvantage; }
condition type "state:unconscious" { instances coexist; held_at_minimum "hp"; includes "state:incapacitated"; modifier "speed" stat "speed" at most 0; modifier "initiative" stat "initiative" roll disadvantage; modifier "save:strength" stat "save:strength" roll fail; modifier "save:dexterity" stat "save:dexterity" roll fail; }
```

A `roll` modifier sets how checks roll, not a total: `advantage`,
`disadvantage` or `fail`, an automatic failure. It reaches every check whose
stat reads the named one, so Poisoned's reaches every ability check, skill and
Initiative through `bonus:checks`, and Unconscious's reach one save each.

`includes "state:incapacitated"` makes Incapacitated present wherever
Paralyzed, Petrified, Stunned or Unconscious is, as the SRD says: a type that
ends on Incapacitated, such as concentration, ends when any of them begins and
cannot start while one is present, and `requires` and `unless` terms see it.
Inclusion is presence only. Incapacitated's own modifier reaches a character
through a `state:incapacitated` instance, so each including type carries the
Initiative disadvantage itself. The Prone that Unconscious leaves behind
outlasts it, so the agent applies `state:prone` as its own instance.

`at most 0` holds speed at 0 after every addition, so Haste cannot raise it:
"Your Speed is 0 and can't increase." A ceiling wins over a floor.

Every type is `coexist`: each source is its own instance and ends on its own,
so two grapplers are two instances, and speed stays 0 until both let go. A
condition still does not stack with itself, as the SRD says: a bound does not
add up, and the other types carry no numbers. Exhaustion is the SRD's
exception, one instance per level.

Unconscious at 0 hit points is held, not applied. While `hp` is at its
minimum the character has `state:unconscious` with no instance, ID or event;
any healing ends it. `status[].held_conditions` lists it. A type that ends on
Unconscious (`ends_on_condition`) ends when the damage lands and cannot start
at 0 hit points, and `requires` and `unless` terms see it. An instance of
`state:unconscious` covers the other SRD cases, such as a creature knocked out
at 1 hit point, and lasts until the agent removes it.

From `conditions.nql`:

```nql
apply condition "char:fighter:grappled:ogre" of "state:grappled" to "char:fighter" from character "char:rogue";
apply condition "char:fighter:exhaustion:1" of "state:exhaustion" to "char:fighter";
apply condition "char:fighter:exhaustion:2" of "state:exhaustion" to "char:fighter";
...
damage "char:bard" resource "hp" by 40;
```

The fighter's speed is 0 and every d20 Test is 4 lower; passive Perception
stays 13, since it is not a roll. The bard drops to 0 hit points and is
Unconscious with speed 0 until healed.

| Change | Operation |
| --- | --- |
| Gain a condition | `apply condition "<id>" of "state:<name>" to "<character>";`, with `from character "<id>"` for a grappler, charmer or source of fear |
| End it | `remove condition "<id>" from "<character>";` |
| Gain an Exhaustion level | apply one more `state:exhaustion` instance |
| Finish a Long Rest | remove one `state:exhaustion` instance, in the same request as the rest's other changes |
| Drop to 0 hit points | `damage`; Unconscious follows. The SRD leaves the character Prone when it ends, so the agent applies `state:prone` as the character falls |
| A timed condition | add `ticks <n>`, as in [TIMED_EFFECTS.md](TIMED_EFFECTS.md) |

An instance ID is used once in a world's history: E_DUPLICATE refuses it
after removal. A host that keeps its checkpoint gives each new Exhaustion
level a fresh ID, such as one built from the request ID. A host that rebuilds
the world per call can number levels 1, 2, 3.

Apart from Unconscious at 0 hit points and concentration, the engine does not
decide when a condition begins or ends: it resolves the save or escape check
the agent asks for, and the agent decides what the result ends. Durations and the Long Rest
stay with the agent. The engine holds what the agent applies and keeps speed,
d20 Tests and their roll modes right.

## Checks and saves

The DM names the check; the engine takes the player's faces or rolls, adds the
total and compares it with the DC. From `checks.nql`:

```nql
check "char:fighter" stat "skill:athletics" die 20 against 15 faces 17 6;
check "char:bard" stat "save:dexterity" die 20 against 13 advantage faces 4 15;
check "char:rogue" stat "skill:stealth" die 20 against 25;
check "char:fighter" stat "initiative" die 20;
```

`against <dc>` is optional: without it the check reports its total only, as
for Initiative or each side of a contest, where the DM compares the two
totals. `advantage` or `disadvantage` is the DM's call for the situation.
`faces` are the player's dice. Without them the world's dice roll:

```nql
dice seed 20 25;
```

A host picks a fresh seed for every genesis from its own random source; the
same seed rolls the same faces. A world with an Adventure or a Battle rolls
from that and cannot declare dice (E_DICE).

The check combines the requested mode with the conditions' modes: any `fail`
is an automatic failure with no dice, advantage and disadvantage cancel, and
neither stacks. Faces must match the result: one, two with advantage or
disadvantage, none for an automatic failure. Anything else is E_ROLL, which
names the count, such as `2 faces (disadvantage)`. Before asking the player to
roll, a host reads the stat's `roll` from status or explain: its net mode from
conditions, with `roll_sources` naming each condition.

A condition may also add a die to a stat's checks, rolled after the d20 is
kept: Bless adds 1d4 to saves through `bonus:saves`, Guidance 1d4 to one
skill's checks (docs/GRAMMAR.md "Added-die Condition modifiers"). Status and
explain list them under `added_dice`, so the host can ask for those faces too;
a check sends them as `added "<condition type>" <faces>`, and a die without
them is rolled by the world's dice. One spell's die never stacks with itself.
With Guidance on the fighter's Athletics, the player rolls 10 and 3:

```json
{"intent":{"character":"char:fighter","stat":"skill:athletics","sides":20,"dc":15,"faces":[10],"added":[{"type":"spell:guidance","faces":[3]}]},"faces":[10],"kept":10,"added":[{"instance":"char:fighter:guidance","type":"spell:guidance","modifier":"guidance","stat":"skill:athletics","count":1,"sides":4,"faces":[3],"sum":3}],"bonus":5,"total":18,"success":true,"margin":3}
```

After `checks.nql` applies Poisoned to the fighter and Restrained to the
rogue, and drops the bard to 0 hit points:

| Check | Faces | Result |
| --- | --- | --- |
| Poisoned fighter, Athletics 5, DC 15 | 17 and 6, disadvantage | 6 + 5 = 11, fails by 4 |
| Bard, Dexterity save 5, DC 13, advantage | 4 and 15 | 15 + 5 = 20, succeeds by 7 |
| Rogue, Stealth 17, DC 25 | the dice, once | face + 17 |
| Restrained rogue, Dexterity save 11, DC 15, advantage | 12: the modes cancel | 23, succeeds by 8 |
| Bard at 0 hit points, Dexterity save, DC 13 | none | automatic failure |
| Bard at 0 hit points, Wisdom save 1, DC 13 | 11 | 12, fails by 1 |
| Poisoned fighter, Initiative 3 | the dice, twice | the lower face + 3 |

Each check is a `StatCheckResolved` event in the receipt, and nql-apply lists
them in order under `checks`. The fighter's Athletics:

```json
{"intent": {"character": "char:fighter", "stat": "skill:athletics", "sides": 20, "dc": 15, "faces": [17, 6]},
 "roll": "disadvantage",
 "roll_sources": [{"instance": "char:fighter:poisoned", "type": "state:poisoned", "modifier": "checks", "stat": "bonus:checks", "roll": "disadvantage"}],
 "faces": [17, 6], "kept": 6, "bonus": 5, "total": 11, "success": false, "margin": -4}
```

`bonus` is the stat's total and `margin` is the total minus the DC. `drawn`
is true when the dice rolled. An automatic failure has `"roll": "fail"`,
`"success": false` and no faces or total. A check changes nothing but the
dice: what a failed save does, such as damage or a condition, is the agent's
next operation, in the same request or a later one.

## Reading the totals

Ask `nql-apply` for `"status": ["<character>"]`. Each `status[].stats[]` entry
is one stat with its `effective` total, its declared `base`, its effect
`contributions`, and, for a derived total, `recipe.terms[]` with the input and
whether each term counted. The bard's Athletics:

```json
{"stat": "skill:athletics", "base": 0, "effective": 0, "contributions": [],
 "recipe": {"base": 0, "terms": [
  {"term": {"stat": "strength", "offset": -10, "divisor": 2}, "input": 8, "active": true, "exact": "-1"},
  {"term": {"stat": "level", "offset": 7, "divisor": 4, "requires": "prof:skill:athletics"}, "input": 5, "active": false, "exact": "0"},
  {"term": {"stat": "level", "offset": 7, "divisor": 4, "requires": "expertise:skill:athletics"}, "input": 5, "active": false, "exact": "0"},
  {"term": {"stat": "level", "offset": 7, "divisor": 8, "requires": "feature:jack-of-all-trades", "unless": "prof:skill:athletics"}, "input": 5, "active": true, "exact": "1"},
  {"term": {"stat": "bonus:checks", "offset": 0, "divisor": 1}, "input": 0, "active": true, "exact": "0"},
  {"term": {"stat": "bonus:d20", "offset": 0, "divisor": 1}, "input": 0, "active": true, "exact": "0"}]}}
```

A bound shows under `bounds`, with `applied` true when it set the total. The
grappled, twice-exhausted fighter's speed:

```json
{"stat": "speed", "base": 30, "effective": 0,
 "contributions": [
  {"condition_origin": {"instance": "char:fighter:exhaustion:1", "modifier": "speed"}, "amount": -5, ...},
  {"condition_origin": {"instance": "char:fighter:exhaustion:2", "modifier": "speed"}, "amount": -5, ...}],
 "bounds": [{"instance": "char:fighter:grappled:ogre", "type": "state:grappled", "modifier": "speed", "bound": "at_most", "amount": 0, "applied": true}]}
```

A held type's bound names `"held_at_minimum": "hp"` instead of an instance.

A prompt needs only the totals. One compact form:

```text
PB +3  INIT +2  PASSIVE 12  SPELL DC 14  SPELL ATK +6
SAVES str -1, dex +5*, con +1, int +0, wis +1, cha +6*
SKILLS acrobatics +3, ..., performance +9**, persuasion +9**, ...
```

Conditions print by name from `status[].conditions` and
`status[].held_conditions`, Exhaustion as its level:
`CONDITIONS grappled, exhaustion 2`.

## Concentration

The fourth marked block, between `// BEGIN srd-stats:concentration` and its
END line, holds concentration:

```nql
condition type "state:concentration" { instances replace; ends_on_condition "state:incapacitated"; }
condition_check "state:concentration" after_damage resource "hp" stat "save:constitution" sides 20 minimum 10 divide 2 maximum 30;
```

The caster holds one `state:concentration` instance, and the spell's effects
are Conditions anchored to it, on whoever they affect. The engine ends the
concentration, and everything anchored to it, when the SRD does:

| Cause | In the engine |
| --- | --- |
| Another concentration effect | `instances replace`: the new instance ends the old one |
| Damage | a Constitution save against DC 10 or half the damage, up to 30; a failed save ends it |
| Incapacitated, including at 0 hit points | `ends_on_condition`: Incapacitated and every type that includes it end it, and it cannot start while one is present (E_CONDITION) |
| The caster stops concentrating | `remove condition` |
| The spell's duration | `ticks <n>` on the concentration, as in [TIMED_EFFECTS.md](TIMED_EFFECTS.md) |

From `concentration.nql`:

```nql
apply condition "char:bard:concentration" of "state:concentration" to "char:bard";
apply condition "char:fighter:shield-of-faith" of "spell:shield-of-faith" to "char:fighter" from character "char:bard" anchor "char:bard:concentration";
damage "char:bard" resource "hp" by 12 faces "char:bard:concentration" 9;
damage "char:bard" resource "hp" by 20 faces "char:bard:concentration" 4;
```

Shield of Faith raises the fighter's defense from 11 to 13. Both hits are DC
10. The bard's Constitution save is 1: the player's 9 makes 10 and holds, and
the 4 makes 5 and fails, ending the concentration and the shield.

`faces "<instance>"` are the player's dice for the save that damage calls for
on that instance; without them the world's dice roll. Roll modes on
`save:constitution` apply as they do to `check`. Each save is a
`StatCheckResolved` event, listed under nql-apply's `checks` with the instance
and the damage:

```json
{"intent": {"character": "char:bard", "stat": "save:constitution", "sides": 20, "dc": 10, "faces": [4]},
 "condition": "char:bard:concentration", "damage": 20,
 "faces": [4], "kept": 4, "bonus": 1, "total": 5, "success": false, "margin": -5}
```

In `ended_conditions` the concentration's reason is `check` and the shield's is
`anchor`. Damage that drops the caster to 0 hit points ends the concentration
through Unconscious, before any save. One damage operation is one save, so the
agent applies damage from two sources as two operations.

## Experience points

The fifth marked block, between `// BEGIN srd-stats:xp` and its END line, goes
inside the equipment block beside the derived stats. It holds two rules over
the SRD Character Advancement table:

| Total | Declared base | Adds |
| --- | --- | --- |
| `xp:next` | 0 | the XP the level after `level` needs: 300 at level 1, 900 at level 2, up to 355,000 at level 19, and 0 at level 20 |
| `xp:pending` | 0 | the level the XP reaches, minus `level`: the level-ups the XP has earned |

Each reads a stat through a `step` table (see [GRAMMAR.md](GRAMMAR.md)):

```nql
derive stat "xp:next" {
 term stat "level" offset 0 divide 1
  step 1 = 300 step 2 = 900 step 3 = 2700 step 4 = 6500 step 5 = 14000
  ...
  step 16 = 225000 step 17 = 265000 step 18 = 305000 step 19 = 355000 step 20 = 0;
}
```

Each character declares `xp` at its XP and both totals at 0:

```nql
 stat "xp" = 6500; stat "xp:next" = 0; stat "xp:pending" = 0;
```

An award is one action per character, and the agent decides the amounts, such
as an encounter's XP split between the party. From `xp.nql`:

```nql
adjust "char:fighter" stat "xp" by 300;
adjust "char:bard" stat "xp" by 300;
adjust "char:rogue" stat "xp" by 300;
```

The fighter's 300 reaches level 2: `xp:pending` becomes 1, and `xp:next` stays
300. The bard's 6,800 and the rogue's 230,300 stay short of 14,000 and
265,000. The engine never takes the level. The agent applies it when the
player does, and `xp:pending` returns to 0 while `xp:next` becomes 900:

```nql
apply effect "char:fighter:level:2" from character "char:fighter" on character "char:fighter" stat "level" add 1;
```

An award that crosses two thresholds makes `xp:pending` 2. At level 20
`xp:next` is 0 and more XP earns no level. A negative amount corrects a
mistaken award. Levels taken without XP, as in a milestone game, make
`xp:pending` negative: level 5 at 0 XP is -4.

`adjust` changes the stat's declared base rather than adding an effect, so
awards never pile up, and status reports `xp` with its new `base`. It refuses a
derived total (E_RULE), a stat the character does not declare (E_STAT) and a
zero amount (E_AMOUNT).

## Hosts that rebuild the world per call

A host that keeps the returned checkpoint keeps every effect and Condition as
it is. A host that writes a fresh world from its own store for each call
stores facts, never totals:

- Level and ability scores: `base` plus the contributions that have no
  `condition_origin` or `equipment_origin`, which are the agent's permanent
  changes. Condition and equipment contributions stay with their Condition or
  item.
- Proficiencies, expertise, features and feats: the `prof:`, `expertise:`,
  `feature:` and `feat:` instances in `status[].conditions`.
- SRD conditions: the `state:` instances in `status[].conditions`, one per
  source and one per Exhaustion level.
- Concentration: the `state:concentration` instance and each spell instance
  with the `anchor` it was applied with. `ended_conditions` names every
  instance the engine ended.
- Never `held_conditions`: the stored hit points bring Unconscious back.
- Experience points: the `base` of `xp`, which each award changes.
- Never the dice state: each genesis takes a fresh `dice seed`.
- Totals: declare each at its base (0, 10 or 8). A total declared at its value
  would count twice.

## Hosts that keep character sheets

A host that keeps its own character sheets stores each field as a fact the
engine holds, or shows it as a total the engine derives:

| Sheet field | Engine |
| --- | --- |
| level, speed, ability scores | the stats of the same name |
| saving throw proficiencies | `prof:save:<ability>`, the full lower-case ability name |
| skill proficiencies | `prof:skill:<id>`, lower case with hyphens: Sleight of Hand is `sleight-of-hand` |
| expertise | `expertise:skill:<id>` |
| features and feats, such as Jack of All Trades and Alert | `feature:jack-of-all-trades`, `feat:alert` |
| spellcasting ability | declare `spell-dc:<ability>` = 8 and `spell-attack:<ability>` = 0 |
| active conditions | one `state:<name>` instance per condition, and one per Exhaustion level |
| Unconscious at 0 hit points | nothing to store: 0 hit points hold it |
| experience points | `xp`: an award is `adjust ... by <n>`, not a new total |
| proficiency bonus, initiative, spell save DC and attack bonus, skill and save bonuses | engine output: show them from `status`; no agent writes them |
| experience needed for the next level | engine output: `xp:next`; `xp:pending` above 0 means a level-up is available |

With the totals read from `status`, an agent that runs or checks a turn reads
numbers instead of recomputing them, and an agent that updates a character
states facts instead of writing and checking derived totals.

## Limits

- An effect on Perception must target both `skill:perception` and
  `passive:perception`.
- Each skill uses its default ability. For a variant such as Strength
  (Intimidation), add the proficiency bonus to `check:strength` when the
  character is proficient.
- A passive score shows the `roll` of the checks it reads, but the SRD's +5
  and -5 for advantage and disadvantage on passive Perception and on an
  Initiative score are not applied to its total.
- Situational modes stay with the DM, who asks for `advantage` or
  `disadvantage` on the check: Blinded's failed sight checks, Frightened's
  disadvantage while the source is in sight, cover, help. So do attack rolls
  against a creature, critical hits within 5 feet, resistance and what an
  Incapacitated character cannot do.
- An included type brings presence, not modifiers: each type that includes
  Incapacitated carries its Initiative disadvantage itself.
- A feature that gives advantage on concentration saves only, such as War
  Caster, is the player's to roll: the player rolls both dice and the damage
  brings the higher face. Advantage on every Constitution save is a `roll`
  modifier on `save:constitution`.
- A special speed (climb, fly, swim) is reduced with Speed in the SRD. A world
  that declares one adds its own `at most 0` modifier to each speed-0 type.
- Death stays with the agent: Exhaustion level 6, three failed Death Saving
  Throws, or massive damage.
- Dice bonuses such as Bless and Guidance belong to the roll, not the total,
  and `check` does not roll them: the DM adds them to the reported total.
- Levels outside 1 to 20 are not refused; the proficiency formula and the XP
  tables match the SRD only inside that range.
- The engine does not award XP for an encounter or split it between the
  party: the agent states each character's amount.
- The derive and XP blocks use 47 of the 64 derived rules allowed per world,
  with at most 6 terms of the 8 allowed per rule, and 20 steps of the 32
  allowed per term.
