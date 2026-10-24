# Semantic boundaries

NQL Core is an experiment in centralized RPG semantics. It is not an experiment in shortening JSON. Source has a deliberately closed grammar; the compiler resolves immutable entity kinds, stats and resources. Runtime checks operations against current state and authored policies, even when operations originate directly from Go.

Owner is an optional character reference on an item. Wearer is another optional character reference on that item. Neither implies the other. Inventory and equipment are selections by these two different fields. Neither establishes physical item location. Only characters have modeled locations in M1; transfer and equip have no proximity restriction.

An effect's source is attribution; its target determines attachment and activation. A character-sourced effect targeting a worn item follows that item's wearer, even though its source is not the item. Worn effects persist while inactive. Removing or explicitly expiring an effect deletes just that record; each contribution retains its ID and source in explanations and events.

Authored rules are two explicit choices, `transfer keeps_worn|unequips` and `wear any|owner_only`. This is a limited rule vocabulary, not an expression language. Ownership checks under `owner_only` are evaluated when the operation executes. Changing wearers requires explicit unequip followed by equip. Rules do not grant host authorization; an actor field is attribution only.

The runtime owns a private canonical snapshot protected by one mutex. Public records are detached data. Candidate operations and validation happen under the same lock, and candidate state, structured history, revision and request receipt publish once. No callback or I/O occurs in this section. A source compiler receives a snapshot; its knowledge never authorizes later publication. Expected revision is checked again at execution.

Integers are signed 64-bit. Resource operations clamp, and their events record the positive requested amount plus the actual before/after. Effective stat sums use exact intermediate arithmetic and must fit int64 at publication. A valid final sum is independent of effect enumeration order. Relationships carry a signed strength but no automatic social behavior.

The in-memory history is an audit/reconstruction mechanism, not a durable or authenticated event store. Replay constructs a new isolated world. It reconstructs semantic operations from typed event payloads, verifies the resulting complete event trace (including before facts, derived rule events and operation grouping), and retains original request receipts. It never changes an existing world or treats copied projections as state. Shared transition semantics keep live execution and replay consistent; independent numeric test oracles are still required to catch shared bugs.

Transfer projection completeness means enough information to assess a single ownership transfer under current rules, including numeric consequences of unequipping. It does not promise completeness for a subsequent equip or compound request. Totals can reflect contributors omitted from the narrow view. M1 demonstrates selective fields, not information-flow security or ACL enforcement.

The completeness validator checks a detached view against its included required-effect manifest. Both are caller-editable values: coordinated removal from the view and manifest is not detectable without a trusted producer or authentication mechanism. This is structural sufficiency checking, not authenticity verification. The authoritative runtime rechecks actual state regardless of a view's assessment.
