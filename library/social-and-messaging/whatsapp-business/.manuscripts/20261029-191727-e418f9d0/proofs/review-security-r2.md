# Security review — round 2

PASS — no in-scope security findings pending. Re-reviewed the merged hand-authored CLI/MCP/auth/transport/privacy, capture and inspection packages and generated integration boundaries. No source edits, credential retrieval, external calls or real remote mutations.

Round-1 submitted-secret error reflection is fixed: per-call recursive sensitive values now reach both top-level RPC error sanitization and tool `isError` sanitization. The bearer and submitted values are removed before bounded error rendering. Values are not stored on the client, and nested previews redact without mutating inputs. Typed RPC errors remain intact.

Validation: the independent overlay-only local regression that failed in round 1 now passes. The parent-added two error-form regression and selected mutation-no-retry regression pass. Selected CLI dry-run and harness-refusal tests also pass. These fixtures used synthetic values only.

Cascading review: separate confirmation and JSON-confirm refusal remain intact; raw RPC remains preview-only; exact native credential scope and official-origin/redirect restrictions remain intact; read-only SQL still uses driver mode=ro; capture query paths remain parameterized; caller deadlines and transport/body bounds remain. Actual negotiated protocol/endpoint provenance does not introduce credential or dispatch bypass. No new security issue from the merged scope guards or business-scope comparison validation.

No template/out-of-scope security retro candidate. Reserved cliutil/cobratree packages unchanged by this reviewer.
