# Quests

A quest is a goal the party can take up: a step of the main plot or a side
quest. The engine keeps each quest's status, what it requires, and a log of
what happened to it. An agent changes a quest with typed actions, one step
at a time; it never rewrites a quest's record.

"Agent" means any caller that runs the game: a DM model, a script, a menu.
The agent decides what happens: the party took up the mill job, the beacon
is lit. The engine applies the change atomically, refuses one the record
does not allow and says why, and keeps the history.

The files:

- `examples/quests/world.nql`: the valley of the map guide with three
  quests. Clearing the mill comes before warning the watchtower; fetching
  herbs for Mira is a side quest under the mill.
- `examples/quests/start.nql`: the party takes up both main quests.
- `examples/quests/ahead.nql`: an hour later, the party warns the
  watchtower before the mill is cleared.
- `tests/quests_test.go`: every rule below, at its bounds, and the rules
  checked against a separate model on thousands of random requests.

## Facts and totals

| The agent or host controls | The engine derives |
| --- | --- |
| which quests exist, and which must be completed before each | that requirements are declared and form no cycle |
| that a quest starts, progresses, is completed or failed, or reopens | that the move is one the status allows |
| that a quest was completed before its requirements, and why | which requirements were open at that moment |
| what happened, in its own words | the log, its order, and the clock tick of each entry |
| | which requirements are still open, and which quests are ready to finish |

## Declaring quests

From `world.nql`:

```nql
quest "quest:valley/mill" named "Clear the Old Mill";
quest "quest:valley/tower" named "Warn the Watchtower" { requires "quest:valley/mill"; }
quest "quest:valley/herbs" named "Herbs for Mira" {
  side of "quest:valley/mill";
  started impact "Mira needs feverfew from the riverbank for her fever" at 27000;
}
```

- `requires "<quest>";` names a quest that must be completed before this
  one completes. One line per requirement, at most 16.
- `side of "<quest>";` groups a side quest under another. The engine never
  reads it for rules.
- The rest is the log, which states a quest in play back to the engine: one
  line per entry, in order, each as an action below wrote it. The status is
  the one the log ends in; a quest with no log is unstarted. `at <tick>` is
  the clock tick of the entry.

Quest IDs share the namespace of characters, items, places and occupants.
Qualifying them with the module, as above, keeps two modules' quests apart.
A world with quests needs no clock; without one, log entries have no tick.

## Changing quests

```nql
start quest "quest:valley/mill" impact "Ranger Mira asked the party to drive the goblins from the mill";
start quest "quest:valley/tower" impact "the watch must hear of the raids before nightfall";
```

The receipt holds two `QuestStarted` events, each with the `intent` and the
record `before` and `after`.

| Action | Moves |
| --- | --- |
| `start quest "<id>" [impact "<text>"];` | unstarted to started |
| `note quest "<id>" impact "<text>";` | no move: progress on a started quest |
| `complete quest "<id>" [ahead because "<reason>"] [impact "<text>"];` | unstarted or started, to completed |
| `fail quest "<id>" [impact "<text>"];` | unstarted or started, to failed |
| `reopen quest "<id>" because "<reason>";` | completed or failed, back to started |

Completed and failed are closed. Only `reopen` changes them, and it says
why: a quest marked done by mistake, or a failure the party later undoes.
It goes back to started, never to unstarted. A quest can be completed or
failed without being started first.

Every action in a request commits together or not at all, in order, so one
request can complete a quest and then the quest that requires it.

## Requirements and finishing ahead

Starting and failing are never gated: a party may take up a later quest
first. Completing is gated. A quest whose requirements are not all
completed is refused with `E_QUEST` `requires`, the open ones in `actual`.
The agent then completes them first, in the same request if that is what
happened, or says the quest was finished ahead of them:

```nql
advance time by 3600;
complete quest "quest:valley/tower" ahead because "the party slipped past the mill by night" impact "the beacon is lit";
```

The completion records the requirements open at that moment and the
reason. `ahead` with nothing open is refused, so a log never claims a skip
that did not happen. A requirement that failed, or was reopened, is open.

## The log

Each action adds one entry: its kind (`started`, `note`, `completed`,
`failed` or `reopened`), the text, for an `ahead` completion the open
requirements and the reason, and the clock tick when the world has a clock.
An entry is never rewritten. A text is the agent's own words: the engine
keeps it, shows it in the receipt and the view, and never reads it. It is
at most 512 bytes and not blank.

A quest keeps at most 16 entries. When the log is full, a new entry drops
the oldest note, and the event's `dropped` holds it. Status changes are
never dropped: when a full log holds no note, the change is refused
(`E_QUEST` `log`).

## Refusals

Each is `E_QUEST` with the reason in `actual`, `E_REFERENCE` for an unknown
quest, or `E_NO_CHANGE`:

| Field | Expected | Actual | Meaning |
| --- | --- | --- | --- |
| `status` | the statuses the action takes | the quest's status | for example, starting a completed quest |
| `requires` | `completed requirements` | the open requirements, comma separated | completing with open requirements and no `ahead` |
| `ahead` | `an open requirement` | `none` | `ahead` when every requirement is completed |
| `log` | `at most 16 status changes` | `full` | a full log with no note to drop |
| `text` | `valid text of at most 512 bytes` | `invalid`, or its length | a blank impact, or one over 512 bytes |
| `text` | `text for a note` | `absent` | a note without text |
| `reason` | `valid text of at most 512 bytes` | `invalid`, or its length | a blank reason, or one over 512 bytes |
| `reason` | `a reason for reopen` | `absent` | a reopen without a reason |
| `reference` | `quest` | `missing` | no quest has that ID (`E_REFERENCE`) |

`E_NO_CHANGE` answers starting a started quest, completing a completed one
and failing a failed one. An unknown quest, a blank text and a missing
reason are refused when the request is compiled, at the token.

## Reading quests

nql-apply takes `"quests": ["quest:valley/tower", "quest:valley/mill"]` and
returns, for each quest, its record and:

- `open`: its requirements that are not completed;
- `ready`: true when it is neither completed nor failed and every
  requirement is completed. These are the quests the party can finish next.

After `start.nql` and `ahead.nql`:

```json
[
  {
    "revision": 2, "id": "quest:valley/tower", "name": "Warn the Watchtower",
    "requires": ["quest:valley/mill"], "status": "completed",
    "log": [
      {"kind": "started", "text": "the watch must hear of the raids before nightfall", "tick": 28800},
      {"kind": "completed", "text": "the beacon is lit", "ahead": ["quest:valley/mill"],
       "reason": "the party slipped past the mill by night", "tick": 32400}
    ],
    "open": ["quest:valley/mill"], "ready": false
  },
  {
    "revision": 2, "id": "quest:valley/mill", "name": "Clear the Old Mill", "status": "started",
    "log": [{"kind": "started", "text": "Ranger Mira asked the party to drive the goblins from the mill", "tick": 28800}],
    "open": [], "ready": true
  }
]
```

The watchtower is done, and its log says it was done ahead of the mill and
why. The mill is ready to finish. One call can ask for any number of
quests; they are read together, at one revision. An unknown quest fails the
call when it carries no request. After a request commits, the response
stays ok with its receipt, `view_error` names the view, and the quests
before the unknown one still come back.

## Hosts that rebuild the world per call

A host that keeps the returned checkpoint keeps everything. A host that
writes a fresh world for each call passes `live_state` (docs/LIVE_STATE.md),
which holds each quest's status and log from version 3. It declares each
quest with its requirements and writes nothing back: the document's log
wins over the source's. A held quest the host does not declare in a call
stays in the document.

A host declares a quest's log in two cases: once, for a quest in play that
no document holds yet, as when it converts a campaign; and on every call
when it passes no `live_state`, writing each changed quest back from the
receipt. The last event for a quest has its `after` record, which maps one
to one onto a declaration:

| Record | Declaration |
| --- | --- |
| `requires`, `side_of` | one `requires` line each, `side of` |
| a `started` or `failed` entry | `started;` or `failed;`, with `impact "<text>"` when it has text |
| a `note` entry | `note impact "<text>";` |
| a `completed` entry | `completed;`, with `ahead "<id>" ... because "<reason>"` when it has `ahead`, then `impact "<text>"` when it has text |
| a `reopened` entry | `reopened because "<reason>";` |
| an entry's `tick` | `at <tick>` before the `;` |

The status follows from the log and is not written.

## Hosts with their own plot files

| Host record | Engine |
| --- | --- |
| plot points and side quests, each with an ID | one quest each, `quest:<module>/<id>` |
| a point's next points, or a list of prerequisites | `requires` on the later quest |
| a side quest under a plot point | `side of` |
| a status string set by writing it | `start`, `complete`, `fail` or `reopen` |
| an impact or progress text that replaces the last one | an entry added to the log, the earlier ones kept |
| a separate list of active quests | derived: the started quests |
| a module counted done when its last point is | the agent's call, from the view |

## Limits

- At most 4,096 quests per world, 16 requirements and 16 log entries per
  quest. Texts and reasons are at most 512 bytes.
- Quest actions are not Ability steps.
- The engine keeps no descriptions, places or rewards for quests; the host
  keeps its authored content by quest ID.
