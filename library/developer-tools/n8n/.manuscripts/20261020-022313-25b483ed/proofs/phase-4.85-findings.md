# Phase 4.85 output review — n8n-pp-cli
status: WARN (warnings only)

1. format-bugs (warning): `workflows diff` not-found error exposed an internal path ("base side not found: /workflows/<id>"). Action: fix to "workflow <id> not found on <side>".
2. coverage (warning): reviewer samples ran against an empty local store. Mitigation: behavioral samples on seeded PRIVATE TEST data are recorded in the shipcheck proof (failures grouping, uses selectors, stale reasons, credentials usage, wait exit codes, diff modes, patch/export/push).
