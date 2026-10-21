# Phase 17 — Security review, round 2

Result: **PASS — zero new or unresolved integration security findings.**

Review path: direct subagent dispatch, security persona. Re-reviewed the whole phase-17 security surface: `internal/cli`, `internal/mcp` excluding reserved `cobratree`, `internal/store`, `internal/client`, and both `cmd` entry points. Compared the current source hashes with `phase-4.95-before-source-hashes.json`, reviewed every changed source/test file, and re-traced unchanged auth/routing, data-output, storage, mutation, MCP and timeout boundaries. No source edits, no laboratory activity and no new suite execution in this review.

## Changed boundaries examined

- **M1 identifiers:** required ID placeholders now reach MCP schemas. IDs are still parsed inside bounded command execution after dry-run; token/route/confirmation-bypass controls remain in the same mirror. Mutation commands and the potentially mutating slot view remain outside read-only mode.
- **M2 private snapshots:** initial backup still uses exclusive 0600 creation. Finalization writes and syncs a 0600 sibling temporary file before replacing the directory entry. It checks original path identity before and after writing, does not follow a final target symlink, cleans temporary files, and preserves original bytes when write/sync/rename fails. Restore still validates private regular-file identity, site, declared/exposed fields and current selected values. The documented restore concurrency check remains distinct from server-side CAS.
- **C1 form projection:** new retained attributes are allowlisted boolean switches, enumerated input types and literal numeric limits. Private defaults, submitted values, option values, validation messages/rule values, presets and action settings are not added to schema output. The final published-form branch accepts the official `{fields:[...]}` envelope and rejects missing/null/nonarray members; draft parsing remains read-only. Re-read the final source after the agent released the native-envelope correction.
- **C2 reservation capture:** missing/null/nonarray `data` fails explicitly instead of becoming a complete empty capture. A delivered `data:[]` remains valid. No new request path or mutation classification was introduced.
- **S1 PHP boundary:** site URL bytes now travel only through a base64 literal inside a single-quoted `base64_decode` expression. The site comparison remains before index mutation. SSH host validation, shell quoting, `CommandContext`, cleanup registered before upload, and journal preparation error behavior remain intact.
- **S2/R1 replay boundary:** curated POST/PUT/DELETE writes override both `Idempotency-Key` names to empty strings alongside the explicit false replay marker. The generated client's configured-key branch therefore cannot re-enable retries for these adapters. Existing focused TLS proof covers each key, mixed casing, both keys and no key across all three methods.

## Whole-scope checks

- HTTPS/auth route checks, fixed or validated relative endpoint paths, redirect credential handling and credential error masking were revisited. Removed raw API transports are not re-registered through MCP.
- Native MCP calls use selected tool names, validated input shapes and explicit read/write intent. The server is stdio-only. Generated SQL remains single-statement gated, read-only and bounded; search/SQL use the operator-selected snapshot path.
- Default submission reads and records sync retain presence metadata without field values. Sensitive-key redaction remains recursive; explicitly selected field operations retain declaration/REST-schema checks. Customer data is excluded from reservation snapshots and journal details remain constrained operational metadata. The invocation journal records argument shapes rather than payload values.
- Local analyses continue using read-only snapshots with coverage/site/version checks and bounded contexts. Crocoblock store SQL remains parameterized; row/capture replacement and snapshot reads retain their transactional boundaries. No new dynamic SQL, shell interpolation, unbounded request path or external-data write sink was introduced.

## Evidence and retained retro

- Source digest and changed-file inventory: `review-security-round2.json`.
- Prior focused security tests: `review-security-autofix-tests.jsonl` (**22 PASS events including subtests, zero failures**). Tests use TLS/local stubs; no remote PHP execution was claimed.
- Parent reports final whole-Go CLI/store/client/MCP tests, build and vet **PASS**, plus published-form live smoke **PASS**. Those parent checks were not repeated by this reviewer. The transient round-1 vet error from concurrent form edits is resolved in the final source.
- **R1 / parent S2 remains a template-shape retro candidate, mitigated locally.** Template source `internal/generator/templates/client.go.tmpl:3340` still lets a generic idempotency header override false replay intent. WordPress-specific header suppression closes the integration path without editing the generated client. Keep the upstream retro record; it does not count as an outstanding integration convergence finding.

Convergence outcome: security findings cleared at round 2. No additional actionable security findings or regressions identified.
