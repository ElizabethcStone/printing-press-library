Acceptance Report: airtable
  Level: Full Dogfood (read-only matrix, plus one operator-approved sandbox write lifecycle)
  Tests: 149/149 passed (237 skipped/unverified: mutating rows run dry-run only; synthetic-ID rows unverified-needs-access)
  Hollow coverage: none. records upsert-csv is proof-covered by upsert-csv-lifecycle.md (sandbox create, merge-update, delete, verified cleanup)
  Failures: none on the final run
  Fixes applied: 6
    - Novel dry-run branches now use writeDryRun, so --json --dry-run emits JSON (analytics, schema-diff, diff, sync-table, upsert-csv)
    - Added Examples to 26 generated endpoint commands via spec `example` (synthetic IDs)
    - Added response_path unwrap for bases, views, webhooks, and comments lists. Agents now get arrays, and the schema-diff companion ID resolves
    - search happy-args attached at runtime from the hooks file, which survives regen
    - upsert-csv: pp:happy-args with the synthetic fixture testdata/leads-sample.csv, plus pp:verified-by-proof
    - Phase 4.95 review fixes (CSV BOM and duplicate headers, empty rows and merge keys, parse errors, diff error handling, analytics scoping)
  Printing Press issues: see retro-notes.md (platform whoami collision, dead-helper emission, novel host check leaf over-match, regen-merge strips `var _` guards and leaves files unformatted, stale root.go headline on same-spec regen, phantom freshness paths in docs)
  Doc-audit warnings carried (generator boilerplate): phantom `bases get`/`bases search` freshness paths, exit code 1 missing from tables, `auth-status` wording, placeholder templates in AGENTS/SKILL
  Gate: PASS
