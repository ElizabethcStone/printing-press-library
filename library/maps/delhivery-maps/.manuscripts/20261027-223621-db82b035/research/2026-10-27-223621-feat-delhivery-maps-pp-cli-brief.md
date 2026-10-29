# Delhivery Maps CLI Brief

## API Identity
- Domain: India-specific location intelligence (Delhivery, logistics; launched June 2026, "Naksha LLM" address models).
- Users: logistics/e-commerce ops, last-mile dispatch, address-quality and data teams, developers building delivery apps; AI agents (Delhivery also advertises an MCP).
- Base URL: https://gateway-maps-pub-int.delhivery.com (single gateway, JSON in/out, uvicorn/FastAPI behind an AWS WAF edge).
- Spec: OFFICIAL OpenAPI 3.0.3 served at https://www.delhivery.com/maps/openapi.json (14 paths, 38 schemas, 154 KB). Docs page (/maps/reference) is rendered from it. Spec gaps: no `securitySchemes`, `/validate` and `/verify` lack operationIds, 200-response shapes drift slightly from live (extra `request_id`).
- Surface (14 endpoints): geocode, reverse geocode (/rvg), validate, verify, verify-visit, standardise (/standarize [sic]), route, matrix, toll by OD, toll by polylines, isochrone/isodistance, route-plan (VRP), autosuggest (/search, GET), raster tiles (GET, binary webp).
- Data profile: stateless compute/lookup API. No list/sync resources. Every response carries `request_id` (+ echoes `req_id`).

## Reachability Risk
- Low. Live-probed 13/14 endpoints with a real token: all 200 with correct shapes (geocode 2.3s, validate 3.8s, verify 5.2s, route 0.17s, matrix 0.3s, isochrone 0.17s, toll_od 3.4s, route-plan 31s for 3 orders, tiles 0.18s).
- `/verify-visit` returned 503 "Service temporarily unavailable" on two live calls (docs: search backend or LLM dependency down; "safe to retry"). Treat as flaky dependency; CLI must retry-with-backoff on 503/502/504 and surface it clearly.
- Rate limit: headers `x-ratelimit-limit: 50`, `x-ratelimit-remaining`, `x-ratelimit-reset` (epoch). 429 on exceed. CLI should read these and back off.
- Gateway is behind AWS WAF; plain curl with Authorization header passed without a challenge.

## Auth (token reality)
- `Authorization: Bearer <JWT>` on every request. No securitySchemes in the spec; add `bearerAuth`.
- Test token is a WEB-session JWT: claims `source: WEB`, `ucid`, 24h lifetime (iat→exp = 86400s). The docs site mints it through a phone-OTP login in a browser (auth.js). Commercial keys are issued by Delhivery under an Enterprise Agreement.
- CLI design: env var `DELHIVERY_MAPS_TOKEN` (primary), config-file token via `auth set-token`, `auth status` that decodes the JWT `exp` locally (never prints the token) and warns when <1h left. OTP login automation is out of scope (interactive, WAF-protected).

## Top Workflows
1. Bulk-clean an address file: CSV/JSONL of raw addresses -> standardise + validate + geocode, with rate-limit-aware concurrency, resumable, output CSV with quality/granularity/lat/lng/pincode.
2. Delivery-address QA: validate (quality, granularity, diff) then verify delivery history; verify-visit to check a driver's GPS ping against the address cluster.
3. Trip costing: route + toll in one go (distance, ETA with traffic, toll plazas by vehicle type, alternates), plus distance matrix for dispatch.
4. Reachability / service-area: isochrone & isodistance -> GeoJSON file ready for QGIS/geojson.io.
5. Fleet planning: VRP route-plan from an orders CSV (coords or addresses), output stop sequences per vehicle.

## Table Stakes
- One command per endpoint with typed flags (all 14), `--json`, `--select`, `--dry-run`, agent-friendly exit codes.
- Readable output: decoded polylines, tile save-to-file, matrix as table.
- Stdin/file body support for large inputs (matrix, route-plan, toll polylines).

## Data Layer
- Primary entities: address (raw -> standardised/validated/geocoded), route/toll quotes, call history.
- Sync cursor: none (no list endpoints). Use a local SQLite as a request cache + history keyed by (endpoint, normalized input hash) so repeat geocodes/validations are free against a 50/window rate limit.
- FTS/search: over cached addresses (raw, formatted, pincode, city).

## Codebase Intelligence
- Omitted: no public repo/SDK exists. Competitor scan (npm/PyPI/GitHub/web): no Delhivery Maps CLI, SDK or wrapper found. Only an advertised "Delhivery Maps MCP" with no public setup docs. Generic `maps-cli` (PyPI) is unrelated.

## User Vision
- User asked: CLI for Delhivery Maps; study each API intensely via Chrome DevTools; convert each into CLI; test with supplied bearer token.

## Product Thesis
- Name: delhivery-maps-pp-cli
- Why it should exist: the only CLI for India's logistics-grade geocoder. Raw API is single-shot; real jobs are bulk (thousands of addresses/orders). A CLI that batches, caches, respects rate limits, decodes geometry and composes route+toll turns a demo gateway into an ops tool, and gives agents a typed local surface.

## Build Priorities
1. All 14 endpoints as first-class commands from the official spec (patched with bearer auth + operationIds).
2. `batch` (CSV/JSONL) for geocode/standardise/validate/verify with rate-limit-aware concurrency, retries on 502/503/504, resume, SQLite cache.
3. Compound: `trip` (route+toll+ETA), `address audit` (standardise+validate+geocode), `route decode`, `isochrone --geojson`, `tiles fetch --bbox`, `plan` from CSV.

## Reachability Gate
- Decision: PASS. GET /search = 200 with bearer, 401 without; 12 of 13 kept endpoints verified live. /verify-visit dropped (503 on 5/5 attempts, user-approved cut).
- Rate limit correction: spec info.description says 50 requests / 24 HOURS per user (not per short window). Test token had ~32 calls left at generation time; window resets 2026-10-28 22:37 IST, 5 minutes after the test token expires (22:32 IST). Live dogfood must be rationed; most verification runs against mocks.
- Design consequence: address clean / fleet plan / trip quote must pre-flight the remaining budget (x-ratelimit-remaining), cache every result, and refuse or truncate rather than burn the daily quota.
