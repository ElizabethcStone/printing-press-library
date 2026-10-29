# Novel-features brainstorm (subagent audit trail)

## Customer model
1. **Ritu Sharma**, order-ops/address-quality analyst at a COD-heavy D2C brand. Cleans 5k-20k addresses every Monday; hits 50/window 429s and restarts from row 1 when a 24h JWT expires. Frustration: resuming a half-finished 10k-row run without re-paying 3-5s validate latency.
2. **Karthik Menon**, last-mile hub dispatch lead. Plans next-day runs via route-plan (50-order cap, ~31s), re-costs lanes weekly, drags isochrone GeoJSON into geojson.io. Frustration: sheets have 200+ address rows; he splits and geocodes by hand.
3. **Anjali Verma**, delivery-quality / RTO-dispute analyst. Runs /verify and /verify-visit one case at a time; /verify-visit 503s often. Frustration: no consolidated pass/fail sheet.
4. **Aarav Nair**, developer / AI-agent builder. Re-geocodes the same fixtures, burns the rate budget, gets opaque 401s on expired JWT. Frustration: no cache/history.

## Survivors (>=5/10)
| # | Feature | Command | Score | Persona | Tag |
|---|---|---|---|---|---|
| 1 | Bulk address cleaning pipeline | address clean | 10 | Ritu | hand-code |
| 2 | Delivery visit audit | visit audit | 8 | Anjali | hand-code |
| 3 | Trip quote (route+toll+ETA) | trip quote | 9 | Karthik | hand-code |
| 4 | Service-area coverage check | coverage check | 8 | Karthik, Ritu | hand-code |
| 5 | Fleet plan from orders CSV | fleet plan | 8 | Karthik | hand-code |
| 6 | Cached-address quality report | address report | 7 | Ritu | hand-code |
| 7 | Duplicate-doorstep finder | address dupes | 7 | Ritu | hand-code |

## Killed
address audit (= clean with one row); route decode (already route's decoded-polyline option); hub assign (VRP covers it); tiles mosaic (scope creep); route eta-profile (burns rate budget; note: departure_time IS in the spec, so this is a budget kill, not unverifiable); quota status (stale headers); address find (dupes framework search); history list (no domain question); trip report (estimates, speculative).
