Manifest transcendence rows: 5 planned (4 hand-code + 1 spec-emits), 5 built.

## Slice 1 (port)
- Copied prior hand-authored files (records_upsert_csv, records_diff, records_expand_links, bases_schema_diff, records_sync_table, analytics + tests, store/extras.go) from the vendored library.
- Delegated compile fix, hook registration, expand-links wiring, and acceptance tests to Codex (workspace-write sandbox; --yolo was denied by the permission classifier).
- analytics ported (not a novel row): the gate showcase wrongly said the framework emits it.

## Slice 1 result
- Codex: classifyAPIError signature fixes, airtable_novel_hooks.go (records sync-table, analytics via registerNovelCommand), --expand-links on records list/get (minimal generated-file edit), novel_acceptance_test.go. Codex's sandbox blocked loopback listeners, so Claude ran the tests.
- Claude fixes after review: bases schema-diff now uses GetNoCache (the cached GET hid live drift; real bug found by the acceptance test); test stub routing and pretty-print-tolerant assertions; pp:data-source annotations added to cmd.Annotations; the hook guards root.Find.
- Gate: all approved paths resolve (records upsert-csv, search, records diff, bases schema-diff, records list/get --expand-links, records sync-table, analytics, user). dogfood novel_features_check planned=5 found=5.
- go build, go vet, and go test -count=1 ./... are green.

## Deferred / known
- Generated-file hand-edit: records_list.go and records_get.go carry --expand-links. Needs a .printing-press-patches record at promote.
- Generator limitations (retro): platform whoami collision; Codex delegation reference assumes --yolo.
- Dropped prior novel: bases sync-status (prior file not ported).
