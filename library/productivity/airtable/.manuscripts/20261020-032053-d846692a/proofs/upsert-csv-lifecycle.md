# records upsert-csv write lifecycle (sandbox)

Re-run 2026-10-20 on the post-review build (PR #2346 commit 526b31ac), which validates in dry-run and sends typecast by default.

Sandbox: a disposable content-template table in the operator's Airtable account, chosen by the operator. Base, table, and record IDs are redacted. The fixture is a 1-row CSV keyed on the primary field `Title` with a unique `PRIVATE TEST pp-dogfood <timestamp>` value, plus a text column and a date column (`Publish Date`) to exercise typecast. The run used isolated XDG homes with AIRTABLE_NO_LEARN=true.

```
$ airtable-pp-cli records upsert-csv --base <sandbox-base> --table <sandbox-table> --file fixture.csv --merge-on Title --dry-run --json
exit=0
{"dry_run":true,"action":"records upsert-csv","rows":1,"batches":1,"merge_on":"Title","sample":[{"fields":{"Publish Date":"2026-10-20","Results (shares, click, sales, views etc.)":"run-1","Title":"PRIVATE TEST pp-dogfood 20261020121338"}}]}

$ airtable-pp-cli records upsert-csv --base <sandbox-base> --table <sandbox-table> --file fixture.csv --merge-on Title --json
exit=0
{"merge_on":"Title","records_created":1,"records_updated":0,"rows_processed":1}

$ airtable-pp-cli records upsert-csv --base <sandbox-base> --table <sandbox-table> --file fixture2.csv --merge-on Title --json
exit=0
{"merge_on":"Title","records_created":0,"records_updated":1,"rows_processed":1}

$ airtable-pp-cli records list --base <sandbox-base> --table <sandbox-table> --filter-formula {Title}='PRIVATE TEST pp-dogfood 20261020121338' --json
exit=0
{"meta":{"source":"live"},"results":[{"id":"<test-record>","createdTime":"2026-10-20T19:13:39.000Z","fields":{"Results (shares, click, sales, views etc.)":"run-2","Title":"PRIVATE TEST pp-dogfood 20261020121338","Publish Date":"2026-10-21"}}]}

matching rows after two upserts: 1
$ airtable-pp-cli records delete <test-record> --base <sandbox-base> --table <sandbox-table> --yes --json
exit=0
{"action":"delete","data":{"deleted":true,"id":"<test-record>"},"path":"/v0/<sandbox-base>/<sandbox-table>/<test-record>","resource":"records","status":200,"success":true}

$ airtable-pp-cli records list --base <sandbox-base> --table <sandbox-table> --filter-formula {Title}='PRIVATE TEST pp-dogfood 20261020121338' --json
exit=0
{"meta":{"source":"live"},"results":[]}

table rows before=18 after=18
```

Result:
- The dry run validated the CSV and returned the plan (1 row, 1 batch, a sample) with no write.
- The first upsert created 1 row.
- The second upsert with the same merge key updated it: records_updated=1 and exactly 1 matching row, so no duplicate.
- The typecast date string was stored as a date and updated from 2026-10-20 to 2026-10-21.

Cleanup: the delete removed only the created test row, by ID (exit 0). A re-query returned 0 matching rows, and the table count was unchanged (18 before, 18 after).
