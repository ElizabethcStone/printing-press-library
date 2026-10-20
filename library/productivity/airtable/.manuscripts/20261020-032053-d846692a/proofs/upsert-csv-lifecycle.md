# records upsert-csv write lifecycle (sandbox)

Sandbox: a disposable content-template table in the operator's Airtable account, chosen by the operator. The base and table IDs are redacted. The fixture is a 1-row CSV keyed on the primary field `Title` with a unique `PRIVATE TEST pp-dogfood <timestamp>` value. It runs under isolated XDG homes with AIRTABLE_NO_LEARN=true.

```
$ airtable-pp-cli records upsert-csv --base <sandbox-base> --table <sandbox-table> --file <tmp>/fixture.csv --merge-on Title --dry-run --json
exit=0
{"dry_run":true,"action":"records upsert-csv","would":"run records upsert-csv; no changes made"}

$ airtable-pp-cli records upsert-csv --base <sandbox-base> --table <sandbox-table> --file <tmp>/fixture.csv --merge-on Title --json
exit=0
{"merge_on":"Title","records_created":1,"records_updated":0,"rows_processed":1}

$ airtable-pp-cli records upsert-csv --base <sandbox-base> --table <sandbox-table> --file <tmp>/fixture.csv --merge-on Title --json
exit=0
{"merge_on":"Title","records_created":0,"records_updated":1,"rows_processed":1}

$ airtable-pp-cli records list --base <sandbox-base> --table <sandbox-table> --filter-formula {Title}='PRIVATE TEST pp-dogfood 20261020093113' --json
exit=0
{"meta":{"source":"live"},"results":[{"id":"<test-record>","createdTime":"2026-10-20T16:31:14.000Z","fields":{"Results (shares, click, sales, views etc.)":"run-2","Title":"PRIVATE TEST pp-dogfood 20261020093113"}}]}

matching rows after two upserts: 1
$ airtable-pp-cli records delete <test-record> --base <sandbox-base> --table <sandbox-table> --yes --json
exit=0
{"action":"delete","data":{"deleted":true,"id":"<test-record>"},"path":"/v0/<sandbox-base>/<sandbox-table>/<test-record>","resource":"records","status":200,"success":true}

$ airtable-pp-cli records list --base <sandbox-base> --table <sandbox-table> --filter-formula {Title}='PRIVATE TEST pp-dogfood 20261020093113' --json
exit=0
{"meta":{"source":"live"},"results":[]}

```

Result: the first upsert created 1 row. The second upsert with the same merge key updated it (records_updated=1), and exactly 1 matching row exists, so there was no duplicate.

Cleanup: deleted only the created test row by ID (exit 0). A re-query returns 0 matching rows, and the table's record count is back to its pre-test value of 18.

Addendum (after Greptile review on PR #2346): upsert-csv now sends `typecast: true` by default. The text-only sandbox fixture above is unaffected by this; typed-column conversion is covered by unit tests (TestGreptileUpsertCSVTypecast).
