# Public review regression fixes

Four accepted findings from PR #2347 were fixed before the next publication gate.

- Every advertised export collection follows its real plugin pagination contract and validates arrays, identities, and available totals. Forms use stable WordPress pages; reservations use GET offset pagination. Malformed or incomplete results fail.
- Export previews perform no file, configuration, authentication, or network access. Completed file exports replace the target atomically from a private staging file; failures retain the previous backup.
- JSONL imports use the site-pinned WordPress no-replay transport. Six configured idempotency-header variants cannot cause a second request after an ambiguous committed HTTP 500. Typed authentication and refusal failures stop the batch.
- Field update and restore persist metadata-only intent before transport. Failed transport, readback, verification, or snapshot finalization leaves an unresolved intent. Matching filters report unknown even if a concurrent reindex receipt covers the intent sequence. A verified completion links its original intent and retains its own later event sequence.

Validation: Go 1.26.10 build, vet, complete tests, and reachable govulncheck passed. Focused cases include all four collections across multiple pages, full-page WordPress termination, malformed/changing totals, backup retention, JSON/JSONL atomic replacement, import replay suppression, journal preflight refusal, failed field readback, mismatched values, and failed snapshot finalization. No request payload values enter the journal. Full live matrix results are recorded separately in publish-live-summary.md and phase5-acceptance.json.

Follow-up review: intent/completion reconciliation now indexes attempts once by int64 event ID and performs constant-time completion lookups. A 10,000-pair regression covers large IDs and unordered input; nonmatching and malformed links retain conservative outcomes. This removes the quadratic journal scan without weakening ambiguity handling.
