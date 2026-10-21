# Polish — n8n-pp-cli (mid-pipeline)

Scorecard 96 -> 96; verify 100% -> 100%; gosec (hand-authored) 3 -> 0; tools-audit 0 pending; go vet clean; dogfood PASS (6/6 novel); verify-skill 0 findings; pii-audit clean.

Fixes applied:
- workflows_export.go: export dir 0o750, files 0o600, narrow #nosec G304 on the user-chosen-path read
- which.go: gofmt after dogfood sync

Skipped:
- 31 gosec findings in generator-emitted files (Press retro candidates)
- gosec scan partial: pinned v2.26.1 fails on go1.27.x; gosec@latest reports export-data errors
- polish's own live checks ran without an API key (401s on wait/diff); full live dogfood with the key is the authoritative gate

ship_recommendation: ship
