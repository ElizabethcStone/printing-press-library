# Polish: delhivery-maps-pp-cli (Phase 5.5)

Scorecard 87 -> 90 (Grade A). Verify (mock) 100% -> 100% (41/41). Dogfood FAIL/WARN -> PASS. go vet 0. Tools-audit 0 pending. Hand-authored gosec findings 4 -> 0 (41 generator-emitted remain: retro candidates).

Fixes: removed 5 dead generator helpers plus the paginatedGet cascade (~700 lines) from internal/cli/helpers.go (Dead Code 2/5 -> 5/5); gosec fixes in address_purge.go (unchecked Close), address_report.go (#nosec G202 on fixed column allow-list), addrkit/table.go (#nosec G304 on user-supplied --file); mcp-descriptions.json trims isochrone_compute (2063 -> ~900 chars) and route_compute ("BREAKING CHANGE" changelog text removed); mcp-sync overwrite of curated tools-manifest/tools.go text reverted from backup.
Skipped: Cache Freshness 3/10 (stateless API; cache freshness intentionally disabled); MCP Token Efficiency 7/10 (needs spec mcp: block + regeneration: retro/reprint candidate); generator-emitted gosec findings; which.go gofmt drift (re-synced by dogfood).

Orchestrator re-verification after polish: go vet/build clean, go test -race 15/15 packages ok, shipcheck 7/7 legs PASS 90/100, all 7 novel commands resolve, go.mod still `go 1.27.3`, no token anywhere in tree or run dir.
Live matrix: see acceptance report (quick runner matrix 13/13 + targeted live smoke); polish itself ran mock-only by constraint.

ship_recommendation: ship
