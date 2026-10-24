# Item definitions and reference-based creation

Item definitions supply shared display data, classification and optional prices.
Items still use the existing stock, custody, transfer and equipment rules.
There is no separate inventory, shopping action or price quote. Charges are in
[ITEM_CHARGES.md](ITEM_CHARGES.md).

## Declare definitions, state intent

This synthetic example is [world.nql](../examples/item-catalog/world.nql), not
a published game price table:

```nql
// Synthetic example data, not a published game price table.
rules { transfer unequips; wear owner_only; }
location "loc:camp" named "Camp";
character "char:ada" named "Ada" at "loc:camp" {
 resource "copper" = 10 min 0 max 100;
}
item type "itemdef:torch" named "Reed torch" {
 description "A bundle of dry reeds bound with twine.";
 kind "gear"; subtype "light";
 magical false; consumable true;
 price "copper" 1 per 1;
}
```

The host checks price/lot arithmetic, merchant terms and authorization, then
submits [buy.nql](../examples/item-catalog/buy.nql) as one request:

```nql
spend "char:ada" resource "copper" by 2;
create item "item:torch-7" from "itemdef:torch" {
 owner "char:ada"; custody character "char:ada"; quantity 2;
};
```

Insufficient funds or a later failing action rolls back the whole request.
No automatic purchase, currency conversion, stock merge or item effect occurs.
If the merchant already holds stock, use Split/Transfer/Place instead of
creating a second copy of the goods. Price does not authorize a purchase.

For `nql-apply`, pass the first file as `world`, the second as `actions`,
`actor:{"kind":"character","id":"char:ada"}`, `request:"buy:1"`, and
`item_definitions:["itemdef:torch"]`. The `item_definitions` response is:

```json
[
  {
    "revision": 1,
    "id": "itemdef:torch",
    "name": "Reed torch",
    "description": "A bundle of dry reeds bound with twine.",
    "kind": "gear",
    "subtype": "light",
    "magical": false,
    "consumable": true,
    "price": {"unit": "copper", "amount": 1, "units_per_lot": 1}
  }
]
```

Exact-ID lookups preserve request order (including duplicates), all at one
revision. The Go lookup returns earlier views and an `E_CATALOG` fault at an
unknown ID. A query-only command fails in the `item_definitions` phase. After
a commit or historical retry, the command keeps `ok:true`, the receipt and
checkpoint, any earlier views, and a `view_error` naming `item_definitions`.
A failed query never undoes an action that already committed.
The lookup fault's field is `id`; a missing reference on an Item uses `from`.
There is no paged catalog listing. The host already has its authored pack.

## Data contract

`Blueprint.ItemDefinitions` (`item_definitions`) contains `ItemDefinition`
records. IDs are immutable for one World's lifetime and have their own
namespace, independent of entities and equipment definitions. Prefixes are
conventions only. Duplicate display names are allowed; duplicate IDs refuse.

- `id`: valid nonblank ID, at most 256 UTF-8 bytes, no control characters.
- `name`: nonblank UTF-8 text, at most 4096 bytes.
- `description`: UTF-8 text, at most 4096 bytes; empty is allowed.
- `kind`: a valid classification token, at most 256 bytes. `subtype` is
  optional with the same bound. These are authored labels, not engine enums.
- `magical`, `consumable`: booleans (source defaults to false). Neither grants
  a mechanic. Classification exists only on the definition, not on each Item.
- `equipment`: optional existing equipment-definition ID. It reuses the sole
  equipment evaluator, including modes, slots, training, grants and time.
- `price`: optional `{unit,amount,units_per_lot}`. Unit is a valid token of at
  most 256 bytes; amount is a nonnegative int64; units_per_lot a positive
  int64. Missing means unpriced; zero amount means explicitly free. Never
  multiply or round prices inside the engine. Host arithmetic must be checked.

Source uses `item type ID named NAME { ... }`. Fields can appear in any order,
once each. Kind must be present; description defaults to empty. Optional
equipment and price fields, when present, must be valid, not empty placeholders.
Forward references to item/equipment definitions are supported.

`Item.From` (`from`) is optional. Existing explicit `named` items are unchanged.
Reference-based source uses `item ID from DEFINITION { instance fields }` for
genesis and `create item ID from DEFINITION { instance fields };` for actions.
A world declaration ends with `}`, not `};`. Neither source form accepts
`named`, `description` or `definition` overrides with `from`.

The typed API accepts empty Item name/description/equipment-definition fields
as "derive". Equal supplied fields are accepted for snapshot round-trips;
conflicting fields refuse with `E_CATALOG`. There are no classification flags
on Item. Equipment mode remains instance state: a worn genesis item can state
its mode while deriving its equipment definition. An ordinary definition
without equipment cannot acquire an equipment object or mode.

All existing instance rules apply: globally fresh creation ID, positive initial
stock, valid references and initially unworn. Equip afterward in the same
transaction. Genesis can retain depleted identities and worn equipment.
Split preserves `from` with its existing `split_from` lineage; transfer,
placement, stock consumption/addition and equipment keep the instance ID.

## Execution, persistence and retries

Compiled CreateItem retains the exact `from` reference and submitted instance
facts. The engine derives fields during construction/execution, not by
rewriting action intent. ItemCreated's `item` is the resolved birth record;
only catalog births also have `item_intent`, the original submitted Item.
Replay reconstructs from that intent and checks the complete derived trace.
Changing even an omitted field to an equal supplied field changes the request.

Snapshots, receipts, definitions and views are detached copies. Kept checkpoints
retain genesis definitions. An empty definition list canonicalizes to absent;
it changes neither legacy archive bytes nor the profile. Native saves with definitions use the distinct
`nql-native-item-catalog-1` profile (also with equipment, clock or battle);
older feature tags must not admit them. Feature-free output omits all new
optional fields and preserves existing tags and event shapes.

For one-shot sheet hosts, keep items on their existing sheets. Persist instance
ID, `from`, quantity, owner/custody/wearer and mode from accepted results.
Redeclare these facts with `from` on the next call. Retrieve definition data
for classification; do not ask a model to fill it in. No `live_state` inventory
facet is added. Follow [LIVE_STATE.md](LIVE_STATE.md), "The host's write order",
including its pending-response recovery. Item-only requests do not gain a
new live_state retry receipt; use that existing write-back protocol. A full
checkpoint retains ordinary exact historical receipts.

Host content may change between fresh worlds; a kept world does not refresh
its definitions. Do not silently map old/customized items by name. Keep them
explicit unless a reviewed mapping agrees with their facts. Invalid catalog
references must not fall back to invented item details.

## Limits and errors

At most 4096 item definitions; no extra allowance beyond 32768 items (including
depleted ones), 4096 characters/locations/condition definitions and 32768
conditions. Catalog worlds opt into the existing archive encoding bounds:
256-byte IDs, 4096-byte strings, a 16 MiB genesis record minus framing, 1 MiB
batch records, and a 128 MiB archive. Existing source/request/work/event limits
are unchanged; see [WORLD_SIZE.md](WORLD_SIZE.md).

Catalog validation reports `E_CATALOG` with subject, field, expected, actual
and `invalid item catalog FIELD`. For example, `from:"missing"` on item `new`
has field `from`, expected `declared item definition`, actual `missing`.
Unknown equipment, conflicting display/equipment data, invalid prices,
duplicate definitions and catalog bounds are refused, never repaired.
Other item/equipment/resource faults keep their existing codes and behavior.

Synthetic tests are not a reviewed game catalog. Real content and prices need
source, edition and redistribution review, plus measured per-call cost and a
host acquisition/retry gate before deployment.
