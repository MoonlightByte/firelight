# The Firelight apply protocol

`firelight-apply` is a stateless checkpoint transaction tool. Each invocation reads **one JSON document on stdin** and writes **one JSON document on stdout**. The process opens no files and keeps nothing between invocations. The host owns durability, locking and choosing which checkpoint generation to send.

One call can, in order:

1. build a world, from NQL source (genesis) or from a checkpoint;
2. optionally execute one transaction;
3. optionally answer read-only views;
4. return the checkpoint the host must store.

## Exit codes

| Code | Meaning |
|---|---|
| 0 | A complete response was written. `ok` carries the outcome, success or refusal. |
| 2 | The input could not be read, exceeds the size limit, is not valid JSON, or has an unknown field; or the response could not be encoded. The body (when one is written) is `{"ok": false, "phase": "input", "error": ...}`. No world was touched. |
| 3 | The engine panicked. The response has `"phase": "internal"` and no checkpoint, so the host stores nothing. |

stdin is limited to 256 MiB. Unknown input fields are refused with exit code 2, so a misspelled field never passes silently. A request that decodes but is inconsistent (for example both `world` and `checkpoint`) is a normal response: exit code 0, `"ok": false`, `"phase": "input"`.

## Request

| Field | Type | Meaning |
|---|---|---|
| `world` | string | NQL world source, for genesis. Use either this or `checkpoint`, not both. |
| `world_name` | string | The source name used in diagnostics. Default `world.nql`. |
| `checkpoint` | object | A complete checkpoint returned by an earlier call (`Genesis`, `Head`, `History`, `State`). Its history is replayed and must agree with `Head` before any request is accepted. |
| `actions` | string | NQL action source for one transaction. Without it the call only builds the world and answers views. |
| `actions_name` | string | The source name used in diagnostics. Default `actions.nql`. |
| `actor` | `{"kind", "id"}` | **Required with `actions`.** It records attribution, not authorization. `kind` defaults to `character`. |
| `request` | string | **Required with `actions`.** The idempotency key (see Versioning). |
| `expected_revision` | integer | Optional optimistic-concurrency check. It defaults to the world's current revision, or to the original request's expected revision when the id was already committed. |
| `live_state` | object | For hosts that send a fresh world source each call: the document the last response returned, holding what only the engine writes. It requires `world`, and is not allowed with `checkpoint` or `new_live_state`. See docs/LIVE_STATE.md. |
| `new_live_state` | boolean | Start a live-state document from the source's seeds. |
| `include_state` | boolean | Include the full `State` snapshot in the returned checkpoint. It is omitted by default to keep responses small. |
| `status`, `equipment`, `attack_options`, `map` | character ids | Read-only views per character. |
| `explain` | `[{"character", "stat"}]` | Stat explanations: base, contributions, effective value. |
| `items_at` | `[{"kind", "id"}]` | Custody: the items at a place or with a holder. |
| `locations` | location ids | Location views. |
| `quests` | quest ids | Quest views. |
| `journal` | filter object | Journal view. |
| `item_definitions` | item definition ids | Catalog definitions. |
| `item_charges` | item ids | Charge state of charged items. |

## Response

| Field | Present when | Meaning |
|---|---|---|
| `ok` | always | `true` when the world was built and any transaction committed or was answered as a retry. |
| `phase` | always | Where the call stopped (see Phases). |
| `revision` | always | The world's revision after the call. On a refusal, the revision it was refused at. |
| `checkpoint` | ok | **The document to store.** Send it back as `checkpoint` on the next call. |
| `live_state` | live-state calls | The next live-state document to store. |
| `receipt` | a transaction ran | `{revision, request, events}`: the request as executed (operations already typed) and every event it published, in order. |
| `historical` | a retry | `true` when the request id was already committed and the original receipt is returned unchanged. |
| `ended_conditions`, `checks`, `attacks`, `saves` | a transaction ran | Summaries drawn from the receipt's events, so a host need not walk them. |
| `status`, `equipment`, ... | requested | One entry per requested view, named as in the request. |
| `diagnostics` | `phase: compile` | Typed source diagnostics (see Errors). |
| `error`, `fault` | a refusal | A message and, when the core refused, a typed fault. |
| `view_error` | a view failed after a commit | `{view, error, fault}`. The transaction stands, the response stays `ok` with its receipt and checkpoint, and later views are omitted. |

Each event in `receipt.events` has an `id` (`event:<revision>:<n>`), the `revision`, the `request_id`, the `actor`, the `operation` index it came from, a `kind` (for example `ItemEquipped`, `DamageApplied`, `HealingApplied`, `ConditionRemoved`), and a kind-specific body with typed before and after values.

## Phases

| Phase | Stopped because |
|---|---|
| `ready` | The world was built and views answered. No `actions` were sent. |
| `executed` | The transaction committed, or it was answered as a retry (`historical`). |
| `input` | The request document is inconsistent (exit 0): both `world` and `checkpoint`, neither, or a missing `actor` or `request` with `actions`. Also the phase of an undecodable request (exit 2): invalid JSON, an unknown field, or oversize input. |
| `compile` | The world or action source did not compile, or exceeds its size limit. See `diagnostics`. |
| `genesis` | The compiled world violates an invariant. |
| `replay` | The checkpoint's history does not replay to its head. |
| `live_state` | The live-state document is invalid or does not fit the source. |
| `execute` | The core refused the transaction (for example a revision mismatch or a reused request id with changed content). See `fault`. Nothing changed. |
| a view name | Without a transaction, that view failed, for example `"phase": "status"`. |
| `internal` | The engine panicked (exit code 3). |

A refusal in any phase publishes nothing. A request commits all of its events or none.

## Versioning

- **Revision.** Every committed request advances the world by one revision. All of a request's events share it.
- **expected_revision.** Send the revision you last stored. If the world has moved on, the request is refused in `execute` and nothing changes.
- **Idempotent request ids.** Re-sending a committed request with the same id and identical content returns its original receipt with `"historical": true`, even against a later checkpoint. The same id with different content is refused (`E_REQUEST_REUSE`). This makes a host's retry after a crash safe.
- **Protocol changes.** Fields are added, not repurposed. Unknown input fields are refused, so a host that sends a newer field to an older engine learns this at once (exit code 2, `phase: input`).

## Errors

A **compile diagnostic** is typed and positioned. The host shows it to whoever proposed the source and lets them correct it:

```json
{"source": "actions.nql", "line": 1, "column": 8, "code": "E_REFERENCE",
 "expected": "character", "actual": "\"char:nobody\" (missing)",
 "message": "entity does not exist; entity \"char:nobody\"; field \"reference\""}
```

A **fault** comes from the core's refusal of a built world or a transaction:

```json
{"code": "E_REQUEST_REUSE", "subject": "req-2", "field": "request",
 "expected": "identical retry", "actual": "different content",
 "message": "request ID already committed with different content"}
```

`code` is stable and meant for programs. `message` is for people. `subject`, `field`, `expected` and `actual` are present when they apply.

An **input error** has `"phase": "input"`, an `error` string and no `fault`. For example: `actor is required for execution (attribution, not authorization)` (exit 0), or `json: unknown field "colour"` (exit 2).

---

Firelight Engine, by Eternal Tavern.
