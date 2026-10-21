# Correctness review — round 1

Result: **FAIL — 2 reproducible, in-scope findings.** Review path: direct reviewer subagent dispatch (correctness). No implementation changes and no laboratory requests. The reproducer was injected through a Go overlay stored outside the repository; only its two targeted tests ran.

## C1 · P2 · Form schema comparison drops validation and input-type changes

- Location: `internal/cli/schema_diff.go:48–52`; lossy projection at `internal/cli/croco_forms.go:217`.
- Reproduction: compare the same form ID, status, title, field name and label, changing only the text-field Gutenberg attribute `required:false` to `required:true`, or `field_type:"text"` to `field_type:"email"`. Both pairs normalize to identical `{name,type:"text-field",label}` objects. `crocoSchemaChanges` returns zero differences.
- Source contract: official local JetFormBuilder source `discovery/jetformbuilder-source/modules/blocks-v2/text-field/block.json:34–36` defines `field_type`; lines 123–125 define `required`. These are actual supported field-definition changes, not invented attributes.
- Impact: `schema diff --resources forms` can report no changes despite a new submission requirement or input type. The command explicitly directs callers to use it for changed form definitions, so this hides a real deployment change.
- Fix: use a schema-specific, sanitized representation of stored block definitions that retains structural/validation attributes. Preserve the existing minimal field-name/presence representation for field-usage if needed. Do not solve it by printing submitted values or secret configuration.
- Verification: `TestReviewRound1SchemaFormValidationChange/{required,field_type}` both fail the assertion that the definition change produces at least one difference.

## C2 · P2 · Missing reservation data becomes a complete empty capture

- Location: `internal/cli/croco_bookings.go:56–70`; shared by bookings and appointments. Sync propagates the result at `internal/cli/croco_sync.go:317–344`.
- Reproduction: a TLS test server returns either `{"success":true,"total":0}` or `{"success":true,"data":null,"total":0}`. `crocoArray(nil)` supplies an empty array, so `crocoReservationPage` sets `complete:true`; `crocoReservationAll` succeeds with an empty collection for both resource types.
- Impact: malformed/missing collections are indistinguishable from the valid `data:[]` result. Sync can replace prior local reservation rows with an empty capture marked complete, and overlap analysis can then give a false clean result. A successful HTTP response and `total:0` do not establish that the collection was delivered.
- Fix: require an explicitly present, non-null JSON array for `data` before count/coverage calculation. Keep `data:[]` as a valid empty collection and fail closed for missing/null/malformed data. Add both booking and appointment cases to the focused response tests.
- Verification: `TestReviewRound1ReservationMissingCollection` fails all four combinations (missing/null × booking/appointment), confirming current success with an empty collection.

## Evidence

- Raw output: `proofs/review-correctness-round1-repro.txt`.
- Reproducer source: `<local-path>`.
- Invocation: `go test -overlay <local-path> ./internal/cli -run '^TestReviewRound1' -count=1 -v`.
- Exit: 1; 2 top-level tests / 6 cases failed with the exact observed behavior above. These are new focused review reproducers; they do not contradict the recorded pre-review suite PASS.
- Examined native contracts and mutation readback, forms/fields/records, reservations, filters/reindex, six local analyses, sync and atomic snapshot/store paths, plus client replay intent and MCP dispatch. No additional confirmed correctness finding was identified in that directed review. No template/out-of-scope retro candidates from this reviewer.

Convergence: **not cleared at round 1**; parent may autofix C1/C2 and rerun every reviewer as required by phase 17.
