# Phase 17 maintainability review — round 1

Scope: custom CLI wiring and tests, MCP transport/catalog/auth, immutable capture store and inspection packages; root hook, command mirror integration and durability records. Reserved cliutil/cobratree excluded. No source edit, credential lookup, remote request or real mutation.

## Findings

1. **Low — generic output-limit contract permits zero.** `internal/cli/whatsapp_commands.go:180` checks `< 0`, although its own usage error and bounded-output contract say 1–1000; downstream `if limit > 0` makes zero unbounded. Reproduced: staged binary `connection ping --limit 0 --dry-run --json` exits 0. Fix: when no upstream `limit` property exists, validate the custom display limit as 1–1000; do not reject the zero local variable on commands whose limit is an upstream schema flag.

2. **Medium — catalog provenance records a constant rather than negotiation result.** `internal/cli/whatsapp_commands.go:403` persists `protocol: 2025-03-26`, while `internal/metamcp/client.go:70` uses whatever `protocolVersion` the initialized server negotiated. Captures can therefore assert equal protocol scopes across different actual sessions, bypassing CatalogDrift’s same-protocol prerequisite. Fix: expose the negotiated protocol on Client and save that value; test a loopback fixture returning a different version (or reject unsupported versions explicitly). No live claim is made about current Meta choosing a different version.

No additional maintainability blockers found. Declarative facades are justified by static command tooling and delegate shared runtime behavior; all 24 schema-aware leaves retain previews and common validation. Custom root registration is through the existing novel hook, and durable patch records list the custom source/data files.

Convergence: awaiting parent autofix and all-reviewer round 2.
