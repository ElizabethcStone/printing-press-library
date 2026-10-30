# Round 2 correctness review

Reviewed all prior correctness scope plus cascading effects of merged privacy, limit, endpoint/protocol and effective-business changes. Original cross-business mismatch regression is fixed; actual protocol/endpoint are captured. Generic output limits no longer accept zero while schema-defined string limits remain intact. Error redaction runs before bounded truncation.

Validation: `go test -count=1 ./internal/inspection ./internal/metamcp ./internal/captures ./internal/cli` passed all four packages. No source edits, external requests or credentials accessed.

## Remaining P2 — Neither capture establishes a selected business

`internal/inspection/diff.go` now rejects differing effective business IDs but still accepts two empty IDs. The official accounts input permits business_id omission and its output schema permits replies without a business_id (selection replies with needs_selection/businesses). Equal empty request scopes and missing effective IDs do not establish the same selected business. Such records can still produce complete additions/removals.

Narrow fix: for known business-scoped tools, when both effective IDs are empty, require an explicit stable source-specific asset selector. Preserve templates whose equal scope contains waba_id; permit accounts phone_number_id; phone inventory waba_id/phone_number; onboarding app_id/phone_number_id; app publication app_id. Global businesses and template-library lists remain global. Reject unselected scope where none of these is present. Parent notified; followup convergence pending.

No other actionable correctness finding found.
