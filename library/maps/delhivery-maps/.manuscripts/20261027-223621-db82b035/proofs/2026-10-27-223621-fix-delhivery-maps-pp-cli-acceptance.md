Acceptance Report: delhivery-maps-pp-cli
  Level: Quick Check (runner-owned matrix) + targeted live smoke (user-approved, quota-constrained)
  Runner matrix: 13/13 passed (phase5-acceptance.json status=pass), 2 fix loops used
  Gate: PASS

Why not Full dogfood: the test token allows ~50 requests/24h (shared pool) and 5/24h for /route-plan. A full matrix (happy path + JSON fidelity + error path per command) would exceed the pool, and the matrix's flat 30s per-command timeout is shorter than /route-plan latency (~30s). User chose "Quick check + targeted smoke".

Failures found and fixed (first quick run 9/14):
  - autosuggest help had no Examples section -> added Example (CLI fix, generated file annotation/literal).
  - address / coverage parent-group examples ran `address clean --file orders.csv ...` against a nonexistent file (exit 1) -> declared `pp:happy-args` placeholders (blocked-fixture) on leaf and parent groups (CLI fix). Same for fleet and trip groups.

Targeted live smoke (real API, via the printed CLI, ~19 shared-pool calls + 1 route-plan call):
  - route (motorcycle): recommended_route + alternates parsed; 0.825 km / 100 s.
  - matrix: 2x2 sources_to_targets returned.
  - toll-charges-by-od (bus_truck): 8 plazas, total INR 3888.72 (matches independent probe).
  - toll-charges-by-route (bus_truck): 2 plazas, INR 755.
  - isochrone (time=600s, encode_geojson=false): Polygon feature.
  - rvg: structured address; verify: is_verified false, granularity PREMISE; autosuggest passed live in the runner.
  - raster-tiles 256/10/731/437: valid WebP saved via --deliver.
  - trip quote (coords, car_jeep_van): 51.65 km, 83.9 min, 1 toll plaza INR 35, api_calls=2 (route + toll; proves recommended_route unwrap + toll body live).
  - coverage check (hub + 20 min, 2 orders with coords): 1 inside, 1 outside, 1 isochrone call.
  - address clean (1 address, 3 steps): ok, PREMISE, pincode 122003 via standardize postal_code, 3 calls.
  - address report / dupes: local, exit 0 (report grouped by pincode correctly).
  - fleet plan (3 orders, 2 vehicles, depot): chunk 1/1 planned in one /route-plan call (~30 s), plan_status boolean true parsed, 3 assigned, 22.169 km, CSV with stop sequence. Proves the plan_status fix live.

Not exercised live: /standarize and /validate and /geocode as standalone generated commands (exercised through address clean), `address purge`, `--max-calls`/lockout against a real 429, route with traffic_aware/departure_time, toll with waypoints, the 429 path of /route-plan. /verify-visit was cut earlier (503 on 5/5).
Quota observed: general pool ~10 left, route-plan ~3 left (estimates; client cannot read headers). Token expires 2026-10-28 22:32 IST.

Printing Press issues for retro: runner executes parent-group Example as the happy path and ignores leaf annotations; quick level covers only the first alphabetical commands (6 here) and makes no live endpoint calls; generated `--flag false` bool examples; `search` resource collision; no quota-aware (per-endpoint bucket) rate limiting in the generated client; spec drift (standarize postal_code vs pincode; no securitySchemes; per-endpoint quota undocumented).

---
## ADDENDUM 2: Publish-time live gate (2026-10-29)
Full live dogfood re-run at publish time (publish Step 4.5) with a fresh-quota token: 139/139 passed, 0 failed, hollow_features none, status pass. Cause of earlier hollow coverage for address clean / coverage check / fleet plan / trip quote: the runner treated them as mutating (no mcp:read-only) and only dry-ran them; fixed with mcp:read-only + read-only-unless-flags/write-flags=out, `--stdin` input and pp:happy-args/pp:happy-stdin. Proven first against a local mock (free), then live.
