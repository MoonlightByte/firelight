# Reading runtime diagnostics

From the project root, build with the established toolchain and run the existing native world:

```sh
scripts/go.sh build -o bin/nql-m8 ./cmd/firelight
bin/nql-m8 -world examples/m7/world.nql -actor char:tess -repl \
  --runtime-errors=structured < examples/m8/continuation.repl 2> .cache/m8-errors.json
python3 examples/m8/read_diagnostics.py < .cache/m8-errors.json
```

The stream first requests spend99 (a runtime rejection), then malformed source (compiler diagnostics), then valid SteadyAid (one successful commit on stdout). Inspection is a separate stdout record. The consumer reads consecutive whitespace-separated, potentially multiline JSON objects from either captured channel; it does not infer ordering across separately captured channels. Existing legacy errors work too, with absent structured detail reported honestly.

Version1 runtime records expose `fault.code/subject/field/expected/actual/message` and optional `ability.definition/step/operand/parameter`. Step0 is a real first body step and step-1 is binding context. Null objects and empty strings remain distinct. Expected/Actual are displayed as text; there are no typed numeric balance fields.

`attempt_committed:false` applies only to this failed execution attempt. `detail_scope:"evaluation_not_snapshot"` means a failed later step may report a private balance that was rolled back. Two spend2 steps at committed Focus3 report current1, while a separate inspection still shows Focus3. No request/history is created by the reader, and it never repairs, refills, retries or authorizes an action.

`two-spends.nql` is an additive Ability declaration for appending to M7's world source during a test; it is not an action file or a standalone world. Its body contains only existing spend operations.

The example reader uses only Python's standard library, reads a finite local stdin stream into memory, and prints deterministic summaries. Unsupported versions, malformed types/fields and duplicate JSON keys produce a `consumer_protocol_problem` and exit2; it never falls back to interpreting the error sentence. Compiler and success recognition are limited to their documented outer shapes, not independent validation of nested world/event truth. No live-model understanding or public-service security is claimed.

Decoder value/depth-limit failures also return exit2. Output uses safe JSON escaping, retaining decoded string data. Summaries already printed before a later malformed record remain historical reader output; they do not authorize an action or establish whole-stream validity. Input memory and output-delivery recovery are outside this finite local example's contract.

Synthetic decoder controls are separate from actual binary evidence:

```sh
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s examples/m8 -p 'test_*.py' -v
```

