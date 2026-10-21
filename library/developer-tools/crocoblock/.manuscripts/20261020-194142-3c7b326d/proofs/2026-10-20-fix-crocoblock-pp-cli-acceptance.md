# Acceptance Report: Crocoblock

Level: Full Dogfood, explicitly selected by user.
Gate: **PASS**. Final binary-owned matrix after polish: **252/252 mandatory checks passed, 0 failures**.

246 conditional rows were skipped/unverified; these are not counted as passes. Reasons:

- 48: mutating command requires --allow-destructive
- 44: no positional argument
- 27: mutating command dry-run only
- 25: mutating command; error_path would call live API without --dry-run
- 23: list companion previously failed at depth 0 for "id"
- 15: non-id positional "type" at depth 0
- 14: no id parseable from companion at depth 0
- 11: non-id positional "name" at depth 0
- 9: non-id positional "query" at depth 0
- 9: list companion previously failed at depth 0 for "relation-id"
- 5: list companion previously failed at depth 0 for "service-id"
- 3: command does not honour --dry-run
- 3: command path [export] has fewer segments than placeholders (2)
- 3: non-id positional "text" at depth 0
- 3: non-id positional "resource" at depth 0
- 2: no --dry-run short-circuit
- 2: list companion failed at depth 0: exit 2

All six approved analyses have complete real state-creating lifecycle proofs and cleanup in crocoblock-local-analysis-lifecycle.md. Current runner proof coverage: [{"command": "cct reconcile", "proof": "crocoblock-local-analysis-lifecycle.md"}, {"command": "relations audit", "proof": "crocoblock-local-analysis-lifecycle.md"}]. Base write families have separate real disposable-lab lifecycles; automatic matrix mutations use previews unless specifically enabled.

Repairs: parent help-group metadata corrected; real schema fixture coverage completed, including relations. Polish kept all 61 CLI capabilities while omitting five file-dependent domain tools and framework import from MCP, which cannot accept their required filesystem parameters. Final MCP exposes 56 domain plus 19 framework tools. No hand-edited acceptance marker. First attempts and pre-polish 248/248 pass retained as history. Real strict sync and native auth/discovery passed.

Printing Press retro candidates retained locally: parent scaffold metadata omission, host-data detector false positive, retry-template override and file-dependent MCP mirror operability. No public issues or PRs created. Temporary live snapshot databases are excluded from the deliverable.
