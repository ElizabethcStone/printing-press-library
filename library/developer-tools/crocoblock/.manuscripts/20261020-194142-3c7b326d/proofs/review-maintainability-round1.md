# Maintainability review — round 1

Review path: direct subagent dispatch, maintainability reviewer. Read-only source review; no code edits, lab requests, test-suite execution, or receipt edits.

Scope: curated command wiring and mutations in `internal/cli`, their local snapshot/journal persistence in `internal/store`, and MCP registration/client contracts. Generator-owned `internal/cliutil` and `internal/mcp/cobratree` were not reviewed for changes; the latter's documented placeholder parser was inspected only to validate an in-scope command-shape integration defect. Existing whole-Go-test PASS was supplied by the parent, not rerun here.

## Findings

### M1 — [P2] Required identifiers disappear from the MCP/runtime command contract

Primary location: `internal/cli/croco_appointments.go:61` (`Use: "get ID"`). Same defect at `croco_appointments.go:328,368,398,438`, `croco_bookings.go:96,342,382`, `croco_fields.go:42`, and `croco_forms.go:78`.

**Behavior:** Eleven leaves emit bare `ID`, `SERVICE_ID`, or `RESOURCE_ID`, although the Press runtime and MCP mirror recognize only `<required>` or `[optional]` placeholders. For ten leaves with no bracketed token, the MCP mirror has neither the ID property nor the raw `args` escape hatch; they cannot receive the required identifier. Appointment delete recognizes only its optional `[ID...]`, so its mandatory first ID is not represented as required.

**Evidence:** `work/pre-matrix-agent-context.json` contains `appointments get: "get ID"`, `bookings config: "config RESOURCE_ID"`, `fields update: "update ID"`, and `forms update: "update ID"`, while correctly shaped sibling `forms get` emits `get <id>`. `internal/mcp/tools.go:79` mirrors that same Cobra tree. Its consumer matches only angle/square placeholders and allows raw args only when that match succeeds.

**Reproduction:** Inspect `agent-context`, then list the mirrored MCP tools and inspect the appointment-get schema: it has no required ID input. Supplying structured `id` or raw `args` is rejected as unknown, and invoking it without one reaches the CLI's missing-ID error. No live WordPress request is needed to reproduce the schema mismatch.

**Fix:** Normalize all affected hand-written Use strings to `get <id>`, `update <id>`, `delete <id>`, `slots <service-id>`, `providers <service-id>`, and `config <resource-id>`; use `delete <id> [<id>...]` for variadic appointment deletion. Keep dry-run before argument validation. Add a focused runtime/MCP schema assertion so the required ID remains visible after reprint. No cobratree template change is needed.

### M2 — [P2] Snapshot finalization can destroy the only pre-write backup

Primary location: `internal/cli/croco_fields.go:552` (`file.Truncate(0)`); write follows at line555. The same helper is called after the remote mutation and successful readback at line163.

**Behavior:** `fields update` initially persists `Before` safely, performs the remote write, and then rewrites the same backup inode in place to mark it verified. A process crash between truncate/write, or a partial/failing write during finalization, destroys or corrupts `Before` after the remote mutation already happened. The command reports finalization failure, but the original data needed for recovery has been lost.

**Reproduction:** In a controlled test, make the second `crocoWriteFieldSnapshot` call fail immediately after its successful truncate, or terminate the process at that point after a successful fake-server mutation. The existing snapshot becomes empty/partial; `crocoReadFieldSnapshot` cannot recover the original `Before`. This is a source-level failure-path reproduction, not a lab mutation performed by this review.

**Fix:** Finalize through a private sibling temporary file, write and sync it completely, close it, then atomically replace the snapshot. If finalization fails before replacement, retain the original pre-write snapshot unchanged. Preserve the exclusive-create semantics for the initial snapshot, and test that a finalization failure leaves `Before` readable for recovery.

## Reviewed invariants without additional findings

- Curated adapters replace raw `*-api`/transport leaves through preserved registration hooks; the six analyses have independent hand-written registration and proof annotations.
- `CrocoReplace` atomically swaps rows, search index, coverage and sync state; `CrocoSnapshot` reads rows and coverage in one read transaction.
- Site pinning and journal errors are explicit, mutation requests disable replay, and failed journal writes do not report success.
- Local analysis wrappers check dry-run before file/store access and retain the bounded command context.
- No style-only or speculative refactoring findings were raised. No generator-reserved patch is proposed.

Convergence outcome: round 1 has two in-scope findings for the parent's merged review; no fixes were made by this reviewer.
