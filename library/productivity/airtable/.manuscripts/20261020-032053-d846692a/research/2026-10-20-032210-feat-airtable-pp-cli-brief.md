# Airtable CLI Brief

## API Identity
- Domain: Airtable is a low-code relational database / spreadsheet-database hybrid. A **base** contains **tables**; tables contain typed **fields** (columns) and **records** (rows, dynamic JSON keyed by field name/id). Bases also have **views**, **webhooks**, **comments** (per-record), and **collaborators**.
- Users: ops/product teams running internal tools on Airtable, automation builders who currently use Zapier/Make/n8n, developers syncing external systems into/out of Airtable, agents doing bulk data entry or querying.
- Data profile: hierarchical (workspace -> base -> table -> field/record), schema is per-base and fetched dynamically via the Metadata API (`GET /v0/meta/bases/{baseId}/tables`). No universal fixed schema — this is the central design fact for the local store (see Data Layer below).

## Users (sourced, 2026-10-20 user research)
1. **Automation builders hitting the no-upsert wall.** Airtable community threads say that without a native upsert "you have to write a custom script every single time" (community.airtable.com/t/automation-upsert/41234). Make's Upsert module matches only on Airtable record ID, so users with their own keys run a Search Records step first (community.airtable.com/automations-8/bigquery-import-and-upsert-in-airtable-table-23049). CSV push is described as "requires code and it's not simple" (community.airtable.com/t5/automations/connect-csv-to-airtable/td-p/139874). *Ritual:* a recurring (nightly or weekly) CSV/warehouse export pushed into one table, keyed on a business field such as email or SKU.
2. **Developers and ops staff exporting readable data.** The API returns linked-record fields as bare `rec…` IDs. Airtable staff confirmed that a separate request per linked table is required, and users work around it with formula or rollup fields (community.airtable.com/development-apis-11/record-id-showing-instead-of-value-3403, .../retrieving-linked-records-via-api-7154). *Ritual:* periodic status or report exports where linked Carrier/Customer/Owner names must be human-readable.
3. **Data/integration engineers mirroring bases into warehouses.** Airbyte's Airtable connector docs say renames stop syncing because "Airtable offers no way to notify" downstream of renamed tables. Its guides say field renames fail syncs or silently drop columns, and they recommend freshness checks on the destination (docs.airbyte.com/integrations/sources/airtable). Airtable support recommends keying on field IDs with the schema endpoint as the ID→name map (support.airtable.com/docs/understanding-airtable-ids). The `airtable-to-sqlite` PyPI tool exists for local SQLite copies. *Ritual:* scheduled base→warehouse/SQLite mirror runs, plus a pre-deploy check that the schema hasn't moved.
4. **Agent operators running AI assistants against bases.** Airtable's official hosted MCP (Feb 2026) and `@airtable/mcp-cli` (`AIRTABLE_TOKEN`) let agents search and create/update records, but tool shapes "may change between server releases". There is no delete, comments, webhooks, or attachments, and no offline store (support.airtable.com/articles/9897799762-using-the-airtable-mcp-server). *Ritual:* recurring agent bulk-edit or cleanup passes across several tables, where an operator wants to preview before authorizing writes.
5. **Everyone doing bulk pulls under the rate limit.** 5 req/s per base, 50 req/s per token, and a 30s penalty on 429 (airtable.com/developers/web/api/rate-limits). Repeated full-table pulls for reporting are slow and hit 429s, which pushes users toward a local cache that is refreshed on a schedule.

## Reachability Risk
- None. Official, fully documented, versioned REST API (`https://api.airtable.com/v0`). Stable since 2018; no reverse-engineering required. Auth via Bearer Personal Access Token, confirmed against the most popular open-source Airtable MCP server's source (`domdomegg/airtable-mcp-server`, `AIRTABLE_API_KEY` env var, `Authorization: Bearer <token>` header, `https://api.airtable.com` base + `https://content.airtable.com` for attachment upload).
- Probe-safe endpoint: `GET /v0/meta/whoami` (get-user-id-scopes) — safe, read-only, returns user id + token scopes, ideal for `doctor`.

## Top Workflows
1. **Query/filter/sort records** across a table with formula filtering, view scoping, field selection, and pagination — the single most common Airtable API use case (ops dashboards, reporting, syncing out).
2. **Bulk create/update/upsert records** — data sync and automation. `PATCH/PUT /v0/{baseId}/{tableIdOrName}` supports `performUpsert.fieldsToMergeOn` (up to 3 merge fields) for true upsert semantics — a genuinely powerful feature most simple wrappers don't expose well.
3. **Schema introspection and management** — list bases, get full base schema (tables + fields + views), create/update tables and fields programmatically. Needed by any integration/automation builder.
4. **Webhooks** — subscribe to base/table/view-scoped change notifications, poll for payloads, refresh/enable/disable. Airtable webhooks require polling a payloads endpoint (no direct push body beyond a ping), which is a real integration pain point worth smoothing over.
5. **Record comments** — list/create/update/delete threaded comments per record, with `@mention` support.

## Table Stakes
- Full CRUD on records including batch create/update/delete and upsert-by-field.
- Formula-based filtering (`filterByFormula`), sorting, view scoping, field projection (`fields[]`).
- Pagination via `offset` cursor, `pageSize`/`maxRecords` control.
- Schema management: list bases, get base schema, create/update tables, create/update fields.
- Webhooks: create, list, delete, refresh, enable/disable notifications, list payloads.
- Comments: list/create/update/delete on records.
- Attachment upload (base64 bytes, up to 5MB, separate `content.airtable.com` host).
- `typecast` support on writes (best-effort string->typed conversion).

## Data Layer
- Primary entities: `bases`, `tables` (nested per base, includes field schema), `records` (per-table, dynamic `fields` JSON blob + `id` + `createdTime`), `views`, `webhooks`, `comments`.
- Records are the hierarchical/dynamic-schema case the generator's `<parent>_<resource>` naming convention exists for: sync path is `/v0/{baseId}/{tableIdOrName}` (records) nested under a resolved `baseId`+`tableIdOrName`, so local storage should key by `resource_type = 'base_table_records'` (or equivalent) with `base_id`/`table_id` columns, not a bare `records` type — a single Airtable account can have records from many bases/tables in the same local mirror.
- Sync cursor: records have no `updatedTime` on the base record envelope (only `createdTime`); true incremental sync isn't offered by the core API for arbitrary tables (webhooks are the actual "what changed" mechanism). Local sync should be full-refresh per table by default, using cache TTL, with webhooks as the freshness-triggering enrichment for CLIs that opt in.
- FTS/search: index `fields` JSON blob (flattened key:value text) per record for full-text search across arbitrary Airtable schemas — this is Airtable-idiomatic since field names are user-defined per base and unknowable in advance.

## Codebase Intelligence
- Source: `domdomegg/airtable-mcp-server` (most-starred community Airtable MCP server; tools: `list-bases`, `list-tables`, `describe-table`, `list-records`, `search-records`, `get-record`, `create-record`, `update-records`, `delete-records`, `create-field`, `update-field`, `create-table`, `update-table`, `list-comments`, `create-comment`, `upload-attachment`).
- Auth: `Authorization: Bearer <token>`, env var `AIRTABLE_API_KEY`, base URL `https://api.airtable.com`; attachment upload uses a distinct host `https://content.airtable.com`.
- Data model: confirms bases -> tables -> fields/records hierarchy; `describe-table` tool mirrors the Metadata API schema shape directly.
- Architecture: no rate-limiting/retry abstraction of its own beyond basic error enhancement (`enhanceAirtableError`) — our generated client should own backoff (5 req/s per base, 50 req/s per token, 429 -> wait 30s per Airtable's documented policy).

## Re-validation (2026-10-20, press 4.31.2 -> 4.33.8)
- Reachability: `GET /v0/meta/whoami` returns 200 with the house PAT. Bearer auth, `AIRTABLE_API_KEY`, and `https://api.airtable.com` are unchanged. The prior internal spec is byte-identical to the vendored spec.yaml.
- Ecosystem change: Airtable shipped an official hosted MCP server (`mcp.airtable.com/mcp`, OAuth or PAT, all plans, Feb 2026). It covers search, analyze, and create/update records, plus Interface data. It omits record delete, comments, attachment upload, and webhooks, and it has no offline store, SQL, cross-base FTS, diff-before-write, or schema-drift detection. Our thesis still holds; the main incumbent is now the official MCP, not `@airtable/mcp-cli`.
- Machine deltas worth exercising: code-orchestration write confirmation and the MCP read-only switch (MCP surface), credential binding to server/file and plaintext refusal (auth), redirect/private-dial guards and bounded bodies (transport), and sync fixes (first-request offset, narrowed-prune skip, tenant checkpoints, dependent sync naming).

## User Vision
- Reprint run via /printing-press-reprint with Codex mode: delegate code-writing to Codex.
- Accepted enrichment: MCP remote transport (stdio+http) and an MCP surface decision (prior score: transport 5, tool_design 5, surface_strategy unscored, 32 auth-required tools). Sync/cache enrichment (data_pipeline_integrity 7, cache_freshness 5).
- Upstream target: mvanhorn/printing-press-library. No private base IDs, workspace names, or house brand references. Live tests must be read-only and isolated from the house cache.

## Product Thesis
- Name: **Airtable CLI** (canonical display name: "Airtable")
- Why it should exist: wrappers (pyairtable, airtable.js) are libraries. Airtable's official MCP is a hosted, online-only tool surface with no delete/comments/webhooks/attachments, no offline store, and no bulk-diff. A local SQLite mirror with cross-base FTS, merge-field CSV upsert, diff-before-write, and schema-drift detection is still unique. All of it is exposed agent-natively as a CLI and as a local/remote MCP server.

## Build Priorities
1. Data layer for records (dynamic schema keyed per base+table), base schema, and the sync/search/SQL path, with correct freshness metadata.
2. Absorb full CRUD, batch, upsert, schema management, views, webhooks, comments, and attachments.
3. Transcend: re-reconcile the 7 prior novel features (upsert-csv, records diff, cross-base search, schema-diff, expand-links, analytics, sync-status).
4. MCP: stdio+http transport, plus intents for the multi-step workflows (sync->search, diff->upsert).

## Reachability Gate
- Decision: PASS
- Evidence: GET /v0/meta/whoami -> 401 AUTHENTICATION_REQUIRED unauthenticated; 200 with PAT (2026-10-20).
