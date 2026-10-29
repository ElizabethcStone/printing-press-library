Manifest transcendence rows: 6 planned, 0 built. Phase 3 will not pass until all 6 ship.

# Build log: delhivery-maps-pp-cli

## Slice 0: generation (done)
- Generated from official OpenAPI (patched: bearerAuth, operationIds for /validate,/verify, x-pp-resource autosuggest for /search, x-auth-env-vars DELHIVERY_MAPS_TOKEN). /verify-visit removed by user decision (503 on 5/5 live attempts).
- 13 endpoints promoted to flat top-level commands: geocode, rvg, validate, verify, standarize, route, matrix, toll-charges-by-od, toll-charges-by-route, isochrone, route-plan, autosuggest, raster-tiles.
- go.mod pinned `go 1.27.3` (host Homebrew Go 1.27.0 has stdlib advisories; govulncheck clean at 1.27.3). A `generate --force` reverts this; re-pin after any regen.
- Generator limitation: generated example for bool flags is `--encode-geojson false` (space form) which cobra rejects; narrative uses `=false`.
- Generator limitation: spec resource name "search" collides with framework `search`; fixed with x-pp-resource.

## Slice 1: novel commands (delegated; in progress)
address clean, trip quote, coverage check, fleet plan, address report, address dupes.
Constraint: no live calls during build (token has ~30 calls left for the Phase 5 dogfood).

## Slice 1 result (novel commands) - DONE, verified by orchestrator
Manifest transcendence rows: 6 planned, 6 built.
- Built: address clean, trip quote, coverage check, fleet plan, address report, address dupes. New: internal/addrkit (11 table-driven tests), internal/store/delhivery_maps_migrations.go (dm_cache, dm_address, dm_calls), internal/cli/dm_common.go (cachedCall, quota counter, 5xx retry, 429 mapping).
- Orchestrator verification: go build + vet + test all green; per-row Cobra resolution 6/6 (Usage `<leaf> [flags]`); dogfood novel_features_check planned=6 found=6; no real token in tree; address report/dupes exit 0 with [] on empty DB; address clean --dry-run plans without IO.
- Limitation: generated client does not expose x-ratelimit-* response headers, so the budget guard counts this CLI's own calls in a rolling 24h (dm_calls) vs 50 (override DELHIVERY_MAPS_DAILY_BUDGET); a real 429 always wins.
- Not verified against live API: /route encode_polyline=false behaviour, departure_time format, exact route-plan field types (parsed tolerantly). Phase 5 live dogfood covers these within the call budget.
- Deferred: none. Skipped body fields: none significant.
