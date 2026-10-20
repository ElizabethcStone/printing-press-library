Acceptance Report: n8n
  Level: Full Dogfood (live, --allow-destructive)
  Target: disposable local n8n 2.44.4 container, PRIVATE TEST owner and data; production untouched
  Tests: 506/506 passed (runner: 2026-10-20-092519-dogfood-results.json)
  Proof-covered: executions wait (executions-wait-lifecycle.md), workflows diff (workflows-diff-lifecycle.md)
  Failures: none
  Fixes applied: 4 (across 2 fix loops: run 1 242/365, run 2 506/507, run 3 506/506)
    - 120 write commands lacked help Examples: added x-pp-example per operation to the derived spec, regenerated, ported only the Example deltas (CLI fix, durable via spec)
    - Binary-response --dry-run JSON lacked action: stamped in shared handleBinaryResponseDelivery (patch binary-dry-run-action)
    - tail happy-path fixture polled an empty resource: repointed to workflows (patch tail-happy-args-workflows)
    - workflows test-runs list error probe: n8n returns 200 [] for unknown ids; opted out (patch test-runs-list-no-error-probe)
    - Coverage: executions failures opted into pp:live-happy-path (local cache write only); wait/diff proof-covered
  Printing Press issues: 3
    - Generator emits no Example when the spec lacks body/param examples (120 of n8n's write commands); help check then fails live dogfood
    - Binary-response dry-run path skips stampDryRunEnvelope, so action is missing
    - tail happy-args picks the first resource alphabetically, which is often empty
  Gate: PASS
