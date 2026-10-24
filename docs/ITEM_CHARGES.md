# Item charges

An item definition can give its items a charge count. Charges are spent
exactly, or the request is refused. Spending grants no spell, damage or other
power. Any consequence the engine supports goes in the same request, so a
failure rolls both back. Charges build on [item definitions](ITEM_CATALOG.md).

A definition may also say how charges come back, how a new item's count is
rolled, and what happens when the last charge goes. Recharge is worked out when
charges are read, from the item's stored count and the world clock. Advancing
time does no charge work, so the result never depends on how a host splits time
into calls.

## Declare a charged item, then spend

This synthetic example is [world.nql](../examples/item-charges/world.nql):

```nql
// Synthetic example data, not a published game item.
rules { transfer unequips; wear any; }
location "loc:camp" named "Camp";
character "char:ada" named "Ada" at "loc:camp" {}
item type "itemdef:wand-of-sparks" named "Wand of sparks" {
 description "A charred birch wand.";
 kind "wand"; magical true;
 charges 7;
}
item "item:wand-1" from "itemdef:wand-of-sparks" {
 owner "char:ada"; custody character "char:ada"; charges 3 of 7;
}
```

[use.nql](../examples/item-charges/use.nql) spends two of the three charges:

```nql
expend 2 charges of "item:wand-1" by "char:ada";
```

For `nql-apply`, pass the first file as `world` and the second as `actions`,
with `actor:{"kind":"character","id":"char:ada"}`, `request:"use:1"` and
`item_charges:["item:wand-1"]`. The `item_charges` response is:

```json
[
  {
    "revision": 1,
    "item": "item:wand-1",
    "owner": "char:ada",
    "max": 7,
    "current": 1,
    "available": 1
  }
]
```

The receipt has one `ChargesExpended` event, with the amount and the count
before and after. Spending a third charge from one is refused with `E_CHARGES`;
the world does not change.

## Recharge at dawn

[recharge.nql](../examples/item-charges/recharge.nql) has a clock in seconds.
Its staff regains 1d6+4 charges each day at 06:00 (offset 21,600 in a day of
86,400). The sheet says the staff was empty as of tick 100,000; the world is now
at tick 120,000, so one dawn (108,000) has passed:

```nql
// Synthetic example data, not a published game item.
rules { transfer unequips; wear any; }
clock "second" at 120000;
location "loc:camp" named "Camp";
character "char:ada" named "Ada" at "loc:camp" {}
item type "itemdef:staff-of-embers" named "Staff of embers" {
 description "A blackened oak staff.";
 kind "staff"; magical true;
 charges 10;
 recharge every 86400 at 21600 dice 1 sides 6 plus 4;
 exhausted roll 20 destroy 1;
}
item "item:staff-1" from "itemdef:staff-of-embers" {
 owner "char:ada"; custody character "char:ada"; charges 0 of 10 since 100000 seed 6 2;
}
```

[spend-staff.nql](../examples/item-charges/spend-staff.nql) spends three:

```nql
expend 3 charges of "item:staff-1" by "char:ada";
```

Called like the first example, with `request:"use:1"` and
`item_charges:["item:staff-1"]`, the dawn's roll gave 6. The event records
`before` 6, `after` 3, `clock` `current` and `as_of` 120000, and the view is:

```json
[
  {
    "revision": 1,
    "item": "item:staff-1",
    "owner": "char:ada",
    "max": 10,
    "current": 3,
    "available": 3,
    "as_of": 120000,
    "seed": [
      6,
      2
    ],
    "clock": "current",
    "next_recharge": 194400
  }
]
```

## Data contract

`ItemDefinition.Charges` (`charges`) is optional:
- `max`: 1 to 1000.
- `initial`: 0 to `max`, or `initial_dice` (`initial dice C sides S [plus B];`),
  whose lowest roll must not exceed `max`. A roll above `max` is capped. If both
  are omitted, a new item starts full.
- `recharge` (`recharge every P [at O] AMOUNT;`): the item regains charges at
  every tick `k*P + O`, with `P` at least 1 and `O` below `P`. AMOUNT is
  exactly one of:
  - `all`: the item becomes full;
  - `by N`: N from 1 to 1000;
  - `dice C sides S [plus B]`.
- `exhausted`, applied when a spend takes the last charge:
  - `exhausted destroy;`: the unit is always destroyed;
  - `exhausted roll S [destroy D] [regain R dice C sides S [plus B]];`: one
    S-sided die. A face of D or less destroys the unit. A face of R or more
    regains the rolled amount, capped at `max`. Anything else does nothing. At
    least one threshold is required, and R must be above D.

Dice take 1 to 20 dice of 2 to 1000 sides, plus 0 to 10,000. Source declares
each rule once, inside the item type, and every rule requires `charges`.

`Item.Charges` (`charges`) holds:
- `current`: from 0 to `max`.
- `max`: follows the catalog rule. Empty derives it from the definition, an
  equal value is accepted, and a different value is refused.
- `as_of`: the tick through which recharge has been counted. Only items with a
  recharge rule carry it. Without it, the item is unanchored.
- `seed`: two unsigned 64-bit words that key the item's rolls. An item carries a
  seed exactly when its rule uses dice: initial dice, recharge dice or an
  exhausted roll.

Source states them as `charges C [of M] [since A] [seed S1 S2];`.

**Which items can carry charges:**
- Only an item `from` a definition with charges carries them, and such an item
  always does.

**Where the state comes from:**
- **A declared item** may state `charges C ...;`. Without it, the item starts
  with the definition's initial count, unless the rule uses dice: then the item
  must declare its count and seed, or be created.
- **`create item ... from`** derives what the request leaves out:
  - the seed, drawn from the world's random source (`dice`, an adventure or a
    battle). Without one, creating a rolling item is refused with
    `E_CHARGES` field `charges.seed`;
  - `as_of`, set to the current tick when the world has a clock. Without one the
    item starts unanchored;
  - the count: the definition's initial count, rolled when the rule says dice.
    A stated `charges N;` is kept, for an item found partly used.

  Like the other derived fields, the receipt's `item_intent` keeps what was
  submitted.

**What a charged item can't do:**
- It is one physical unit: its quantity is at most 1.
- It can't be a container, be split, have stock added, or be consumed as
  recoverable stock.
- Ordinary consumption of the unit is allowed. The identity and the count stay,
  nothing more can be spent, and nothing can bring it back.

## How recharge is read

At tick T the engine counts the boundaries in `(as_of, T]` and applies them in
order. It caps at `max` after each and stops once the item is full. A dice
boundary's roll depends only on the item's seed and the boundary's tick, so the
same item at the same dawn always gets the same roll, whenever it is read.

Time never refuses a world. The view and the receipt say which case applied:

| `clock` | When | Count | `as_of` after a spend |
| --- | --- | --- | --- |
| `absent` | The world has no clock | `current`, no recharge | Spending is refused (`E_CHARGES`, field `clock`) |
| `behind` | The clock is before `as_of` | `current`, no recharge | Unchanged: it never moves backwards |
| `unanchored` | The item has no `as_of` | `current`, no back credit | The current tick |
| `current` | Otherwise | Recharged as above | The current tick |

Items without a recharge rule have no `clock` and spend in any world.

## Spending

`expend N charges of ITEM by OWNER;` checks, in this order:
1. The item must be charged (`E_CHARGES`). A recharging item needs a clock
   (`E_CHARGES`, field `clock`).
2. Known custody must be the stated owner (`E_CUSTODY`).
3. The owner must be the current owner (`E_OWNER`).
4. The physical unit must remain (`E_QUANTITY`).
5. The amount must be positive (`E_AMOUNT`).
6. There must be enough charges, after recharge (`E_CHARGES`).

A worn item can be used. Unknown custody keeps the existing compatibility
behavior of consumption. Transfer, placement and equipping keep the state.

The `ChargesExpended` event has `before` (the count available before the
spend), `after` (the stored count), and, for recharging items, `clock` and
`as_of`. When the spend empties an item with an `exhausted` rule, it also has
`exhausted`:
- `outcome`: `destroyed`, `regained` or `none`;
- `face`: the die, absent for `exhausted destroy`;
- `regained`: the rolled amount; `after` is that amount capped at `max`;
- `seed`: present after a regain. The regain replaces the item's seed, so an
  item that empties again at the same tick rolls a fresh face.

A destroyed unit is unworn first (an `ItemUnequipped` event with reason
`charges exhausted`, without equipment time), then its quantity becomes 0. Its
identity and count stay.

**The `item_charges` view** reads exact item IDs in request order, at one
revision.
- `current` and `as_of` are the normalized pair: the count and anchor as of the
  current tick.
- `available` is what can be spent now. It is 0 for a consumed item.
- `seed`, `clock` and `next_recharge` are present when they apply.
  `next_recharge` is the first boundary after `as_of`, shown while the item is
  below `max`.
- An unknown or uncharged ID returns the earlier results and an `E_CHARGES`
  fault with field `id`. That fault behaves like the `item_definitions` view
  failure.

## Host write-back

For one-shot sheet hosts, keep charged items on their sheets:
1. Persist the item's charge state from the accepted result:
   - from the `item_charges` view, `current`, `max`, `as_of` and `seed`. This
     is the normalized state and always safe to store;
   - otherwise from the receipt: `after`, `as_of`, and `exhausted.seed` when
     present.
2. Declare them back as `charges C of M since A seed S1 S2;` on the next call.

A call that spends a recharging item, or reads its charges, declares the clock.
Other calls may omit it; the items are then inert.

Item-only requests gain no `live_state` retry receipt. Follow
[LIVE_STATE.md](LIVE_STATE.md), "The host's write order", so that a retried
spend is never applied twice.

## Errors and persistence

Instance faults use `E_CHARGES`:

| Field | When |
| --- | --- |
| `charges` | No rule on the definition, an uncatalogued item, or not enough charges |
| `charges.current` | Out of range |
| `charges.max` | Differs from the definition |
| `charges.as_of` | An anchor on an item without a recharge rule |
| `charges.seed` | A missing seed on a rolling item, a seed without dice, or no random source at creation |
| `clock` | Spending a recharging item without a world clock |
| `quantity` | More than one unit |
| `container` | The item is a container |
| `recoverable` | The item has a recoverable count |
| `id` | A failed view lookup |

Definition faults stay `E_CATALOG`, with a field such as `charges.max`,
`charges.initial`, `charges.recharge.every` or `charges.exhausted.roll`.

Rolls use Go's ChaCha8 generator, keyed by the seed, a purpose code and the
tick, and draw each die with `Uint64N(sides)+1`. The version is pinned as
`go1.27-chacha8-uint64n-v1`.

**Save profiles:**
- Worlds whose definitions have only counts save under
  `nql-native-item-charges-1`, which extends `nql-native-item-catalog-1`.
- Worlds with a recharge, initial-dice or exhausted rule save under
  `nql-native-item-recharge-1`, which also pins the roll version.
- Older tags refuse them. Worlds without charge rules keep their bytes, events
  and profiles.
