# Maintainability review — round 2

Review path: direct subagent dispatch, maintainability reviewer. Result: **PASS**. No new in-scope findings or regressions identified.

Reviewed on 2026-10-21T04:10:44+00:00. This is a source review, not a test-suite receipt. The parent reported current CLI/store/client/MCP tests, build and vet passing; this reviewer did not rerun them. No additional targeted tests were necessary because no new finding required a reproduction. No laboratory requests, source edits or receipt edits were performed.

## Scope and method

The review covered the phase-17 paths: `internal/cli/`, `internal/mcp/` excluding `cobratree/`, `internal/store/`, `internal/client/`, and both `cmd/` entry points. It re-examined command registration, request and retry wrappers, local-store ownership, capture replacement/invalidation, read transactions, mutation readback and error propagation, private snapshot lifetime, output/resource cleanup, and the six analytical consumers. This was a fresh review of final behavior across the scope, not only a check of the two previous maintainability findings. Generator-reserved `internal/cliutil/` and `internal/mcp/cobratree/` remain outside the patch/review finding scope.

## Final contracts checked

- Curated registration removes raw transport command groups and installs the approved adapters and six analysis constructors. Root and MCP consume the same runtime tree. The eleven affected identifiers now use recognized required placeholders, and `internal/mcp/crocoblock_required_ids_test.go` checks both property existence and required status. No raw-args fallback requirement is asserted.
- The initial field backup is still exclusively created with mode 0600 before the remote mutation. Finalization writes and syncs a private sibling temporary file, closes handles, verifies that the target is still the original regular inode, and renames the new file. Errors before replacement preserve the original `Before`; the focused tests exercise partial-write, sync, rename, regular-file replacement and symlink replacement failures. Restore retains private-file checks and refuses an unverified snapshot or changed current values.
- `crocoRequest` clears configured standard and X-prefixed idempotency keys on mutations and passes the explicit replay-unsafe marker. The client applies per-request overrides after configured headers and closes response bodies. The dedicated regression tests cover POST, PUT and DELETE with configured and mixed-case idempotency headers. Read requests retain their deliberate read-only path.
- Reindex constructs the PHP site literal through base64, avoiding PHP interpolation of URL text. SSH execution uses the command context; temporary remote-source cleanup has its own bounded context. Reindex receipts identify the journal sequence observed before the operation.
- The shared reservation reader requires an explicit array even for total zero; absent, null and object payloads cannot become verified empty readbacks. The same reader is used by bookings and appointments, including mutation verification and synchronization.
- The **final** form implementation accepts the native `{fields:[...]}` envelope, rejects missing/null/malformed collections and malformed entries, and derives validation attributes from the same raw Gutenberg projection for drafts and published forms. Required/type/literal-limit changes remain visible to schema comparison; private defaults, action values and submitted values remain excluded from that projection. The final source was reread after the published-endpoint correction.
- `CrocoReplace` commits collection rows, FTS rows, capture metadata and sync state in one transaction. `CrocoSnapshot` observes rows and coverage in one read transaction, avoiding mixed generations for a resource. Iterators close before dependent SQL on the same transaction; rollback paths retain previous committed data. Site pinning rejects a different site's writes. Journal errors after mutation are surfaced as applied/unknown outcomes rather than successful writes.
- Analytical commands retain dry-run before file/store access, local-only access and the shared bounded context. Their consumers check required capture scope/completeness/version/identity, show capture provenance and report limited or unknown coverage instead of a clean global claim. Reconcile keeps ambiguous keys unpaired; relation audit distinguishes observed membership from unproven WordPress absence; overlaps applies explicit business policy to the separate models; filter impact limits claims to captured dependencies and the local journal.
- The preserved patch record covers runtime command-shape, private snapshot, form projection, strict reservation array, safe PHP and no-replay contracts. The hand-authored registration and helper boundaries remain intact; no additional reprint durability regression was identified.

## Findings

```json
{"reviewer":"maintainability","round":2,"status":"PASS","findings":[],"new_retro_candidates":[],"tests_run_by_this_reviewer":[],"lab_requests":0,"source_edits":0}
```

Reviewed source fingerprint: SHA-256 over sorted relative Go paths, NUL, bytes, NUL for all 192 in-scope source/test files: `c7a693d667b84cb1193fbecb8eb6d1fa60c876ea3cbbdc1277aa57c23ee8f82f`. Final `internal/cli/croco_forms.go` SHA-256: `dd47e6b6eb0504afb692915ff510c7b413af553cc9c9a2e4d6ae45bbac5548ac`.

Convergence outcome: no outstanding maintainability findings in round 2. Overall phase convergence remains the parent's merged decision across all reviewers.
