# Ammunition

Arrows, bolts, sling bullets and needles are engine stock: items with a
quantity, like the rest of an inventory. An agent spends them with `consume`,
and the engine keeps count of the spent units that can be picked up again.
After the fight the agent says to recover, and the engine derives how many
come back. An agent never writes the new quantity.

"Agent" means any caller that updates a character: a DM model, a sheet editor,
a combat routine. The agent decides when a fight ends and what the characters
do; the engine applies each change atomically, refuses what the world cannot
hold, and does the arithmetic.

The rule follows SRD 5.2.1, the Ammunition weapon property: after a fight, a
character who spends a minute searching recovers half the ammunition used in
the fight, rounded down, and the rest is lost. The files:

- `examples/ammunition/world.nql`: the rule and two archers.
- `examples/ammunition/fight.nql`: the shots of one fight;
  `examples/ammunition/recover.nql`: the recovery after it.
- `tests/ammunition_test.go`: every number below, worked by hand.

## Facts and totals

| The agent controls | The engine derives |
| --- | --- |
| each stock and its starting `quantity` | the `quantity` after every shot and recovery |
| which shots can be recovered, by `consume ... recoverable` | the stock's `recoverable` count: the units spent since its last recovery |
| when a fight ends, by `recover` | the share that comes back: half the count, rounded down |

## Setting up a world

The world's rules block declares the share. SRD recovery is half:

```nql
rules { transfer unequips; wear any; recovery 1 of 2; }
```

`recovery <numerator> of <denominator>;` returns floor(count * numerator /
denominator) of each recovered count. The numerator is between 0 and the
denominator, and the denominator between 1 and 1,000. `recovery 0 of 1` loses
everything spent. A world with a Battle block refuses the rule, since Battle
records its own shots and recovers them with `battle recover`.

Each kind of ammunition a character carries is one stock:

```nql
item "item:arrows" named "Arrows" { owner "char:ranger"; custody character "char:ranger"; quantity 20; }
```

## During a fight

A shot that can be recovered is spent with `recoverable`. From `fight.nql`:

```nql
consume "item:arrows" from "char:ranger" by 6 recoverable;
consume "item:bolts" from "char:fighter" by 3 recoverable;
consume "item:bolts" from "char:fighter" by 1;
```

The ranger's arrows go from 20 to 14 with a count of 6. The fighter's bolts go
from 20 to 16 with a count of 3: the fourth bolt, which the agent narrates
breaking, is spent without `recoverable` and can never come back. Each
`consume` is refused when the stock is short (E_QUANTITY), so the agent cannot
fire arrows the character does not have.

## After a fight

One `recover` per stock closes its count. From `recover.nql`:

```nql
recover "item:arrows" from "char:ranger";
recover "item:bolts" from "char:fighter" by 0;
```

The ranger takes the minute and recovers half of 6: the arrows are 17. The
fighter moves on without searching, and `by 0` forfeits the share: the bolts
stay at 16. `by <n>` takes n of the share when the story finds fewer; more
than the share is refused (E_QUANTITY). Either way the count returns to 0, so
the next fight starts from nothing.

The receipt's `ItemRecovered` event holds `item_recovery`: the `spent` count
it closed, the share `allowed` from it, the `requested` amount when the agent
stated one, and the quantity `before` and `after`.

`recover` is refused when the count is 0 (E_QUANTITY), so a recovery the agent
forgot to mark on the shots fails loudly instead of returning nothing.

## Reading the stock

`items_at` for the character lists each stock with its `quantity` and, while a
count is pending, its `recoverable`. A stock spent to 0 keeps its identity at
quantity 0 and still recovers into it.

A split leaves the count with the original stock. A transfer carries it to the
new owner, who recovers it once the stock is in their custody. Recover before
handing a quiver over to keep the recovery with the archer.

## Hosts that rebuild the world per call

A host that keeps the returned checkpoint keeps every count. A host that
writes a fresh world from its own store for each call stores, for each stock:

- Quantity: the `quantity` from `items_at`.
- A pending count: the `recoverable` from `items_at`, declared back on the
  item:

  ```nql
  item "item:arrows" named "Arrows" { owner "char:ranger"; custody character "char:ranger"; quantity 14; recoverable 6; }
  ```

- A stock at quantity 0 with a pending count: keep it, or its recovery is
  lost.

## Hosts that keep an ammunition list

A host that keeps ammunition in a list of its own moves it into the engine's
stock:

| Host record | Engine |
| --- | --- |
| an ammunition row | one stock with a `quantity`, in the character's custody like the rest of the inventory |
| the row's count of spent shots that can be found again | the item's `recoverable` |
| a shot | `consume ... recoverable` instead of a new quantity |
| recovery after a fight | `recover`, or `recover ... by 0` when there is no time to search, instead of a new quantity |

The separate list then retires: the host shows the quantities from `items_at`
and stops writing them.

## Limits

- Outside Battle the engine does not know when a fight ends. The agent says so
  with `recover`, once per stock. Two fights without a recovery in between
  pool their counts.
- Each stock has one count. Arrows of two kinds are two stocks.
- An Ability's `consume` step spends without `recoverable`.
- A thrown weapon, such as a dagger, is not ammunition. It stays one item that
  the agent places or transfers.
- The share is one rule per world. Magic ammunition that is lost on a hit is
  spent without `recoverable`.
