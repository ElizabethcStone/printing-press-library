## Customer model

### Nightly CSV operator

**Today (without this CLI):** They export a CSV or warehouse file and push it into Airtable. When their business key is something like email or SKU, Make’s upsert module cannot match on it, so they add a search step or rerun a custom script.

**Weekly ritual:** They refresh a target table from an external system, matching rows by a business field so existing records are updated instead of duplicated.

**Frustration:** Each new CSV-to-upsert flow takes custom mapping and batching work.

### Readable-report operator

**Today (without this CLI):** They query a table for a status report, but linked Carrier, Customer, or Owner fields arrive as `rec…` IDs. They make separate requests for linked tables or maintain formula and rollup fields to get readable names.

**Weekly ritual:** They pull operational records and prepare a report whose linked fields make sense to people.

**Frustration:** Turning linked-record IDs into useful names adds repeated lookups or maintenance inside Airtable.

### Base-mirror engineer

**Today (without this CLI):** They mirror Airtable bases into a warehouse or SQLite, then check whether table and field names still match what their pipeline expects. A rename can stop syncs or silently drop columns; field IDs and schema snapshots help them diagnose it.

**Weekly ritual:** They refresh a base mirror and check schema stability before deploying dependent pipeline changes.

**Frustration:** Airtable does not notify downstream consumers when table or field names change.

### Agent-workflow operator

**Today (without this CLI):** They run assistant-driven bulk edits or cleanup across tables and want a preview before writes. Hosted MCP tools support search and create/update, but the brief identifies no offline store or diff-before-write workflow there.

**Weekly ritual:** They review a proposed bulk change, then authorize the agent to apply it across relevant records.

**Frustration:** A row count or generic dry-run does not show which fields on existing records would change.

## Candidates (pre-cut)

1. **Merge-field upsert from CSV** — `records upsert-csv`: Map CSV headers to Airtable fields and batch merge-upsert rows using a business key. **Persona:** Nightly CSV operator. **Source:** (a) Persona-driven; (d) Reprint `prior-keep`. **Long Description:** Use this command to merge-upsert CSV rows by business fields. Do NOT use it to preview field-level changes to a proposed record payload; use `records diff` instead. **Rubric:** Keep candidate; no LLM or external service, uses documented merge-upsert semantics, and is independently verifiable with a controlled table.

2. **Local diff before bulk write** — `records diff`: Show field-by-field changes between proposed payloads and current Airtable records before writing. **Persona:** Agent-workflow operator. **Source:** (a) Persona-driven; (d) Reprint `prior-keep`; (e) User Vision. **Long Description:** Use this command to inspect field-level changes before a bulk write. Do NOT use it to apply CSV rows by a business-key upsert; use `records upsert-csv` instead. **Rubric:** Keep candidate; no LLM or external service, reads live records and compares supplied changes, and output can be checked against those records.

3. **Cross-base full-text search** — `search`: Search all locally synced bases and tables through one index over dynamic Airtable fields. **Persona:** Readable-report operator; Agent-workflow operator. **Source:** (b) Service-specific content patterns; (c) Cross-entity local queries; (d) Reprint `prior-keep`. **Long Description:** none. **Rubric:** Keep candidate; local FTS addresses a missing cross-base API query, with no additional service or inferred semantics.

4. **Schema drift detection** — `bases schema-diff`: Compare a fresh base schema with the last stored schema snapshot and report field-level changes. **Persona:** Base-mirror engineer. **Source:** (a) Persona-driven; (d) Reprint `prior-keep`. **Long Description:** none. **Rubric:** Keep candidate; schema changes are directly verifiable against the two snapshots and answer an explicit downstream-sync pain.

5. **Linked-record expansion** — `records list --expand-links`: Resolve linked-record IDs to display values using the local mirror. **Persona:** Readable-report operator. **Source:** (a) Persona-driven; (b) Service-specific content patterns; (d) Reprint `prior-reframe`. **Long Description:** none. **Rubric:** Keep candidate; Airtable’s linked-record IDs make the service-specific need clear, and local rows provide the display values without extra API calls.

6. **Field-value distribution** — `analytics --type records --group-by fields.Status`: Count synced records by a field value for a table-level breakdown. **Persona:** Readable-report operator. **Source:** (c) Cross-entity local queries; (d) Reprint `prior-keep`. **Long Description:** none. **Rubric:** Keep candidate for cut review; it is mechanical and verifiable, but the brief does not establish weekly demand for status breakdowns.

7. **Sync staleness report** — `bases sync-status`: Show local sync times and counts for each base/table. **Persona:** Base-mirror engineer; Agent-workflow operator. **Source:** (d) Reprint `prior-keep`; (e) User Vision. **Long Description:** none. **Rubric:** Keep candidate for cut review; it uses local metadata and is verifiable, but freshness visibility alone may be too supporting to justify a distinct transcendence row.

8. **Readable linked-record export** — `records export-readable`: Export selected records with locally resolved linked-record display values. **Persona:** Readable-report operator. **Source:** (a) Persona-driven; (b) Service-specific content patterns. **Long Description:** Use this command to export rows with linked-record display values resolved. Do NOT use it to inspect linked values in a single table listing; use `records list --expand-links` instead. **Rubric:** Reframe or cut; relies on the same local link expansion as candidate 5 and duplicates its core job.

9. **CSV header preflight** — `records validate-csv`: Compare CSV headers against a table schema and report unknown columns before an upsert. **Persona:** Nightly CSV operator. **Source:** (a) Persona-driven; (e) User Vision. **Long Description:** Use this command to check CSV headers against a table before preparing an upsert. Do NOT use it to apply CSV rows by a business key; use `records upsert-csv` instead. **Rubric:** Reframe or cut; the feature is verifiable, but it overlaps the CSV upsert’s mapping and dry-run path.

10. **Webhook payload drain** — `webhooks drain`: Poll webhook payloads repeatedly and return the collected changes for downstream processing. **Persona:** Base-mirror engineer. **Source:** (b) Service-specific content patterns. **Long Description:** none. **Rubric:** Cut candidate; useful workflow is plausible, but a continuous poller approaches a persistent process and exceeds a bounded one-command scope.

11. **Local linked-table join report** — `records join`: Join synced records across tables to produce a report of related values. **Persona:** Readable-report operator. **Source:** (c) Cross-entity local queries. **Long Description:** Use this command for reports that combine fields from multiple linked tables. Do NOT use it to resolve linked values inline in a single table’s rows; use `records list --expand-links` instead. **Rubric:** Reframe or cut; a general join surface is broader than the explicit reporting pain, while linked-value expansion covers the evidenced use.

12. **MCP sync-search intent** — `mcp workflow sync-search`: Sync selected tables, then search the refreshed local data in one agent-invocable workflow. **Persona:** Agent-workflow operator. **Source:** (e) User Vision; (f) Codebase Intelligence. **Long Description:** none. **Rubric:** Cut candidate; the workflow uses existing sync and search surfaces but bundles them without a distinct result or demonstrated weekly need.

## Survivors and kills

### Survivors

| # | Feature | Command | Score | Buildability | Persona-served | Weekly use | Wrapper vs leverage | Transcendence proof | Closest killed sibling | How It Works | Evidence | Long Description |
|---|---------|---------|-------|--------------|----------------|------------|---------------------|----------------------|-----------------------|--------------|----------|-------------------|
| 1 | Merge-field upsert from CSV | `records upsert-csv` | 9/10 | hand-code | Nightly CSV operator | Yes: recurring weekly or nightly imports | Not a thin endpoint wrapper: maps a file and batches it into a business-key merge-upsert | Agent-shaped file ingestion plus documented upsert semantics | CSV header preflight; cut because header checking overlaps the upsert’s mapping and preview path | Uses the records API’s `performUpsert.fieldsToMergeOn` to map CSV rows, batch writes, and report the result without external dependencies. | Airtable community reports the no-upsert wall, custom-script burden, and difficult CSV push; the brief documents native merge-upsert fields and the recurring CSV ritual. | Use this command to merge-upsert CSV rows by business fields. Do NOT use it to preview field-level changes to a proposed record payload; use `records diff` instead. |
| 2 | Local diff before bulk write | `records diff` | 8/10 | hand-code | Agent-workflow operator | Yes: before each recurring agent bulk-edit or cleanup pass | Not a thin endpoint wrapper: reads current values and compares them against proposed changes | Field-level live comparison gives an agent operator a verifiable preview | MCP sync-search intent; cut because it merely bundles existing commands without a distinct result | Uses the records API to fetch current field values for supplied record IDs and compares them field-by-field to the proposed payload without external dependencies. | The brief identifies recurring agent bulk edits with previews as a workflow and notes that the official MCP has no offline store or bulk diff. | Use this command to inspect field-level changes before a bulk write. Do NOT use it to apply CSV rows by a business-key upsert; use `records upsert-csv` instead. |
| 3 | Cross-base full-text search | `search` | 9/10 | hand-code | Readable-report operator; Agent-workflow operator | Yes: recurring reporting and cleanup searches | Not a thin endpoint wrapper: searches local data across bases and tables in one query | Local FTS over dynamic record fields enables a query the Airtable API does not provide | Field-value distribution; cut because its reporting breakdown need lacks established weekly demand | Uses synced records in local SQLite to search flattened field JSON across bases and tables without external dependencies. | The brief says Airtable schemas are user-defined and notes the official MCP has no offline store or cross-base FTS. | none |
| 4 | Schema drift detection | `bases schema-diff` | 8/10 | hand-code | Base-mirror engineer | Yes: before recurring pipeline deployments and mirror runs | Not a thin endpoint wrapper: compares current schema with a stored snapshot field-by-field | Combines live schema metadata with local history to expose changes across time | Sync staleness report; cut because metadata freshness alone is supporting functionality, not the core pipeline breakage | Uses the Metadata API’s base schema and a locally stored prior schema snapshot to report field changes without external dependencies. | The brief cites Airbyte sync failures on renames, Airtable’s field-ID guidance, and the recurring pre-deploy schema check. | none |
| 5 | Linked-record expansion | `records list --expand-links` | 7/10 | hand-code | Readable-report operator | Yes: whenever they prepare recurring reports with linked fields | Not a thin endpoint wrapper: resolves IDs from already synced related records | Local relational data turns Airtable’s opaque linked-record IDs into readable report values without extra calls | Local linked-table join report; cut because a general join surface goes beyond the demonstrated display-value need | Uses locally synced linked-table records to resolve linked-record IDs to display values while listing rows, without external dependencies. | Airtable users receive linked-record IDs and otherwise make separate linked-table requests or maintain formula/rollup fields; the brief describes periodic readable status exports. | none |

### Killed candidates

| Feature | Kill reason | Closest-surviving-sibling |
|---|---|---|
| Field-value distribution | Monthly or occasional reporting is not established as a weekly ritual, so a generic offline aggregation does not meet the weekly-use bar. | `search` |
| Sync staleness report | It supports trusting the mirror but does not itself solve the engineer’s recurring schema-rename failures or offer enough standalone leverage. | `bases schema-diff` |
| Readable linked-record export | It duplicates linked-record expansion and adds a separate export surface without evidence that a new command is needed. | `records list --expand-links` |
| CSV header preflight | Its schema/header check is a subset of the CSV upsert preparation path and does not merit another command. | `records upsert-csv` |
| Webhook payload drain | Repeated polling veers into a background integration process, exceeding the bounded command scope. | `search` |
| Local linked-table join report | A general join command is scope expansion beyond the supported need to resolve linked values for readable reports. | `records list --expand-links` |
| MCP sync-search intent | It merely sequences existing sync and search commands, with no distinct weekly benefit established by the brief. | `search` |

## Reprint verdicts

| Prior feature | Verdict | Justification |
|---|---|---|
| Merge-field upsert from CSV (`records upsert-csv`) | Keep | Recurring keyed CSV imports directly match the automation-builder persona and use documented merge-upsert support. |
| Local diff before bulk write (`records diff`) | Keep | Field-level previews directly serve recurring agent bulk-edit workflows. |
| Cross-base full-text search (`search`) | Keep | Local FTS answers cross-base searches that the Airtable API does not expose. |
| Schema drift detection (`bases schema-diff`) | Keep | The base-mirror engineer has explicit recurring schema checks, and renames can disrupt downstream syncs. |
| Linked-record expansion (`records list`) | Reframe | Keep the display-value resolution idea for readable reports, expressed as `records list --expand-links`. |
| Field-value distribution (`analytics`) | Drop | The brief does not establish weekly demand for generic field breakdowns, and the feature lacks the pain evidence of the other candidates. |
| Sync staleness report (`bases sync-status`) | Drop | Freshness metadata is useful support, but it does not independently resolve the engineer’s evidenced schema-drift problem. |