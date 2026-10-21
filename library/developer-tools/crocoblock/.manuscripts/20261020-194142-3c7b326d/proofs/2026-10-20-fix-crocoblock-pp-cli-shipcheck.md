# Crocoblock shipcheck

Verdict: ship (final review, full live matrix and promotion gates still required).

Canonical umbrella: PASS, seven legs exit 0. Verify: 62/62, 100%, zero critical failures. Scorecard: 87/100 (A). All 61 approved leaves resolve; novel features 6 planned / 6 implemented, no stubs. Full Go suite passed. MCP read-only mode and SQL on the selected snapshot were smoke-tested.

Initial umbrella failed on mock-vs-HTTPS transport, stale staged binary, source discovery of wrapper constructors, canonical install section placement, and a Press detector that mistook version/property strings for hosts. Corrections: authenticated live verification, fresh staged binary, explicit Cobra declarations, canonical section restored with local setup outside it, narrowly tested AST detector repair. Current umbrella all PASS.

The native meta-box identifier and filters write readback were corrected against provider source and lab responses. Snapshot metadata+rows now read atomically; CCT capture validates selected schema fields. CROCOBLOCK_DB is shared by captures, journal, CLI reads and MCP SQL.

Evidence: shipcheck-1.json, shipcheck-2.json, crocoblock-approved-leaves.json, crocoblock-mcp-sql-smoke.json, per-slice acceptance and live proofs. Scorecard is structural; scenario tests provide behavior evidence. Seven unused generated helpers remain a non-functional static warning. Patched Press detector focused tests pass; unrelated full generator goldens have pre-existing missing fixtures/attribution differences and are not claimed passing.

Local review: 6 integration findings fixed; all three reviewers cleared round 2. Tests/build/vet and real published-form lifecycle pass. Generator retry candidate mitigated by the WordPress adapter; see phase-4.95-findings.md.

Final polish: ship; verify73/73=100%; score82 live/83 structural (87 historic). PII9→0, tools audit0. CLI61capabilities retained; MCP75usabletools including56domain+19framework. File-dependent CLI-only boundary documented. Final live matrix after polish252/252 PASS,246 conditional skipped/unverified.

Promoted locally; current-host activation PASS with persistent private config: doctor, discover, strict selected sync, forms list and MCP initialize/tools-list/discover. MCP75tools. Transient network read failure recovered with explicit longer sync timeout; source unchanged. No global Codex registration or public publication.
