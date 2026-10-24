# Firelight

Firelight is the rules engine behind NeverEndingQuest: the model tells the story, Firelight keeps the table honest.

**Firelight Engine, by Eternal Tavern.**

Firelight is an authoritative rules runtime for tabletop-style games. A host, an AI game master for example, describes the world and each turn's actions in NQL, a small typed rules language. Firelight checks every identity and invariant, applies the rules, and returns structured events and a new checkpoint. The model may propose and narrate; Firelight decides what actually happened.

- **Deterministic and atomic.** A request commits all its events or none.
- **Stateless process.** The host owns persistence. Each call reads one JSON document on stdin and writes one on stdout. The engine opens no files and keeps nothing between calls.
- **Standard library only.** No external Go dependencies.

## Ten-minute path

### 1. Build

Requires Go 1.23 or newer.

```sh
git clone https://github.com/MoonlightByte/firelight
cd firelight
go test ./...
go build -o bin/firelight-apply ./cmd/firelight-apply
```

Or install it directly: `go install github.com/MoonlightByte/firelight/cmd/firelight-apply@latest`.

### 2. Write a tiny world

`tiny.nql`:

```nql
rules { transfer unequips; wear any; }
location "loc:hall" named "Lantern Hall";
character "char:ada" named "Ada" at "loc:hall" {
  stat "defense" = 10;
  resource "hp" = 20 min 0 max 30;
}
item "item:shield:001" named "Bronze Shield" { owner "char:ada"; }
effect "effect:shield" from item "item:shield:001" on worn "item:shield:001" stat "defense" add 2;
```

Every entity has a typed identity (`char:`, `item:`, `loc:`). Names are for display only, and two characters may share one.

### 3. Run one turn

A request carries the world, the turn's actions (also NQL), an actor for attribution, a request id, and any read-only views you want back:

```sh
bin/firelight-apply < request.json > response.json
```

`request.json`:

```json
{
  "world": "rules { transfer unequips; wear any; }\nlocation \"loc:hall\" named \"Lantern Hall\";\ncharacter \"char:ada\" named \"Ada\" at \"loc:hall\" {\n  stat \"defense\" = 10;\n  resource \"hp\" = 20 min 0 max 30;\n}\nitem \"item:shield:001\" named \"Bronze Shield\" { owner \"char:ada\"; }\neffect \"effect:shield\" from item \"item:shield:001\" on worn \"item:shield:001\" stat \"defense\" add 2;\n",
  "world_name": "tiny.nql",
  "actions": "equip \"item:shield:001\" on \"char:ada\";\ndamage \"char:ada\" resource \"hp\" by 7;",
  "actions_name": "turn.nql",
  "actor": {"kind": "character", "id": "char:ada"},
  "request": "req-1",
  "status": ["char:ada"]
}
```

`response.json` (checkpoint abbreviated):

```json
{
  "ok": true,
  "phase": "executed",
  "revision": 1,
  "checkpoint": {"Genesis": "...", "Head": "...", "History": "...", "State": "..."},
  "receipt": {
    "revision": 1,
    "request": {
      "id": "req-1",
      "expected_revision": 0,
      "actor": {"kind": "character", "id": "char:ada"},
      "operations": [
        {"equip": {"item": "item:shield:001", "wearer": "char:ada"}},
        {"damage": {"character": "char:ada", "resource": "hp", "amount": 7}}
      ]
    },
    "events": [
      {"id": "event:1:1", "revision": 1, "request_id": "req-1",
       "actor": {"kind": "character", "id": "char:ada"}, "operation": 0,
       "kind": "ItemEquipped",
       "wearing": {"item": "item:shield:001", "before": "", "after": "char:ada", "reason": "explicit"}},
      {"id": "event:1:2", "revision": 1, "request_id": "req-1",
       "actor": {"kind": "character", "id": "char:ada"}, "operation": 1,
       "kind": "DamageApplied",
       "resource": {"character": "char:ada", "resource": "hp", "requested": 7,
                    "before": {"current": 20, "minimum": 0, "maximum": 30},
                    "after": {"current": 13, "minimum": 0, "maximum": 30}}}
    ]
  },
  "status": [
    {"revision": 1,
     "character": {"id": "char:ada", "name": "Ada"},
     "location": {"id": "loc:hall", "name": "Lantern Hall"},
     "resources": {"hp": {"current": 13, "minimum": 0, "maximum": 30}},
     "stats": [{"stat": "defense", "base": 10,
                "contributions": [{"effect": "effect:shield",
                                   "source": {"kind": "item", "id": "item:shield:001"},
                                   "target": {"kind": "item", "id": "item:shield:001"},
                                   "amount": 2}],
                "effective": 12}],
     "active_effects": [{"id": "effect:shield",
                         "source": {"kind": "item", "id": "item:shield:001"},
                         "mode": "worn",
                         "target": {"kind": "item", "id": "item:shield:001"},
                         "stat": "defense", "amount": 2}]}
  ]
}
```

Equipping the shield activated its worn effect (defense 10 + 2 = 12), and the damage was applied inside the resource's bounds (hp 20 to 13). Both events share revision 1: one request, one atomic commit.

### 4. Take the next turn

Store `checkpoint` and send it back with the next request, together with the revision you expect:

```json
{
  "checkpoint": { "...": "the object from the last response" },
  "actions": "heal \"char:ada\" resource \"hp\" by 3;",
  "actor": {"kind": "character", "id": "char:ada"},
  "request": "req-2",
  "expected_revision": 1,
  "status": ["char:ada"]
}
```

This returns `"revision": 2`, a `HealingApplied` event, and hp `{"current": 16, "minimum": 0, "maximum": 30}`. Firelight replays the checkpoint's history and checks that it agrees with its head before accepting the request.

### 5. See a refusal

An action that names an entity which does not exist is refused at compile time, and nothing changes:

```json
{
  "ok": false,
  "phase": "compile",
  "diagnostics": [
    {"source": "actions.nql", "line": 1, "column": 8, "code": "E_REFERENCE",
     "expected": "character", "actual": "\"char:nobody\" (missing)",
     "message": "entity does not exist; entity \"char:nobody\"; field \"reference\""}
  ]
}
```

A host shows the model this typed diagnostic and lets it correct its proposal. The host never guesses what was meant.

Every execution needs `actor`. It records attribution, not authorization. Without it the call returns `"phase": "input"` and the error `actor is required for execution (attribution, not authorization)`.

## The apply protocol

The full contract is in [docs/PROTOCOL.md](docs/PROTOCOL.md). It covers every input field and output field, the phases (`ready`, `executed`, `input`, `compile`, `genesis`, `replay`, `live_state`, `execute`, a failed view's name, `internal`), revisions, idempotent request ids, live state, and the read-only views (`status`, `equipment`, `attack_options`, `explain`, `items_at`, `map`, `locations`, `quests`, `journal`, `item_definitions`, `item_charges`). [docs/LIVE_STATE.md](docs/LIVE_STATE.md) is the guide for hosts that rebuild the world source on each call.

## NQL, the rules language

NQL describes a world and the actions taken in it. A world declares rules policies, locations, characters with stats and bounded resources, items with an independent owner and wearer, effects with source and target provenance, and typed relationships. Actions are the verbs the engine understands:

```nql
transfer "item:shield:001" from "char:ada" to "char:cy";
equip "item:shield:001" on "char:ada";
damage "char:ada" resource "hp" by 7;
heal "char:ada" resource "hp" by 3;
move "char:ada" to "loc:garden";
relate character "char:ada" "trust" character "char:bea" = 5;
```

The grammar is in [docs/GRAMMAR.md](docs/GRAMMAR.md). Worked worlds and journeys are in [examples/](examples/).

## Reference integration

[NeverEndingQuest](https://github.com/MoonlightByte/NeverEndingQuest) is the reference host. It is an AI dungeon master in which the language model narrates and proposes, while Firelight owns dice outcomes, totals, inventories, conditions, quests and the clock. A minimal Python host, standard library only, is in [examples/python/](examples/python/firelight_host.py).

## License

Fair Source License 1.0. See [LICENSE](LICENSE). It becomes Apache License 2.0 five years after first publication.

---

Firelight Engine, by Eternal Tavern.
