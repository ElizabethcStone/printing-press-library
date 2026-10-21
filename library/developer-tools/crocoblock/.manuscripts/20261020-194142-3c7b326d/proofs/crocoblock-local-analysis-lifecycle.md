# Crocoblock local analysis live lifecycle proof

Proof date: 2026-10-21T03:42:51.316415+00:00.

All six state-dependent analytics ran on captures read from the authorized isolated WP Engine development laboratory. Data are intentionally synthetic lab records persisted in the live plugins, not SQLite-invented rows. Every SQLite capture used below was created by the real CLI sync. Previous fixture state is explicitly identified. The no-mail mu-plugin was read and its loaded must-use status verified before booking/appointment writes.

This proof supplements phase18. It does not replace the Printing Press-owned full dogfood matrix. Each annotated feature still requires the matrix help and dry-run checks. No phase5 acceptance marker was hand-authored.

Command paths covered:

- `crocoblock-pp-cli schema diff`
- `crocoblock-pp-cli cct reconcile`
- `crocoblock-pp-cli relations audit`
- `crocoblock-pp-cli forms field-usage`
- `crocoblock-pp-cli reservations overlaps`
- `crocoblock-pp-cli filters impact`

Execution directory for all verbatim commands: `<local-path>`. `python3 work/run-cli.py` invokes `<runstate>/printing-press-users-bobe-agents-skills-2-9dd36474/runs/20261020-194142-3c7b326d/working/crocoblock-pp-cli/crocoblock-pp-cli` with `--no-learn` and loads credentials from a private session; no credential value appears in this proof. `work/local-analysis-http.py` uses the same private authorized lab HTTP helper for disposable WordPress draft fixture setup/cleanup.

Raw transcript companion files preserve stdout, stderr, argv, command strings, timestamps, assertions and cleanup. The local orchestration scripts are `work/local-analysis-lifecycle.py` and `work/render-local-analysis-proof.py`.

Fixtures created in this proof were cleaned by their own IDs only. Form23 and record1 predate this proof and are intentionally retained for the parent matrix. Complete capture provenance does not establish live availability or global freshness.
## crocoblock-pp-cli schema diff — schema

Status: PASS. Raw outputs and exact argv: `crocoblock-local-analysis-schema.json`.

### schema_capture_before

```sh
CROCOBLOCK_DB=<local-path> python3 work/run-cli.py sync --resources forms --db <local-path> --full --strict --agent --timeout 90s
```

Exit code: `0`.

### schema_create_form

```sh
CROCOBLOCK_DB=<local-path> python3 work/run-cli.py forms create --data '{"title": "PP local schema pp-local-6d7175cf7aa7", "content": "<!-- wp:jet-forms/text-field {\"name\":\"pp_schema_lifecycle\"} /-->"}' --agent --timeout 90s
```

Exit code: `0`.

### schema_form_read

```sh
CROCOBLOCK_DB=<local-path> python3 work/run-cli.py forms get 36 --agent --timeout 90s
```

Exit code: `0`.

### schema_capture_after

```sh
CROCOBLOCK_DB=<local-path> python3 work/run-cli.py sync --resources forms --db <local-path> --full --strict --agent --timeout 90s
```

Exit code: `0`.

### schema_diff

```sh
CROCOBLOCK_DB=<local-path> python3 work/run-cli.py schema diff --before <local-path> --after <local-path> --resources forms --agent --timeout 90s
```

Exit code: `0`.

### schema_form_trash

```sh
CROCOBLOCK_DB=<local-path> python3 work/run-cli.py forms trash 36 --agent --timeout 90s
```

Exit code: `0`.

### schema_form_cleanup_read

```sh
CROCOBLOCK_DB=<local-path> python3 work/run-cli.py forms get 36 --agent --timeout 90s
```

Exit code: `0`.

Assertions:

```json
[
  {
    "name": "new_live_form_detected",
    "result": "PASS",
    "total_changes": 1,
    "fixture_id": "36"
  },
  {
    "name": "schema_form_trashed",
    "result": "PASS"
  }
]
```

Cleanup:

```json
[
  {
    "kind": "form",
    "id": "36",
    "result": "PASS"
  }
]
```


## crocoblock-pp-cli cct reconcile — cct

Status: PASS. Raw outputs and exact argv: `crocoblock-local-analysis-cct.json`.

### cct_create

```sh
CROCOBLOCK_DB=<local-path> python3 work/run-cli.py cct create pp_probe --data '{"pp_key": "pp-local-b254242ccf1b", "pp_label": "before", "pp_keep": "unchanged", "cct_status": "publish"}' --agent --timeout 90s
```

Exit code: `0`.

### cct_read_before

```sh
CROCOBLOCK_DB=<local-path> python3 work/run-cli.py cct get pp_probe 3 --agent --timeout 90s
```

Exit code: `0`.

### cct_capture_before

```sh
CROCOBLOCK_DB=<local-path> python3 work/run-cli.py sync --resources schema,cct --db <local-path> --full --strict --cct pp_probe --fields pp_key,pp_label,pp_keep --agent --timeout 90s
```

Exit code: `0`.

### cct_update

```sh
CROCOBLOCK_DB=<local-path> python3 work/run-cli.py cct update pp_probe 3 --data '{"pp_label": "after"}' --agent --timeout 90s
```

Exit code: `0`.

### cct_read_after

```sh
CROCOBLOCK_DB=<local-path> python3 work/run-cli.py cct get pp_probe 3 --agent --timeout 90s
```

Exit code: `0`.

### cct_capture_after

```sh
CROCOBLOCK_DB=<local-path> python3 work/run-cli.py sync --resources schema,cct --db <local-path> --full --strict --cct pp_probe --fields pp_key,pp_label,pp_keep --agent --timeout 90s
```

Exit code: `0`.

### cct_reconcile

```sh
CROCOBLOCK_DB=<local-path> python3 work/run-cli.py cct reconcile pp_probe --before <local-path> --after <local-path> --key pp_key --fields pp_label,pp_keep --agent --timeout 90s
```

Exit code: `0`.

### cct_cleanup

```sh
CROCOBLOCK_DB=<local-path> python3 work/run-cli.py cct delete pp_probe 3 --agent --timeout 90s
```

Exit code: `0`.

### cct_absence_read

```sh
CROCOBLOCK_DB=<local-path> python3 work/run-cli.py cct list pp_probe --where pp_key=pp-local-b254242ccf1b --limit 5 --agent --timeout 90s
```

Exit code: `0`.

Assertions:

```json
[
  {
    "name": "cct_update_preserved_omitted",
    "result": "PASS"
  },
  {
    "name": "one_changed_real_row",
    "result": "PASS",
    "counts": {
      "ambiguous_keys": 0,
      "changed": 1,
      "created": 0,
      "deleted": 0,
      "unchanged": 0,
      "unmatched_suppressed": 0
    }
  },
  {
    "name": "cct_fixture_removed",
    "result": "PASS"
  }
]
```

Cleanup:

```json
[
  {
    "kind": "cct",
    "id": "3",
    "result": "PASS"
  }
]
```


## crocoblock-pp-cli relations audit — relations

Status: PASS. Raw outputs and exact argv: `crocoblock-local-analysis-relations.json`.

### relation_definitions

```sh
CROCOBLOCK_DB=<local-path> python3 work/run-cli.py relations definitions --agent --timeout 90s
```

Exit code: `0`.

### relation_child_create

```sh
CROCOBLOCK_DB=<local-path> python3 work/local-analysis-http.py POST /wp/v2/posts '{"title": "PP local relation pp-local-cd66af633019", "status": "draft"}'
```

Exit code: `0`.

### relation_connect

```sh
CROCOBLOCK_DB=<local-path> python3 work/run-cli.py relations update 2 --context child --mode append --data '{"parent_id": 13, "child_id": 34}' --agent --timeout 90s
```

Exit code: `0`.

### relation_edges_read

```sh
CROCOBLOCK_DB=<local-path> python3 work/run-cli.py relations list 2 --agent --timeout 90s
```

Exit code: `0`.

### relations_capture

```sh
CROCOBLOCK_DB=<local-path> python3 work/run-cli.py sync --resources relations,objects --db <local-path> --full --strict --object-types post --agent --timeout 90s
```

Exit code: `0`.

### relations_audit

```sh
CROCOBLOCK_DB=<local-path> python3 work/run-cli.py relations audit 2 --db <local-path> --agent --timeout 90s
```

Exit code: `0`.

### relation_disconnect

```sh
CROCOBLOCK_DB=<local-path> python3 work/run-cli.py relations update 2 --context child --mode disconnect --data '{"parent_id": 13, "child_id": 34}' --agent --timeout 90s
```

Exit code: `0`.

### relation_absence_read

```sh
CROCOBLOCK_DB=<local-path> python3 work/run-cli.py relations list 2 --agent --timeout 90s
```

Exit code: `0`.

### relation_child_trash

```sh
CROCOBLOCK_DB=<local-path> python3 work/local-analysis-http.py DELETE /wp/v2/posts/34
```

Exit code: `0`.

Assertions:

```json
[
  {
    "name": "relation_types_posts",
    "result": "PASS"
  },
  {
    "name": "child_is_draft",
    "result": "PASS"
  },
  {
    "name": "relation_write_verified",
    "result": "PASS"
  },
  {
    "name": "created_edge_readback",
    "result": "PASS"
  },
  {
    "name": "relation_disconnect_verified",
    "result": "PASS"
  },
  {
    "name": "temporary_edge_absent",
    "result": "PASS"
  },
  {
    "name": "temporary_child_trashed",
    "result": "PASS"
  },
  {
    "name": "real_edge_valid",
    "result": "PASS",
    "counts": {
      "dangling_endpoints": 0,
      "definitions": 1,
      "duplicate_edges": 0,
      "duplicate_inventory_ids": 0,
      "edges": 1,
      "inventory_rows": 3,
      "relations": 1,
      "unknown": 0,
      "valid_edges": 1
    }
  }
]
```

Cleanup:

```json
[
  {
    "kind": "relation+draft_post",
    "id": "34",
    "result": "PASS"
  }
]
```

Harness correction (raw output preserved; no remote write repeated): Assertion referenced counts instead of actual scanned output key; corrected assertion and evaluated captured raw output. No mutation repeated.


## crocoblock-pp-cli forms field-usage — field_usage

Status: PASS. Raw outputs and exact argv: `crocoblock-local-analysis-field_usage.json`.

Form23 and record1 were pre-existing synthetic lab fixtures, verified now with `forms get23` and `records fields1`. No historical creation exit is claimed. The real `sync` below writes the complete local declaration/record-presence capture required by this state-dependent command. These fixtures remain for the parent phase18 matrix; they are not abandoned cleanup.

### field_usage_form_read

```sh
CROCOBLOCK_DB=<local-path> python3 work/run-cli.py forms get 23 --agent --timeout 90s
```

Exit code: `0`.

### field_usage_record_read

```sh
CROCOBLOCK_DB=<local-path> python3 work/run-cli.py records fields 1 --agent --timeout 90s
```

Exit code: `0`.

### field_usage_capture

```sh
CROCOBLOCK_DB=<local-path> python3 work/run-cli.py sync --resources forms,records --db <local-path> --full --strict --record-form 23 --agent --timeout 90s
```

Exit code: `0`.

### field_usage_analysis

```sh
CROCOBLOCK_DB=<local-path> python3 work/run-cli.py forms field-usage 23 --db <local-path> --agent --timeout 90s
```

Exit code: `0`.

Assertions:

```json
[
  {
    "name": "live_persisted_record_metrics",
    "result": "PASS",
    "records_total": 1,
    "fields_total": 3
  }
]
```


## crocoblock-pp-cli reservations overlaps — reservations

Status: PASS. Raw outputs and exact argv: `crocoblock-local-analysis-reservations.json`.

The live provider rejects overlapping booking creation and treats an occupied appointment as a no-op. Expected exits5 below are successful rejection checks, not successful creates. Readback established that no second matching row appeared, and no mutation was retried. Real complete1+1 capture honestly reports zero violations under the explicit half-open policy.

### lab_no_mail_guard_source

```sh
CROCOBLOCK_DB=<local-path> ssh -i PII_SSH_IDENTITY_PATH_EXAMPLE -o IdentitiesOnly=yes -o BatchMode=yes -o ConnectTimeout=12 PII_SSH_TARGET_EXAMPLE 'cat /sites/the-disposable-lab/wp-content/mu-plugins/pp-lab-guards.php'
```

Exit code: `0`.

### lab_no_mail_guard_loaded

```sh
CROCOBLOCK_DB=<local-path> ssh -i PII_SSH_IDENTITY_PATH_EXAMPLE -o IdentitiesOnly=yes -o BatchMode=yes -o ConnectTimeout=12 PII_SSH_TARGET_EXAMPLE 'wp --path=/sites/the-disposable-lab plugin list --status=must-use --field=name'
```

Exit code: `0`.

### bookings_before

```sh
CROCOBLOCK_DB=<local-path> python3 work/run-cli.py bookings list --filters '{"apartment_id":13}' --limit 100 --agent --timeout 90s
```

Exit code: `0`.

### appointments_before

```sh
CROCOBLOCK_DB=<local-path> python3 work/run-cli.py appointments list --filters '{"service":13}' --limit 100 --agent --timeout 90s
```

Exit code: `0`.

### booking_create_1

```sh
CROCOBLOCK_DB=<local-path> python3 work/run-cli.py bookings create --data '{"apartment_id": 13, "check_in_date": "2038-06-10", "check_out_date": "2038-06-12", "status": "pending"}' --agent --timeout 90s
```

Exit code: `0`.

### booking_create_2

```sh
CROCOBLOCK_DB=<local-path> python3 work/run-cli.py bookings create --data '{"apartment_id": 13, "check_in_date": "2038-06-11", "check_out_date": "2038-06-13", "status": "pending"}' --agent --timeout 90s
```

Exit code: `5`.

Observed stderr:

```text
Error: plugin rejected /jet-booking/v2/add-booking: map[data:The selected dates are not available for booking. html:Selected dates are not available.Overlapping bookings: #3. overlapping_bookings:true success:false]
```

### booking_recovery_read_2

```sh
CROCOBLOCK_DB=<local-path> python3 work/run-cli.py bookings list --filters '{"apartment_id":13}' --limit 100 --agent --timeout 90s
```

Exit code: `0`.

### appointment_create_1

```sh
CROCOBLOCK_DB=<local-path> python3 work/run-cli.py appointments create --timezone UTC --data '{"service": 13, "provider": 0, "date": "2038-06-10", "slot": "10:00", "slot_end": "11:00", "status": "pending", "user_email": "pp-lifecycle@example.invalid", "user_name": "PP isolated synthetic fixture"}' --agent --timeout 90s
```

Exit code: `0`.

### appointment_create_2

```sh
CROCOBLOCK_DB=<local-path> python3 work/run-cli.py appointments create --timezone UTC --data '{"service": 13, "provider": 0, "date": "2038-06-10", "slot": "10:30", "slot_end": "11:30", "status": "pending", "user_email": "pp-lifecycle@example.invalid", "user_name": "PP isolated synthetic fixture"}' --agent --timeout 90s
```

Exit code: `5`.

Observed stderr:

```text
Error: mutation_applied_or_unknown: create reported success but found 0 new matching appointments (an occupied slot may be a no-op); inspect before retry
```

### appointment_recovery_read_2

```sh
CROCOBLOCK_DB=<local-path> python3 work/run-cli.py appointments list --filters '{"service":13}' --limit 100 --agent --timeout 90s
```

Exit code: `0`.

### reservation_capture

```sh
CROCOBLOCK_DB=<local-path> python3 work/run-cli.py sync --resources bookings,appointments --db <local-path> --full --strict --agent --timeout 90s
```

Exit code: `0`.

### reservation_overlaps

```sh
CROCOBLOCK_DB=<local-path> python3 work/run-cli.py reservations overlaps --db <local-path> --policy <local-path> --agent --timeout 90s
```

Exit code: `0`.

### booking_cleanup_3

```sh
CROCOBLOCK_DB=<local-path> python3 work/run-cli.py bookings delete 3 --agent --timeout 90s
```

Exit code: `0`.

### appointment_cleanup_3

```sh
CROCOBLOCK_DB=<local-path> python3 work/run-cli.py appointments delete 3 --agent --timeout 90s
```

Exit code: `0`.

### bookings_cleanup_read

```sh
CROCOBLOCK_DB=<local-path> python3 work/run-cli.py bookings list --filters '{"apartment_id":13}' --limit 100 --agent --timeout 90s
```

Exit code: `0`.

### appointments_cleanup_read

```sh
CROCOBLOCK_DB=<local-path> python3 work/run-cli.py appointments list --filters '{"service":13}' --limit 100 --agent --timeout 90s
```

Exit code: `0`.

Assertions:

```json
[
  {
    "name": "lab_outbound_mail_disabled",
    "result": "PASS"
  },
  {
    "name": "captured_real_reservations",
    "result": "PASS",
    "booking_ids": [
      "3"
    ],
    "appointment_ids": [
      "3"
    ]
  },
  {
    "name": "booking_deleted_3",
    "result": "PASS"
  },
  {
    "name": "appointment_deleted_3",
    "result": "PASS"
  },
  {
    "name": "all_owned_reservations_removed",
    "result": "PASS"
  }
]
```

Cleanup:

```json
[
  {
    "kind": "reservations",
    "booking_ids": [
      "3"
    ],
    "appointment_ids": [
      "3"
    ],
    "result": "PASS"
  }
]
```

Effective policy:

```json
{
  "schema_version": 1,
  "timezone": "UTC",
  "bookings": {
    "included_statuses": [
      "pending"
    ],
    "boundary": "half-open",
    "capacity": 1,
    "group_by": [
      "apartment_id",
      "apartment_unit"
    ],
    "date_encoding": "utc_calendar_date",
    "check_in_adjustment": "subtract_one_second"
  },
  "appointments": {
    "included_statuses": [
      "pending"
    ],
    "boundary": "half-open",
    "capacity": 1,
    "group_by": [
      "service",
      "provider"
    ]
  }
}
```


## crocoblock-pp-cli reservations overlaps — reservations_boundary

Status: PASS. Raw outputs and exact argv: `crocoblock-local-analysis-reservations_boundary.json`.

Four real adjacent reservations were created in2038. Under explicit half-open boundaries they produce zero violations. Under an explicitly supplied closed-boundary demonstration policy they produce two point conflicts, one in each model. This demonstrates policy behavior; it does not claim the closed policy matches any business. A limit1 report retains total2 and reports truncation.

### bookings_before

```sh
CROCOBLOCK_DB=<local-path> python3 work/run-cli.py bookings list --filters '{"apartment_id":13}' --limit 100 --agent --timeout 90s
```

Exit code: `0`.

### appointments_before

```sh
CROCOBLOCK_DB=<local-path> python3 work/run-cli.py appointments list --filters '{"service":13}' --limit 100 --agent --timeout 90s
```

Exit code: `0`.

### booking_create_1

```sh
CROCOBLOCK_DB=<local-path> python3 work/run-cli.py bookings create --data '{"apartment_id": 13, "check_in_date": "2038-06-10", "check_out_date": "2038-06-12", "status": "pending"}' --agent --timeout 90s
```

Exit code: `0`.

### booking_create_2

```sh
CROCOBLOCK_DB=<local-path> python3 work/run-cli.py bookings create --data '{"apartment_id": 13, "check_in_date": "2038-06-12", "check_out_date": "2038-06-14", "status": "pending"}' --agent --timeout 90s
```

Exit code: `0`.

### appointment_create_1

```sh
CROCOBLOCK_DB=<local-path> python3 work/run-cli.py appointments create --timezone UTC --data '{"service": 13, "provider": 0, "date": "2038-06-10", "slot": "10:00", "slot_end": "11:00", "status": "pending", "user_email": "pp-lifecycle@example.invalid", "user_name": "PP isolated synthetic fixture"}' --agent --timeout 90s
```

Exit code: `0`.

### appointment_create_2

```sh
CROCOBLOCK_DB=<local-path> python3 work/run-cli.py appointments create --timezone UTC --data '{"service": 13, "provider": 0, "date": "2038-06-10", "slot": "11:00", "slot_end": "12:00", "status": "pending", "user_email": "pp-lifecycle@example.invalid", "user_name": "PP isolated synthetic fixture"}' --agent --timeout 90s
```

Exit code: `0`.

### reservation_capture

```sh
CROCOBLOCK_DB=<local-path> python3 work/run-cli.py sync --resources bookings,appointments --db <local-path> --full --strict --agent --timeout 90s
```

Exit code: `0`.

### reservation_overlaps

```sh
CROCOBLOCK_DB=<local-path> python3 work/run-cli.py reservations overlaps --db <local-path> --policy <local-path> --agent --timeout 90s
```

Exit code: `0`.

### reservation_closed_boundary

```sh
CROCOBLOCK_DB=<local-path> python3 work/run-cli.py reservations overlaps --db <local-path> --policy <local-path> --agent --timeout 90s
```

Exit code: `0`.

### reservation_closed_limit

```sh
CROCOBLOCK_DB=<local-path> python3 work/run-cli.py reservations overlaps --db <local-path> --policy <local-path> --limit 1 --agent --timeout 90s
```

Exit code: `0`.

### booking_cleanup_4

```sh
CROCOBLOCK_DB=<local-path> python3 work/run-cli.py bookings delete 4 --agent --timeout 90s
```

Exit code: `0`.

### booking_cleanup_5

```sh
CROCOBLOCK_DB=<local-path> python3 work/run-cli.py bookings delete 5 --agent --timeout 90s
```

Exit code: `0`.

### appointment_cleanup_4

```sh
CROCOBLOCK_DB=<local-path> python3 work/run-cli.py appointments delete 4 --agent --timeout 90s
```

Exit code: `0`.

### appointment_cleanup_5

```sh
CROCOBLOCK_DB=<local-path> python3 work/run-cli.py appointments delete 5 --agent --timeout 90s
```

Exit code: `0`.

### bookings_cleanup_read

```sh
CROCOBLOCK_DB=<local-path> python3 work/run-cli.py bookings list --filters '{"apartment_id":13}' --limit 100 --agent --timeout 90s
```

Exit code: `0`.

### appointments_cleanup_read

```sh
CROCOBLOCK_DB=<local-path> python3 work/run-cli.py appointments list --filters '{"service":13}' --limit 100 --agent --timeout 90s
```

Exit code: `0`.

Assertions:

```json
[
  {
    "name": "lab_no_mail_precondition",
    "result": "PASS"
  },
  {
    "name": "captured_real_reservations",
    "result": "PASS",
    "booking_ids": [
      "4",
      "5"
    ],
    "appointment_ids": [
      "4",
      "5"
    ]
  },
  {
    "name": "real_touching_half_open_no_conflict",
    "result": "PASS",
    "booking_ids": [
      "4",
      "5"
    ],
    "appointment_ids": [
      "4",
      "5"
    ]
  },
  {
    "name": "real_closed_point_conflicts",
    "result": "PASS",
    "count": 2
  },
  {
    "name": "real_limit_retains_total",
    "result": "PASS"
  },
  {
    "name": "booking_deleted_4",
    "result": "PASS"
  },
  {
    "name": "booking_deleted_5",
    "result": "PASS"
  },
  {
    "name": "appointment_deleted_4",
    "result": "PASS"
  },
  {
    "name": "appointment_deleted_5",
    "result": "PASS"
  },
  {
    "name": "all_owned_reservations_removed",
    "result": "PASS"
  }
]
```

Cleanup:

```json
[
  {
    "kind": "reservations",
    "booking_ids": [
      "4",
      "5"
    ],
    "appointment_ids": [
      "4",
      "5"
    ],
    "result": "PASS"
  }
]
```

Effective policy:

```json
{
  "schema_version": 1,
  "timezone": "UTC",
  "bookings": {
    "included_statuses": [
      "pending"
    ],
    "boundary": "half-open",
    "capacity": 1,
    "group_by": [
      "apartment_id",
      "apartment_unit"
    ],
    "date_encoding": "utc_calendar_date",
    "check_in_adjustment": "subtract_one_second"
  },
  "appointments": {
    "included_statuses": [
      "pending"
    ],
    "boundary": "half-open",
    "capacity": 1,
    "group_by": [
      "service",
      "provider"
    ]
  }
}
```

Closed boundary demonstration policy:

```json
{
  "schema_version": 1,
  "timezone": "UTC",
  "bookings": {
    "included_statuses": [
      "pending"
    ],
    "boundary": "closed",
    "capacity": 1,
    "group_by": [
      "apartment_id",
      "apartment_unit"
    ],
    "date_encoding": "utc_calendar_date",
    "check_in_adjustment": "subtract_one_second"
  },
  "appointments": {
    "included_statuses": [
      "pending"
    ],
    "boundary": "closed",
    "capacity": 1,
    "group_by": [
      "service",
      "provider"
    ]
  }
}
```


## crocoblock-pp-cli filters impact — recorded write and reindex lifecycle

Status: PASS. Raw results/exits: `crocoblock-filter-impact-live.json`. Exact subprocess commands below are rendered from the saved executed runner `work/filter-impact-live.py`, its resolved snapshot path, and captured exit codes. No mutation is repeated merely to format this proof.

Prerequisite: filter12 already depends on pp_keep, and a temporary JetEngine declaration allowed scoped writes to post13. Temporary declaration meta-1 was removed after restore. An initial string-ID parsing failure in native meta-box creation was recovered by read-only inventory without repeating creation and assigned to the engine adapter owner.

### post13_baseline

```sh
CROCOBLOCK_DB=<local-path> python3 work/run-cli.py fields get 13 --fields pp_keep --agent --timeout 90s
```

Exit code: `0`.

### capture_filters_before

```sh
CROCOBLOCK_DB=<local-path> python3 work/run-cli.py sync --resources filters --db <local-path> --full --strict --agent --timeout 90s
```

Exit code: `0`.

### post13_temporary_write

```sh
CROCOBLOCK_DB=<local-path> python3 work/run-cli.py fields update 13 --data '{"pp_keep": "pp-impact-live-20261021"}' --snapshot <local-path> --agent --timeout 90s
```

Exit code: `0`.

### impact_candidate

```sh
CROCOBLOCK_DB=<local-path> python3 work/run-cli.py filters impact --db <local-path> --since-event 16 --agent --timeout 90s
```

Exit code: `0`.

### post13_restore

```sh
CROCOBLOCK_DB=<local-path> python3 work/run-cli.py fields restore --snapshot <local-path> --agent --timeout 90s
```

Exit code: `0`.

### post13_restored_readback

```sh
CROCOBLOCK_DB=<local-path> python3 work/run-cli.py fields get 13 --fields pp_keep --agent --timeout 90s
```

Exit code: `0`.

### reindex_verified_receipt

```sh
CROCOBLOCK_DB=<local-path> python3 work/run-cli.py filters reindex --ssh PII_SSH_TARGET_EXAMPLE --ssh-key PII_SSH_IDENTITY_PATH_EXAMPLE --wp-path /sites/the-disposable-lab --agent --timeout 90s
```

Exit code: `0`.

### capture_filters_after

```sh
CROCOBLOCK_DB=<local-path> python3 work/run-cli.py sync --resources filters --db <local-path> --full --strict --agent --timeout 90s
```

Exit code: `0`.

### impact_covered_by_receipt

```sh
CROCOBLOCK_DB=<local-path> python3 work/run-cli.py filters impact --db <local-path> --since-event 16 --agent --timeout 90s
```

Exit code: `0`.

Result: event17 changed fields:13/pp_keep and filter12 became candidate. Restore event18 returned pp_keep to index-value. Verified reindex receipt19 indexed one row and covers through event18. Recapture plus filters impact marked both matching events covered_by_receipt. Coverage is only for these locally journaled events, not global index freshness.

Cleanup: the executed runner called `h["request"]("DELETE", "/jet-engine/v2/delete-meta-box/meta-1")`; HTTP200 and success true were recorded. This is a Python HTTP call, not a separate process, so no process exit is invented. The exact original value was verified after removal with:

```sh
ssh -i PII_SSH_IDENTITY_PATH_EXAMPLE -o IdentitiesOnly=yes -o BatchMode=yes -o ConnectTimeout=12 PII_SSH_TARGET_EXAMPLE 'wp --path=/sites/the-disposable-lab post meta get 13 pp_keep'
```

Exit code: `0`. stdout: `index-value`. Filter12 original title and dependency settings were verified restored.
