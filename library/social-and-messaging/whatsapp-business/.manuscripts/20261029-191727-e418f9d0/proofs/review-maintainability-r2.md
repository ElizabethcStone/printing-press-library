# Phase 17 maintainability review — round 2

PASS — no outstanding in-scope maintainability findings.

Repeated review of custom command wiring, shared schema behavior, capture/inspection packages, transport/catalog/auth and privacy helpers, root/MCP integration and durability records. No external calls, credential lookup, real mutation or source edits.

Both round-1 findings are resolved: generic display limits validate 1–1000 independently from an upstream schema limit; catalog captures use the actual endpoint and mutex-protected negotiated protocol. The fixture test confirms negotiated headers and persisted provenance. Shared privacy handling redacts nested inputs without modifying them and strips error echoes before truncating. Existing registration remains through the generator-supported novel hook; patch records include the new privacy file. No maintainability regression found in the merged fixes.

Verification: `go test ./internal/cli -run TestReviewLimitAndNegotiatedCatalogProvenance -count=1` and `go test ./internal/metamcp ./internal/inspection ./internal/captures -count=1` all exited 0.

## Template-shape retro candidate

Low: `internal/cli/root.go:325` retains generated help prose “run doctor to verify auth and connectivity,” although this print intentionally replaces doctor with a local credential check and uses connection ping for live connectivity. Template origin: `internal/generator/templates/root.go.tmpl` (root Long/health-check boilerplate). Filed rather than patched because phase17 reserves inherited template-shape issues for retro. README/SKILL and actual doctor output already disclose network_checked:false. This is not an in-scope code blocker.

Convergence: maintainability findings cleared in round 2; parent merges all reviewer results.
