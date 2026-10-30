# Shipcheck: pingenerator-pp-cli (run 20261029-192233-d337ee12)

Verdict: **hold** (shipcheck umbrella exits non-zero; live dogfood cannot run in this environment)

## Final umbrella (shipcheck-final.txt)
| Leg | Result |
|---|---|
| verify | PASS, 53/53 (100%), 0 critical, mock mode |
| validate-narrative | PASS, 9 narrative commands, full examples |
| dogfood | FAIL (single remaining item, see below) |
| workflow-verify | PASS (no workflow manifest) |
| apify-audit | PASS (no Apify references) |
| verify-skill | PASS (flag-names, flag-commands, positional-args, shell-var-quotes, unknown-command, canonical-sections) |
| scorecard | PASS, 85/100 Grade A (86 before hiding the raw MCP endpoint tool) |

Scorecard: run 1 = 82/100, loop 3 = 86/100, final = 85/100 after the passthrough safety fix. Dead code 1/5 -> 5/5. Weak dimensions: Insight 4/10, Cache Freshness 3/10 (intentionally no sync/cache; MCP surface is live-only), Vision 6/10, Workflows 6/10, MCP Quality 6/10.
Scorecard live sample: 2/5 passed, 2 skipped, 3 failed with proxy `Forbidden` (vendor host denied by session network policy; no access key present).

## Remaining dogfood failure
`novel feature host(s) ... unverified host "example.com" does not resolve in DNS`.
- `example.com` appears only as a placeholder in help examples and is listed in discovery/documented-urls.txt as a placeholder.
- Before documenting it the error was "not in research artifacts"; after documenting it the error became "does not resolve in DNS".
- This sandbox resolves only allow-listed domains: `getent hosts example.com` -> exit 2 (no answer); `getent hosts github.com` -> 140.82.112.3.
- Expected to pass on an unrestricted resolver. Not proven here.

## Fixes applied during shipcheck
1. verify-skill unknown-command / flag-commands: 21 tool commands now come from a rendered static file (`pingenerator_tools_static.go`) with literal `Use:` and `XxxVar` flag declarations plus literal `addNovelCommandIfAbsent` registration; `TestPGStaticToolsUpToDate` re-renders from `pgmcp.Tools` and fails on drift.
2. validate-narrative: `--select` is ignored on dry-run previews (it targets the real payload). Also the umbrella read a stale `build/stage/bin` binary; rebuilt before the final run.
3. Dead code: 9 unused generated helpers removed with an AST edit (`cli-printing-press polish --remove-dead-code` reverted itself because it left an unused `strconv` import in resource_paths.go; retro finding).
4. Safety (from review): credit-spending calls with --execute get a 120s timeout floor unless --timeout is explicit (prior CLI default; generated default is 60s). Ambiguous failures (transport error, deadline, 5xx) on a spend append an `unconfirmed` ledger entry so pin-post/pin-products will not retry into a double spend. Tests: TestAmbiguousSpendIsLedgeredUnconfirmed, TestSpendTimeoutFloor.
6. Safety (from final review): the generated raw `mcp` passthrough could send `tools/call` for a credit-spending tool with no --execute guard (confirmed against a mock: the request reached the server). Fixed two ways: spec `mcp.endpoint_tools: hidden` removes the raw `mcp_rpc` tool from the MCP server, and a hand-authored hook refuses passthrough `tools/call` for known spending tools (pointing at the typed command with --execute) and for unknown tools unless --allow-unknown-tool. Test: TestPassthroughCannotSpendCredits. MCP server tools/list now shows 44 tools, no `mcp_rpc`; spending tools carry readOnlyHint=false and an explicit `execute` parameter.
5. Novel commands annotated for a future live gate: pin-post and pin-products `pp:preview-happy-path` (no spend without --execute); account-specific inputs declared as `example-value` fixtures.

## Not run / environment-blocked
- Live dogfood (Phase 5 acceptance): no PINGENERATOR_ACCESS_KEY; vendor hosts denied by session network policy.
- govulncheck: vuln.go.dev denied (HTTP 403).
- Generated test `TestSkipLearnHook_FrameworkCommandsDoNotCreateDefaultDB` (teach_test.go, generator-owned) runs `doctor`, which sends one unauthenticated GET to the real base URL; the proxy refused it. No credentials, no writes. Retro finding.

## Known gaps (also for README if shipped)
- Response field names for list_scheduled_pins, list_automations, get_account_status, get_post_content and list_store_products are parsed tolerantly but not verified against live responses.
- Generated `doctor` checks reachability with GET / and reports credentials as "present, not verified"; the prior CLI's doctor made an authenticated get_account_status call.

## Recommendation
hold: not shippable until (a) live read-only dogfood runs with a real access key against the allow-listed host, and (b) the dogfood host check runs on an unrestricted resolver.
