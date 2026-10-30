# Security review — round 1

Result: 1 in-scope finding. No source edits, credentials read, external calls or real remote mutations. Reviewed hand-authored command, MCP transport/auth, capture/inspection packages and generated root/store/MCP integration boundaries. Reserved cliutil/cobratree source was not changed.

## Finding

**P2 — redact submitted verification secrets in upstream errors.** `internal/metamcp/client.go:110` returns request-layer RPC errors directly; `:127–135` formats tool `isError` text using `safe()`, which at `:178–185` removes only the OAuth bearer. A server echo of submitted `verification_code`, `pin`, or `verify_token` reaches CLI stderr/MCP output despite redacted previews and disabled domain journals.

Reproduced with an overlay-only local httptest regression: a valid verify-phone input carrying a synthetic sentinel and an `isError` reply echoing it. The assertion that the surfaced error excludes the sentinel failed. The same exposure follows the top-level RPC-error path. No real credentials were used.

Fix: centralize the sensitive-key predicate, recursively collect nonempty submitted secret values for each call, and redact those values from both request/RPC and `isError` errors before truncation. Preserve typed errors. Add fixture assertions for verification code, PIN, webhook token, top-level RPC and tool error replies, plus non-sensitive actionable error retention.

## Boundaries reviewed

- Domain writes require a separate parsed confirmation flag; JSON `confirm`, `--yes`, and harness confirmed writes cannot execute them.
- Raw generated RPC route is hidden/preview-only and not registered as a typed bypass.
- Native credential reuse is explicit and bound to the official endpoint; external origins and redirects are rejected. Keychain access targets only exact scoped entries.
- SQL uses a driver-level read-only SQLite connection; custom search uses parameterized SQL/quoted FTS terms; output is bounded.
- Captures use private local storage and immutable identities. Preview redacts the existing top-level secret fields.
- JSON Schema compiler does not install an external URL loader. Bounded response/SSE reads and caller deadlines constrain transport paths.
- Generic invocation journals classify flag values rather than serializing them; no additional value-leak finding from sync/search journaling.

No additional in-scope security finding. No template/out-of-scope retro candidate identified in this review.
