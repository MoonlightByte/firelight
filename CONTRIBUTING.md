# Contributing to Firelight

Thank you for helping keep the table honest. Firelight is developed by Eternal Tavern as the rules engine behind NeverEndingQuest.

## Issues

- **Bug:** give the smallest world and actions that reproduce it, the request JSON, the response JSON, and what you expected instead. A rules question cites the rule source (for example the SRD section).
- **Feature:** describe the game rule or host need first, then a proposed NQL form or protocol field. A new verb or field is a contract change and is discussed in an issue before any PR.
- **Security:** do not open a public issue. Report it privately through this repository's private vulnerability reporting (the Security tab, "Report a vulnerability").

## Tests

- Tests are tracked with the code they cover. Go tests sit next to their package or in `tests/`.
- Every test is deterministic and offline: no network, no clock reads, no random seeds outside the engine's own seeded rolls, and no recorded or simulated language-model output. Firelight decides outcomes, so its tests check outcomes; a host's narration is not Firelight's to test.
- A bug fix lands with a regression test that fails before the fix.
- A protocol change updates docs/PROTOCOL.md and its golden tests in the same PR.

## Pull requests

Before you open one:

```sh
gofmt -l .        # prints nothing
go vet ./...
go test ./...
go test -race ./...
```

A PR is ready for review when:

1. All of the above pass on the PR head.
2. It keeps the standard-library-only rule (no new module dependencies).
3. Source and docs are ASCII. No secrets, host paths or personal data appear in code, tests or fixtures.
4. Behaviour changes say which events, fields or diagnostics change. Protocol changes are additive unless an issue agreed otherwise.
5. It changes one thing. Refactors and behaviour changes go in separate PRs.

Maintainers review for correctness against the rules, atomicity (all or nothing), and determinism. Contributions are accepted under the repository LICENSE.

---

Firelight Engine, by Eternal Tavern.
