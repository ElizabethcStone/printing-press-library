Manifest transcendence rows: 6 planned, 6 built. Absorbed hand-code rows (patch, export, push) built. Completion gate: per-row Cobra resolution OK; dogfood novel_features_check 6/6.

## Slice 1 (foundation)
- internal/n8nwf: ParseWorkflow, UsageQuery, ExecutionFailure, Diff, SetNodeParam/SetNodeDisabled/RenameNode, UpdatableBody + table tests.
- N8N_URL alias (instance root -> /api/v1), patch record n8n-url-alias-means-instance-root.
- Spec overlay: list ops renamed (workflows list/get etc.), ApiKeyAuth only, localhost default, x-cache, x-learn.

## Slice 2 (local insight)
- workflows uses, workflows stale, credentials audit (Codex, verified on seeded local n8n). Built: 3/6.

## Slice 3 (live triage)
- executions failures (+ n8n_execution_failures cache), executions wait (exit 0/1/6), workflows diff (version/config/id modes). Codex; fixed: test buffer merge, inverted diff test fixture, cached GET in wait/diff -> GetNoCache. Built: 6/6.

## Slice 4 (write paths)
- workflows patch/export/push (Codex; marker skipped because sandbox blocked httptest binds; tests pass outside sandbox; live-verified on local n8n: preview default, --apply writes, idempotent re-export/push).
