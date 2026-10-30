Manifest transcendence rows: 7 planned, 7 built. Phase 3 will not pass until all 7 ship.

# Phase 3 build log: pingenerator-pp-cli (run 20261029-192233-d337ee12)

## Architecture
- Generated scaffold (v4.34.2) owns root, config, auth, doctor, output/select, MCP server, learn loop, README/SKILL/manifest.
- Spec declares one endpoint: the MCP JSON-RPC passthrough (`mcp`, POST /mcp, mutation: true).
- Hand-authored, regen-mergeable:
  - `internal/pgmcp/` (catalog.go, rpc.go, shapes.go, ledger.go + pgmcp_test.go): 21-tool catalog with credit cost table, JSON-RPC tools/call envelope, SSE strip, content[].text unwrap, isError/error classification, tolerant response-shape helpers, append-only spend ledger.
  - `internal/cli/pingenerator_tools.go`: 21 typed tool commands + offline `tools` catalog, registered through `registerNovelCommand`; credit guard (`--execute`), `--dry-run` previews, ledger writes, harness refusal for spends.
  - `internal/cli/pingenerator_novel_helpers.go` + 7 novel command files (pin-post, pin-products, unpinned-posts, preflight, ledger, queue-gaps, credit-runway).
  - `internal/cli/pingenerator_mcp_test.go`: httptest MCP mock integration tests.
- Transport: novel and typed commands post through the generated `internal/client` (`PostWithHeaders`), so Bearer auth, PINGENERATOR_BASE_URL, timeouts, retries, rate limiting and verify mocks come from the machine. No sibling HTTP client exists.

## Rows built
- Absorbed 1-21: all 21 tool commands, names and flag names identical to the prior CLI (test: TestCatalogMatchesPriorSurface, TestPriorFlagsStillAccepted).
- Absorbed 22-23: credit guard and --dry-run previews; zero network calls without --execute (TestCreditToolsNeverDialWithoutExecute).
- Absorbed 24: `tools` (manifest row resolved in Phase 3: `tools` is not framework-reserved, prior name kept).
- Absorbed 25-27: generated doctor, --agent/--compact/--select, typed exits.
- Absorbed 28: generated `mcp` passthrough.
- Transcendence 1-7: pin-post, preflight, pin-products, unpinned-posts, ledger, queue-gaps, credit-runway.

## Decisions
- Exit codes follow the machine palette (2 usage, 3 not found, 4 auth, 5 API incl. "no credits remaining", 7 rate limit, 10 config). Prior CLI used 3 auth / 4 credits / 5 rate limit. Intentional break, documented in reconciliation.
- `--base-url` (prior) replaced by the generator's PINGENERATOR_BASE_URL env override.
- Nested response field paths use `/` separators so dogfood's host scanner does not read them as domains.
- Response-shape assumptions for list_scheduled_pins / list_automations / get_account_status are tolerant (multiple candidate keys) and documented as unverified live.

## Gate results
- Per-row Cobra resolution: 30/30 approved paths resolve (`<leaf> [flags]` usage line).
- dogfood novel_features_check: planned 7, found 7, not skipped.
- `generate --force` regen: all 15 hand-authored files byte-identical afterwards; tests green.
- go test -count=1 ./... : PASS.
- dogfood verdict FAIL on two items, both carried to shipcheck/polish:
  1. 9 dead generator helper functions (one-endpoint spec leaves them unused) -> deterministic polish.
  2. novel-host DNS check: `example.com` placeholders (documented in discovery/documented-urls.txt) report "does not resolve in DNS" because this sandbox's DNS only resolves allow-listed domains (getent hosts example.com -> NXDOMAIN; github.com resolves). Environment-limited; passes on an unrestricted resolver.
- govulncheck: not run; vuln.go.dev denied by session network policy (HTTP 403).
