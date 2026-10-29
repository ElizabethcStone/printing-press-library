# Delhivery Maps CLI Absorb Manifest

## Absorbed (match or beat everything that exists)
USER DECISION (absorb gate): /verify-visit and `visit audit` DROPPED - endpoint returned 503 on 5/5 live attempts; user approved cutting it. 13 endpoints + 2 foundations + 6 novel features remain.
Competitor landscape: none (no CLI/SDK/wrapper). Source of truth is the official OpenAPI (14 paths) + Delhivery's advertised MCP tool list.

| # | Feature | Best Source | Our Implementation | Added Value |
|---|---------|-------------|--------------------|-------------|
| 1 | Geocode raw address -> lat/lng/error_radius/pincode | Delhivery Maps API/MCP | (generated endpoint) geocode geocode-address | --json, --select, --dry-run, typed exit codes |
| 2 | Reverse geocode lat/lng -> structured address | Delhivery Maps API/MCP | (generated endpoint) rvg reverse-geocode | `--steps` flag, agent output |
| 3 | Validate address (quality, granularity, corrections diff) | Delhivery Maps API | (generated endpoint) validate validate-address | same |
| 4 | Verify delivery history at address (1-24 months) | Delhivery Maps API | (generated endpoint) verify verify-address | same |
| 6 | Standardise address into components | Delhivery Maps API/MCP | (generated endpoint) standarize standardize-address | same |
| 7 | Route (modes, alternates, traffic ETA, avoid tolls/highways, maneuvers) | Delhivery Maps API/MCP | (generated endpoint) route compute-route | polyline decode via output_format |
| 8 | Distance matrix | Delhivery Maps API/MCP | (generated endpoint) matrix compute-matrix | table/CSV output |
| 9 | Toll charges by origin-destination (8 vehicle types, waypoints, alternates) | Delhivery Maps API | (generated endpoint) toll-charges-by-od toll-charges-by-od | same |
| 10 | Toll charges by polylines (up to 3 routes) | Delhivery Maps API | (generated endpoint) toll-charges-by-route toll-charges-by-route | same |
| 11 | Isochrone / isodistance polygon | Delhivery Maps API | (generated endpoint) isochrone compute-isochrone | --out GeoJSON file |
| 12 | Route planning / VRP (<=50 orders) | Delhivery Maps API | (generated endpoint) route-plan plan-routes | body from file/stdin |
| 13 | Autosuggest (address, place, pincode, lat/lng) | Delhivery Maps API/MCP | (generated endpoint) autosuggest search-suggest (renamed from /search to avoid the framework search command) | lat/lng bias flags |
| 14 | Raster map tile (256/512 webp) | Delhivery Maps API/MCP | (generated endpoint) raster-tiles get-raster-tile | binary save to --out file |
| 15 | Auth: env/config token, local JWT expiry check | (new; Delhivery has none) | delhivery-maps-pp-cli auth set-token / auth status | warns when token <1h from expiry; never prints token |
| 16 | Rate-limit + transient-error handling | (new) | (behavior in delhivery-maps-pp-cli root client) x-ratelimit-* aware back-off, retry 502/503/504 | works for every command |

## Transcendence (only possible with our approach)
| # | Feature | Command | Buildability | Why Only We Can Do This | Long Description |
|---|---------|---------|--------------|------------------------|------------------|
| 1 | Bulk address cleaning pipeline (score 10) | address clean | hand-code | Composes standardise+validate+geocode with rate-limit back-off, retries, resumable SQLite cache keyed by (endpoint, input hash) | Use this for files (CSV/JSONL) of raw addresses that need standardising, validating and geocoding. Do NOT use it to analyse results already cached; use 'address report' instead. Do NOT use it to find repeated doorsteps; use 'address dupes'. |
| 2 | Trip quote: route + toll + ETA (9) | trip quote | hand-code | Chains route polyline into toll-by-polylines, geocodes addresses via cache, one record per trip, optional lanes CSV | Use this to cost individual origin-destination trips (distance, traffic ETA, tolls). Do NOT use it for multi-stop vehicle assignment; use 'fleet plan' instead. |
| 4 | Service-area coverage check (8) | coverage check | hand-code | isochrone polygon + cached geocodes + local point-in-polygon flags orders outside a hub's area | none |
| 5 | Fleet plan from orders CSV (8) | fleet plan | hand-code | Geocodes address-only rows (cached), chunks to <=50, calls route-plan per chunk, merges per-vehicle stop sequences | Use this to turn an orders file (addresses or coordinates) into per-vehicle stop sequences. Do NOT use it to cost a single lane; use 'trip quote' instead. |
| 6 | Cached-address quality report (7) | address report | hand-code | Local SQLite aggregation of validate/geocode results by pincode/city/granularity; API has no list endpoints | Use this to summarise quality across addresses already cleaned and stored locally. Do NOT use it to call the API on new addresses; use 'address clean' instead. |
| 7 | Duplicate-doorstep finder (7) | address dupes | hand-code | Local grouping of cached addresses by standardised text or geocodes within N metres | Use this to find repeated delivery locations across cached addresses. Do NOT use it for per-pincode quality stats; use 'address report' instead. |

Data-source directives: address clean, trip quote, coverage check, fleet plan = `live` (with local cache); address report, address dupes = `local`.

## Stubs
None. All rows ship fully.

## Source: audit trail in $RESEARCH_DIR/*-novel-features-brainstorm.md
