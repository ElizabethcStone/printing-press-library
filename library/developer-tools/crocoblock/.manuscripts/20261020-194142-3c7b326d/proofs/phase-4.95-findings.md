# Local code review — PASS

6 in-scope findings autofixed in-place across 2 rounds; this generated working directory has no Git commits. Authoritative change/proof records: `.printing-press-patches/curated-wordpress-surface-and-site-config.json`, `review-round1-merged.json`, focal regression reports, and `review-c1-live.json`.

## Template-shape retro candidate
R1 / S2 — high: `internal/client/client.go:1888`, from `internal/generator/templates/client.go.tmpl:3340`, allows a configured Idempotency-Key to override explicit replay-unsafe intent. A TLS test observed two POST attempts after a first committed response returned 500. The generated template is not patched in this integration. WordPress-specific request adapters now suppress both Idempotency-Key variants for mutations; focused POST/PUT/DELETE regression tests pass. The integration risk is mitigated; retain this machine bug for a future Printing Press repair. No public issue was submitted.

## Other findings
No out-of-scope or surface-to-user findings remain. No shipping scope was removed.

Findings cleared at round 2: correctness, security and maintainability all PASS with empty merged findings.
Review path: direct subagent dispatch with correctness, security and maintainability personas; all three reviewed every round. Codex simplify exemption applies.

Validation: complete CLI/store/client/MCP Go tests, build and vet passed; all eleven MCP identifiers are required; live draft/publish/edit form lifecycle and fixture cleanup passed. See the three `review-*-round2.md` reports.
