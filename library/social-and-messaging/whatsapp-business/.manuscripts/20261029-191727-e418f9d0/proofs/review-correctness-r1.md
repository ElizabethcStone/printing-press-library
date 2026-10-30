# Round 1 correctness review

Status: 1 independent finding, plus 1 duplicate provenance issue already merged by parent. Reviewed custom CLI, metamcp, captures, inspection and generated integration seams. No source changes, remote calls or credentials accessed.

## P2 — Cross-business captures can be compared as same scope

`internal/inspection/diff.go:226` compares only request arguments; `internal/captures/captures.go:65` stores those arguments as scope. The official accounts tool permits omitted business_id and returns the resolved business_id. Two captures with request scope `{}` but distinct resolved businesses pass the same-scope check and report complete additions/removals.

Reproduction: inserted two synthetic `whatsapp_biz_accounts` captures in a disposable SQLite store, both complete with scope `{}`; one response has business_id `synthetic-business-a` and account_a, the other `synthetic-business-b` and account_b. Ran staged CLI `ops changes --before before --after after --db <disposable-store> --agent`: exit 0; output complete:true, removed account_a, added account_b, and changed business_id. No API calls.

Fix: preserve or validate effective business selection from the recognized top-level response business_id for known business-scoped tools. Reject a pair whose effective business identifiers differ; do not harvest arbitrary nested IDs. Keep explicit request scope/filter equality and reject request/response scope conflicts. Add regression for same implicit business (allowed), changed implicit business (rejected), explicit business mismatch (rejected), and global businesses listing unaffected.

## Duplicate P2 — Negotiated protocol provenance

`internal/cli/whatsapp_commands.go:403` hardcodes catalog scope protocol2025-03-26 even though `internal/metamcp/client.go:70` adopts initialize's returned protocol. A server negotiating another version yields mislabeled capture evidence, defeating catalog protocol equality. Parent reports this already merged; expose actual negotiated protocol and capture it, with fixture regression.

No other actionable correctness finding found in reviewed paths. Reserved runtime/template issues excluded per phase17.
