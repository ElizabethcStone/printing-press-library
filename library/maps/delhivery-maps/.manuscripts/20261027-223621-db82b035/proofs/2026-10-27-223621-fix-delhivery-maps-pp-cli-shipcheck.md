# Shipcheck: delhivery-maps-pp-cli

Command: `cli-printing-press shipcheck --dir <work> --spec <patched openapi> --research-dir <run> --no-live-check`
(`--no-live-check` deliberate: the test token allows 50 requests/24h; live sampling is reserved for the Phase 5 dogfood budget.)

## Result
| Leg | Result |
|---|---|
| verify | PASS (mock mode) 41/41 after fix (was 40/41, 97.6%) |
| validate-narrative | PASS (7 narrative commands, full examples) |
| dogfood | PASS (novel features planned=6 found=6; dead flags 0) |
| workflow-verify | workflow-pass |
| apify-audit | PASS (n/a) |
| verify-skill | PASS (flags, commands, canonical sections) |
| scorecard | PASS 87/100 Grade A (before = after) |

## Blockers found and fixed
- verify 40/41: `isochrone` happy-path fixture synthesized `--origin=28.6139` for an array flag; real command dry-runs fine. Fixed the `pp:happy-args` annotation to `--origin=[28.6139,77.209]` in internal/cli/promoted_isochrone.go.
- Earlier (generate): `/search` resource collided with framework `search` -> `x-pp-resource: autosuggest`; govulncheck failed on host Go stdlib -> go.mod pinned 1.27.3 (clean).

## Known non-blocking items (polish phase)
- 5 dead generator helpers (compactFields, isDryRunResponseForClient, paginatedGetWithResponsePath, retainCLIQueryParams, retainExplicitQueryParams): Dead Code 2/5.
- Cache Freshness 3/10 (stateless API; cache intentionally disabled), MCP Token Efficiency 7/10, Vision 6/10.
- Live API Verification: N/A until Phase 5 live dogfood.

## Recommendation: ship
All ship-threshold conditions met; every novel feature has sample behavior verified offline against mocks. Live behavior verified in Phase 5.
