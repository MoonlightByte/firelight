# World size

How large a world can grow, which limit stops it first, and what it costs per
call and per save. A host uses these numbers to budget storage and call time.

The measured item is one stack of stored loot, as a game host declares the
contents of a chest:

```nql
item "item:gemstone:storage-chest-1:5" named "Gemstone" { owner "char:thorin"; custody item "container:storage-chest-1"; definition "gear:worn"; quantity 3; }
```

How the numbers were measured is at the end.

## Limits

| Limit | Value | Refused with |
| --- | --- | --- |
| World source | 16 MiB (`compiler.MaxWorldSourceBytes`) | `E_LIMIT` `source <= 16 MiB`; nql-apply phase `compile` |
| Action source | 1 MiB (`compiler.MaxSourceBytes`) | `E_LIMIT` `source <= 1 MiB`; nql-apply phase `compile` |
| Genesis record | 16 MiB minus 1 KiB of compact JSON (`core.MaxGenesisRecordBytes`) | `E_ADVENTURE` `genesis bytes` |
| One committed request | 1 MiB minus 64 bytes of compact JSON | `E_ADVENTURE` `batch bytes` |
| Archive: genesis and history, one save | 128 MiB | `E_LIMIT` `history bytes` |
| History | 8192 committed requests | `E_LIMIT` `history` |
| Characters, locations, Condition definitions | 4096 each | `E_EQUIPMENT`, `E_CLOCK` or `E_CATALOG` `capacity` |
| Items, Conditions | 32768 each | `E_EQUIPMENT`, `E_CLOCK` or `E_CATALOG` `capacity` |
| Item definitions | 4096 | `E_CATALOG` `definitions` |
| Timed Conditions | 64 active lifetimes | `E_CLOCK` `timed capacity` |
| Strings | 4096 bytes, 256 for an ID | `E_ADVENTURE` `encoding` |
| nql-apply input document | 256 MiB | exit 2, phase `input` |

The genesis, request, archive and history limits apply to a world with an
`adventure`, `equipment` or `clock` block, or item definitions; the entity
limits to a world with `equipment`, `clock` or item definitions. An adventure
world keeps its own smaller limits: 64
characters, 64 items and 32 locations. The entity limits hold during play too:
creating an item or applying a Condition past a limit is refused, and the
world does not change.

[Item definitions](ITEM_CATALOG.md) share the existing total item allowance;
they do not add inventory capacity. Catalog births keep original intent plus
resolved data in their batch, so the batch bound applies to that complete
encoding. The measurements below are for the stated pre-catalog fixture,
not a catalog-content or model-token benchmark.

## What an item costs

Bytes per stored item, the slope between 0 and 10,000 stored items:

| Form | Bytes per item |
| --- | ---: |
| World source | 161 |
| Genesis, compact JSON | 216 |
| Checkpoint, compact JSON | 216 |
| Native archive | 216 |
| nql-apply response | 317 |

The checkpoint and the archive hold the genesis once, so an item costs the
same in all three. nql-apply indents its response, which makes it about 1.5
times the compact checkpoint. A checkpoint stored with Python's default
`json.dumps` separators costs 235 bytes per item.

Each committed request adds its record to the checkpoint and the archive. A
request with one operation adds about 0.5 KB; the measured `consume` added 463
bytes.

A character costs far more than an item. Each added level-5 character sheet,
with its armor, weapon, ring, 12 gear items and Conditions, added about 5.7 KB
of genesis.

## The real ceiling

Two limits bound the items in a world: 32,768 items, and the 16 MiB genesis.
The genesis fills first when items average more than 512 bytes of genesis JSON
(16 MiB / 32,768).

- At 216 bytes per stored item, the item limit binds. 32,768 items (the four
  characters' 62, the chest, and 32,705 stored in it) make a 7,136,145-byte
  genesis, 43% of the bound, from 5,343,350 bytes of world source.
- Items with 4,000-byte descriptions fill the genesis at about 4,000 items.

The 128 MiB archive holds the genesis, 64 KiB of reserve, and each committed
request with 64 bytes of framing. What is left for history:

| Genesis | Left for history |
| --- | ---: |
| The 32,768-item world above: 6.8 MiB | 121 MiB |
| The largest genesis: 16 MiB minus 1 KiB | 111.9 MiB |

At about 0.5 KB per one-operation request, 8192 requests take about 4 MiB, so
the request count fills first. The bytes fill first only when requests average
more than about 14 KiB. A world at either limit refuses new requests with
`E_LIMIT`; it can still be read, saved and replayed. There is no compaction.

A host that writes a fresh world for every call starts a new history each time
and never reaches the archive or history limits. Its limits are the 16 MiB
genesis and world source, the entity limits and the time per call.

## Cost of a call

A world call sends the world source and one action. A checkpoint call sends
the checkpoint a world call returned and one action. Neither asks for views.

| Stored items | Items | World source | Checkpoint | Response |
| ---: | ---: | ---: | ---: | ---: |
| 0 | 63 | 39,903 | 45,462 | 74,538 |
| 1,000 | 1,063 | 200,285 | 260,510 | 390,248 |
| 5,000 | 5,063 | 846,289 | 1,125,181 | 1,657,588 |
| 10,000 | 10,063 | 1,653,785 | 2,206,010 | 3,241,748 |
| 16,384 | 16,447 | 2,691,185 | 3,592,402 | 5,270,796 |
| 32,705 | 32,768 | 5,343,350 | 7,136,782 | 10,458,159 |

Sizes are in bytes. Time and peak memory of the nql-apply process:

| Stored items | World call | Memory | Checkpoint call | Memory |
| ---: | ---: | ---: | ---: | ---: |
| 0 | 9 ms | 10 MiB | 8 ms | 9 MiB |
| 1,000 | 22 ms | 12 MiB | 19 ms | 11 MiB |
| 5,000 | 77 ms | 33 MiB | 60 ms | 22 MiB |
| 10,000 | 140 ms | 48 MiB | 114 ms | 32 MiB |
| 16,384 | 229 ms | 84 MiB | 184 ms | 51 MiB |
| 32,705 | 453 ms | 157 MiB | 347 ms | 87 MiB |

Time grows linearly with items.

## Saving at 10,000 items

At 10,000 stored items (10,063 in the world):

- A host that keeps the checkpoint stores 2,206,010 bytes of compact JSON and
  sends it back with the next call. With Python's default separators the
  checkpoint call's request is 2,404,068 bytes. Writing the file is the host's
  own I/O.
- The native archive is 2,206,625 bytes. In process, writing it takes 116 ms,
  and reading it, which replays and checks the whole history, takes 108 ms.

## Characters and Conditions

Validation runs at genesis, on every request and on replay, and derives every
character's stats. One index of the world serves each validation: it gathers
the effects once and finds each character's effects, Conditions, worn items
and modifiers by lookup. So a call grows linearly in characters and in
Conditions that carry modifiers. Measured on the world with 1,000 stored items:

| Added | Characters | World call | Memory | Checkpoint call | Memory |
| --- | ---: | ---: | ---: | ---: | ---: |
| nothing | 4 | 25 ms | 13 MiB | 20 ms | 12 MiB |
| 16 characters | 20 | 36 ms | 14 MiB | 31 ms | 13 MiB |
| 32 characters | 36 | 50 ms | 19 MiB | 42 ms | 14 MiB |
| 64 characters | 68 | 76 ms | 21 MiB | 63 ms | 18 MiB |
| 128 characters | 132 | 125 ms | 30 MiB | 107 ms | 24 MiB |
| 256 characters | 260 | 219 ms | 52 MiB | 186 ms | 31 MiB |
| 512 characters | 516 | 427 ms | 93 MiB | 346 ms | 47 MiB |
| 1,024 characters | 1,028 | 879 ms | 193 MiB | 682 ms | 103 MiB |
| 2,040 characters | 2,044 | 1.88 s | 359 MiB | 1.36 s | 220 MiB |
| 100 modifier effects | 4 | 27 ms | 14 MiB | 21 ms | 12 MiB |
| 300 modifier effects | 4 | 32 ms | 15 MiB | 25 ms | 12 MiB |
| 1,000 modifier effects | 4 | 48 ms | 17 MiB | 40 ms | 16 MiB |
| 3,000 modifier effects | 4 | 99 ms | 29 MiB | 73 ms | 23 MiB |
| 4,032 modifier effects | 4 | 123 ms | 32 MiB | 90 ms | 33 MiB |

Each added character is a level-5 sheet with armor, a weapon, a ring and 12
gear items. Each effect is an untimed Condition with a +2 Armor Class
modifier, of a Condition type of its own, as the game host's world builder
declares them. Before the index, the 64-character world took 13.9 s per world call and
the 1,000-effect world 24.0 s.

Two other limits bind these worlds first:

- Items. Each sheet carries 15 items, so the 2,040-character world holds
  31,663 items, close to the 32,768-item limit.
- Condition definitions. A world that declares a Condition type per effect
  reaches the 4,096 Condition definitions first. The measured world declares
  64 types of its own, so it holds at most 4,032 such effects; one more is
  refused at compile with `E_CLOCK` `capacity`. Effects that share a type are
  bounded only by the 32,768 Conditions.

The compiler still checks each declared Condition, effect and relationship by
scanning the world, which costs the product of declarations and entities:
about 0.2 s of the 2,040-character world call above, and about 0.3 s of a
2.4 s world call with 2,044 characters, 31,663 items, 32,677 Conditions and
4,096 relationships.

## Saves from older binaries

Nothing in the save format changed. A save or checkpoint whose genesis is
under 1 MiB reads the same in older and newer binaries. A binary from before
the 16 MiB genesis refuses a larger one without misreading it: nql-apply in
phase `replay` with `E_ADVENTURE` `genesis bytes`, and nativesave before it
decodes the record ("native archive byte or record limit exceeded" for an
archive file).

## How the numbers were measured

- Machine: Intel Core i9-13900H, 32 GB of memory, Linux 6.6 under WSL2.
- nql-apply built as a host builds it: `CGO_ENABLED=0 go build -trimpath
  -ldflags "-s -w"`.
- The world: four level-5 characters with SRD proficiencies, features, armor,
  a weapon, a ring and 12 gear items each, two of them with shields, and a
  chest at their location holding the stored items, all owned by one
  character. A game host's world builder wrote it. With nothing stored it has
  63 items and 37 Conditions.
- Each call runs one `consume` of a carried potion.
- A Python harness calls nql-apply the way a host does: `json.dumps(...,
  ensure_ascii=True)` in, one process, `json.loads` out. Times are the median
  of three calls, including encoding, process start and decoding. Memory is
  the largest resident set of the three.
- Archive writes and reads are timed in process, median of three.

The limits are tested at their exact bounds in `core/world_size_test.go`,
`nativesave/world_size_test.go` and `compiler/world_size_test.go`.
`TestWorldSizeHostFlow` in `cmd/firelight-apply/main_test.go` runs a 10,000-item
world through a world call and a checkpoint call.
