# airtable-pp-cli shipcheck (reprint on press 4.33.8)

## Umbrella results
| Loop | verify | validate-narrative | dogfood | workflow-verify | apify-audit | verify-skill | scorecard | Total |
|---|---|---|---|---|---|---|---|---|
| 1 | PASS | FAIL (stale staged binary) | FAIL (dead helper) | PASS | PASS | PASS | PASS | 93 |
| 2 | PASS | PASS | FAIL (novel host false positive) | PASS | PASS | PASS | PASS | 94 |
| 3 | PASS | PASS | PASS | PASS | PASS | PASS | PASS | **94/100 (A)** |

Prior CLI (press 4.31.2) scored 90. MCP remote transport went from 5 to 10, tool design from 5 to 10, surface strategy from unscored to 10, and cache freshness from 5 to 10. Data pipeline integrity is still 7 because search uses the generic store search.

## Fixes applied
- Rebuilt the stale `build/stage/bin` binaries. They predated Phase 3, which caused the narrative failures on `sync-table` and `--expand-links`.
- Removed the dead generated `printStampedDryRunOutput` from helpers.go. The generator emits it unconditionally, but only store-less mutation commands call it. Patch recorded; retro filed.
- Novel host check false positive on `records list`: with Elizabeth's approval, unlisted `--expand-links` from novel_features. The feature is still built and documented. Retro filed.
- Phase 3 real bug: `bases schema-diff` read through the HTTP response cache and hid live drift. It now uses GetNoCache.

## Behavioral samples (live Airtable, read-only, isolated XDG homes)
- `records sync-table` on a 3-record table: 3 fetched, 3 synced, 0 extract failures.
- `records list`: 3 records returned.
- `search <word from record>` with `--type records`: 1 hit.
- `records diff`: an identical payload gives `changes: []`, and three modified string fields give 3 changed fields.
- `bases schema-diff`: the first run records the baseline with a note, and the second reports `no_changes_detected: true`.
- `records upsert-csv --dry-run`: previews the batch plan and sends no write.
- `analytics --type records --group-by fields.Name`: 3 records in 2 groups. Note that the store is credential-scoped (`data-<hash>.db`), so local reads need the same token the sync used.

## Verdict: ship
