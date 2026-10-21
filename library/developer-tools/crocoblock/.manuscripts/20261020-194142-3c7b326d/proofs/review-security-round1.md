# Phase 17 — Security review, round 1

Review path: direct reviewer subagent, security persona. Read-only source review; no CLI edits, no lab commands, no PHP/SSH execution. Scope: `internal/cli`, `internal/mcp` excluding `cobratree`, `internal/store`, `internal/client`, and `cmd`. Reserved MCP code was consulted only to check the integration boundary.

## In-scope finding S1 — P2 — URL data becomes executable PHP interpolation

**Location:** `internal/cli/croco_reindex.go:49–50`.

**Condition/reproduction:** configure a base URL whose path contains `${phpinfo()}`, for example `https://example.test/${phpinfo()}/wp-json`, and invoke `filters reindex` for an authorized SSH installation. The code emits `$expected = "https://example.test/${phpinfo()}";` into the PHP source. `json.Marshal` quotes JSON correctly, but a PHP double-quoted string interpolates PHP expressions. The site validator (`internal/store/crocoblock.go:43–55`) allows this path: it contains no forbidden whitespace, userinfo, query, or fragment. A fresh journal permits the site. Assignment occurs before `get_option('siteurl')` is compared. This is a source-level reproduction; PHP and remote execution were not run.

**Impact:** crafted URL data can invoke a PHP function under the remote WordPress process before the site check, instead of being compared as literal URL bytes. Ordinary `$name` URL paths are also changed by interpolation. The precondition is an operator-supplied/configured URL containing PHP interpolation syntax; this is not a claim that an arbitrary unauthenticated website can modify CLI configuration.

**Fix:** encode the expected URL with `base64.StdEncoding` and emit `base64_decode('ENCODED_BYTES')`, keeping a fixed, non-interpolated PHP source expression. Add a source-generation regression fixture containing `$`, braces, single/double quotes, and backslashes, verifying exact decoded bytes and the site check still precedes index execution. This is hand-written integration code, not a template-shape finding.

## Template-shape retro candidate R1 — P2 — Idempotency header overrides explicit no-replay

**Location:** `internal/client/client.go:1882–1888`; integration opt-out at `internal/cli/croco_base.go:109–110`.

**Template:** `work/printing-press-go-floor-fix/internal/generator/templates/client.go.tmpl:3334–3340` has the identical branch.

**Reproduction:** a temporary Go overlay outside the CLI adds a TLS httptest using the existing `crocoEngineTestHTTP` helper. It sets `c.Config.Headers["Idempotency-Key"]`, calls the real `crocoRequest` POST write path, returns HTTP 500 after the first simulated commit and 200 after the next request. Despite `ReplaySafeHeader=false`, the client logs a retry and sends **two POSTs**. Raw proof: `review-security-round1-replay.jsonl`, `OBSERVED_MUTATION_REQUESTS=2`. The probe passed by demonstrating the bug, not by asserting safe behavior. The CLI tree was never modified.

**Impact:** an operator-configured idempotency header silently re-enables replay for Crocoblock writes. These adapters have not established an upstream idempotency-key contract. The inspected JetBooking callback reads request parameters and reaches `insert_booking` without checking such a header (`work/paid-plugin-source/jet-booking/includes/rest-api/endpoints/add-booking.php:36–119`); source search across the inspected JetBooking/JetAppointment PHP found no idempotency-header implementation. Other plugin policies may independently reject a repeated write, but there is no deduplication guarantee, and the advertised unconditional no-replay contract is false under this valid header configuration.

**Fix direction:** an explicit false replay marker must dominate generic header presence, or provider-supported idempotency must be a separate explicit capability. Add regression coverage for a configured key plus an explicit false marker. Filed instead of patching the generated client because the behavior comes from the generator template. Parent will mitigate at the WordPress adapter boundary by overriding both `Idempotency-Key` and `X-Idempotency-Key` to empty strings for writes, so unsupported keys cannot override the explicit no-replay policy. This integration-specific mitigation preserves the upstream retro candidate; add regression tests for either configured header and both together. No local client-template edit was made.

## Checked boundaries and evidence

- Curated request paths are fixed or validated; post-type REST bases are slug checked and restricted to `wp/v2`. Generated transport concatenates them with the configured base. Auth/plaintext and redirect guards were inspected.
- The MCP server exposes stdio only. Curated commands are mirrored from Cobra; no removed raw endpoint registration remains in `RegisterTools`. Mutation/slot annotations are not read-only; the MCP read-only gate filters them. Root credential/routing flags and automatic `--yes` are blocked by the reserved mirror. The integration does not claim an extra confirmation prompt for every mutation.
- SSH host and installation-path arguments, shell quoting, `CommandContext`, private-key argument handling, and output handling were reviewed. S1 is the confirmed interpolation flaw.
- Snapshot creation uses exclusive 0600 creation. Restore rejects symlinks/nonregular files and nonprivate modes, checks opened inode identity, schema/selected keys and current values before mutation. This is a concurrency check, not compare-and-swap, as documented.
- Default record output excludes submission values; explicitly selected values still redact sensitive names. Recursive secret redaction and capture field allowlists were inspected. Journal detail admits constrained metadata, not arbitrary API payloads.
- Read-only SQL is single-statement gated, opens a read-only store and applies a query deadline. Local analysis wrappers bind context and reject live sourcing. The parent timeout-boundary proof was accepted as corroboration.
- Existing targeted tests: **12 PASS, 0 FAIL**, `GOTOOLCHAIN=go1.26.10`; raw events in `review-security-round1-tests.jsonl`. Covered redaction, identifiers, private snapshots/restore conflict, nested sensitive fields, dry-run I/O boundaries, unknown-field refusal, journal-failure nonretry and slots nonreplay without an idempotency override.

Convergence: round 1 has **1 actionable integration finding (S1)** and **1 template-shape retro candidate (R1)**. No other confirmed security findings. No source fixes were applied by this reviewer.

## Authorized autofix handoff

The integration security correction and the adapter mitigation are complete in four owned files; the generated client remains unchanged. Focused checks: 4 top-level tests, 22 PASS events including subtests, zero failures. Evidence: `review-security-autofix.json` and `review-security-autofix-tests.jsonl`. No PHP/SSH/lab execution occurred; the cleanup test used a local executable stub. The later vet attempt encountered a concurrent `croco_forms.go:280` undefined `raw` outside these owned files; parent must repeat after all fixes settle. All four owned files are released for the merged second review.
