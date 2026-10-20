# n8n-pp-cli shipcheck

## Run 1 (exit 3)
- dogfood FAIL: dead helper printStampedDryRunOutput (generator-emitted, unused); novel host check false positives ("settings.errorWorkflow" diff label; generated root `audit` pulled in by novel `credentials audit` leaf collision -> "audit.generate").
- verify PASS, validate-narrative PASS, verify-skill PASS, scorecard 95/100 A.

## Fixes
- Removed dead generated helper (retro note filed).
- Diff label errorWorkflow (no dotted literal).
- Renamed novel `credentials audit` -> `credentials usage` (same behavior) to avoid the leaf collision with generated `audit`.

## Run 3 (exit 0)
  Total: 96/100 - Grade A
  verify            PASS    0         22.877s
  validate-narrative  PASS    0         157ms
  dogfood           PASS    0         5.138s
  workflow-verify   PASS    0         13ms
  apify-audit       PASS    0         29ms
  verify-skill      PASS    0         6.811s
  scorecard         PASS    0         1.697s

## Behavioral samples (local n8n 2.44.4, PRIVATE TEST data)
- executions failures: 3 failures grouped to (Webhook Fails, Boom) with message; second run served from cache.
- workflows uses: credential/subworkflow/variable/webhook selectors each return the expected single node; non-match -> [].
- workflows stale: never_ran / no_success_in_window / no_error_workflow reasons correct.
- credentials usage: unused credential reported; dangling [].
- executions wait: exit 0 success, 1 error (failed_node Boom), workflow mode picks new run.
- workflows diff: version and --against-id modes; identical -> changed false.
- workflows patch/export/push: preview default, --apply writes, idempotent re-export/push.

Verdict: ship
