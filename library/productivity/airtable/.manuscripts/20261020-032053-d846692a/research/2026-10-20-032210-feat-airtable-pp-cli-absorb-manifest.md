# Airtable CLI Absorb Manifest (reprint 20261020-032053-d846692a)

## Absorbed (match or beat everything that exists)

| # | Feature | Best Source | Our Implementation | Added Value |
|---|---------|-----------|-------------------|-------------|
| 1 | List/query records (filter/sort/view/fields/pagination) | domdomegg/airtable-mcp-server `list-records` | (generated endpoint) records list | Offline cache, --json/--select, SQL composable |
| 2 | Get single record | domdomegg `get-record` | (generated endpoint) records get | Agent-native output |
| 3 | Create records (batch+single) | domdomegg `create-record` | (generated endpoint) records create | --dry-run, --stdin batch |
| 4 | Update/upsert records (PATCH/PUT, performUpsert.fieldsToMergeOn) | domdomegg `update-records` | (generated endpoint) records update | True upsert-by-merge-field |
| 5 | Delete records (batch) | domdomegg `delete-records` | (generated endpoint) records delete | --dry-run preview |
| 6 | List bases | domdomegg `list-bases` | (generated endpoint) bases list | Local cache |
| 7 | Get base schema (tables+fields+views) | domdomegg `describe-table`/`list-tables` | (generated endpoint) bases get-schema | Offline schema snapshot |
| 8 | Create/update table | domdomegg `create-table`/`update-table` | (generated endpoint) tables create / tables update | Scriptable |
| 9 | Create/update field | domdomegg `create-field`/`update-field` | (generated endpoint) fields create / fields update | Scriptable |
| 10 | List/create/update/delete comments (@mention) | domdomegg `list-comments`/`create-comment` | (generated endpoint) comments list/create/update/delete | Agent-native |
| 11 | Upload attachment | domdomegg `upload-attachment` | (generated endpoint) attachments upload | Simple CLI upload |
| 12 | List/create/delete/refresh/enable-disable webhooks, list payloads | official docs (webhooks-overview) | (generated endpoint) webhooks list / webhooks create / webhooks delete / webhooks refresh / webhooks enable-disable / webhooks list-payloads | Polling helper |
| 13 | List/get/delete views | official docs | (generated endpoint) views list / views get / views delete | Offline cache |
| 14 | Get user info/token scopes (whoami) | official docs get-user-id-scopes | (generated endpoint) user get | Used by `doctor` |
| 15 | Get base collaborators | official docs | (generated endpoint) bases get-collaborators | Offline cache |
| 16 | TSV/CSV-first output for agents | arnoldadlv/airtable-cli | (behavior in airtable-pp-cli records list) --csv flag | Framework convention, already agent-native |
| 17 | Type/schema export from base schema | airtable-types-gen (npm) | (behavior in airtable-pp-cli bases get-schema) --json structured schema export | Offline, no separate tool needed |

| 18 | Hosted MCP search/analyze + create/update records (OAuth or PAT) | Airtable official MCP (mcp.airtable.com/mcp) + @airtable/mcp-cli | (behavior in airtable-pp-cli agent-context) companion MCP binary airtable-pp-mcp (stdio+http, code orchestration) over the full Web API surface | Adds delete, comments, webhooks, attachments, offline store; stable tool names |
| 19 | Per-table record mirror into local SQLite (dynamic schema, keyed base+table) | prior airtable-pp-cli hand-code; airtable-to-sqlite (PyPI) | airtable-pp-cli records sync-table | Feeds search, --expand-links, analytics; honors rate limits |

## Transcendence (only possible with our approach)

| # | Feature | Command | Score | Buildability | Why Only We Can Do This | Long Description |
|---|---------|---------|-------|--------------|--------------------------|-------------------|
| 1 | Merge-field upsert from CSV | records upsert-csv | 9/10 | hand-code | Maps CSV headers to fields and batches ≤10 rows into `performUpsert.fieldsToMergeOn`; no Airtable tool goes CSV→business-key upsert | Use this command to merge-upsert CSV rows by business fields. Do NOT use it to preview field-level changes to a proposed record payload; use `records diff` instead. |
| 2 | Cross-base full-text search | search | 9/10 | spec-emits | Framework FTS over flattened `fields` JSON of every synced record across bases; the API has no cross-base search | none |
| 3 | Local diff before bulk write | records diff | 8/10 | hand-code | Fetches current values for target record IDs and diffs field-by-field against the proposed payload before any write | Use this command to inspect field-level changes before a bulk write. Do NOT use it to apply CSV rows by a business-key upsert; use `records upsert-csv` instead. |
| 4 | Schema drift detection | bases schema-diff | 8/10 | hand-code | Diffs a stored local schema snapshot against a fresh Metadata API pull, field-by-field | none |
| 5 | Linked-record expansion | records list --expand-links | 7/10 | hand-code | Resolves `rec…` IDs to primary-field values from records already in the local mirror, no extra calls | none |

### Dropped prior features (reprint verdicts — override at the gate if wanted)
| Prior feature | Verdict | Reason |
|---|---|---|
| Field-value distribution (`analytics`) | drop as novel row | No evidence of weekly demand. The framework `analytics --type records --group-by` command still ships automatically. |
| Sync staleness report (`bases sync-status`) | drop | Supporting only. The accepted cache-freshness enrichment plus the generated `doctor` cache report cover freshness visibility. |
| Linked-record expansion | reframe | Kept as the `--expand-links` flag on `records list`/`records get`. |

> 2026-10-20 (Elizabeth): row 5 Linked-record expansion stays built as `records list/get --expand-links`, but it is removed from research.json novel_features (Highlights/Unique Features). The press novel-host check false-positives on the generic `list` leaf.
