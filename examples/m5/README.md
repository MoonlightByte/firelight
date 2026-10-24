# M5 independently authored content

Four fresh coding-agent authors submitted these definitions/invocations. Individual files are exact accepted submissions, unedited by the controller. world.nql assembles the validated starter plus those four declarations, separated by one LF. journey.repl contains evaluator-authored bindings for the predefined six-request integration journey; it is not an independent author submission.

Run from the project root:

```sh
scripts/go.sh build -o bin/nql ./cmd/firelight
bin/nql -world examples/m5/world.nql -execute examples/m5/c01-invocation.nql -actor char:rowan -inspect
bin/nql -world examples/m5/world.nql -repl -actor char:rowan < examples/m5/journey.repl
```

Each CLI process starts a fresh world. Each REPL line is one request. The six-request journey ends at revision6 with16 events: HP10/12/15, defenses10/8/7 for Iris/Rowan/Sol, shield owned by Rowan, token by Iris, reward by Sol, all unworn. Iris retains marked; two guard lifetime IDs are retired. Detailed provenance, negative controls and complete API replay evidence are in docs/M5_REPORT.md and docs/M5_ACCEPTANCE.md.
