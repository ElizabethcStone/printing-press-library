# Polish (Phase 5.5) result

```
---POLISH-RESULT---
scorecard_before: 91
scorecard_after: 91
verify_before: 100
verify_after: 100
dogfood_before: PASS
dogfood_after: PASS
dogfood_live_matrix_before: not_exercised
dogfood_live_matrix_after: not_exercised
govet_before: 0
govet_after: 0
gosec_before: 0
gosec_after: 0
tools_audit_before: 0 pending
tools_audit_after: 0 pending
publish_validate_before: skipped (mid-pipeline)
publish_validate_after: skipped (mid-pipeline)
fixes_applied:
- gofmt internal/cli/which.go
skipped_findings:
- dead printStampedDryRunOutput: generator-emitted, guarded by var _
- gosec 46 findings: all in generator-emitted files; retro candidates (pinned gosec v2.26.1 crashes on go1.27; ran with GOTOOLCHAIN=go1.26.9)
- live-check/output review: isolated env without token or synced store
remaining_issues: []
ship_recommendation: ship
---END-POLISH-RESULT---
```

The scorecard reads 91 under polish versus 93 under shipcheck because polish scored without the run research dir, so the path_validity and auth_protocol dimensions were unverified.
