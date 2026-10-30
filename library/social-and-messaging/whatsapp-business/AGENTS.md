# WhatsApp Business Tools CLI operating contract

Inspect the actual command tree and current local configuration:

```bash
whatsapp-business-pp-cli doctor --codex-auth --json
whatsapp-business-pp-cli agent-context --pretty
whatsapp-business-pp-cli connection ping --help
```

Use the official MCP at `https://mcp.facebook.com/whatsapp_business_tools`. The schema-aware commands negotiate sessions and verify the live tool schema and read-only hint. The hidden raw RPC route only previews. Do not add a direct execution path that bypasses those guards.

Discover businesses, then select a business explicitly. Preserve the distinction between business admin and app admin, owned and shared accounts, exact account type, page counts and known totals. Unknown setup state remains unknown. `apps publish` and `businesses verify` only report status and return links.

WhatsApp writes preview by default. Execute only when the user authorizes the exact operation, target and content, then pass the separate `--confirm` flag. JSON arguments, `--yes` and `--agent` do not authorize writes. All verification harnesses refuse confirmed writes.

Native OAuth reuse is an explicit `--codex-auth` option and currently reads the Mac's existing Codex Keychain entry. It does not implement independent OAuth registration or copy tokens into source. Unlock or repair the current host's Keychain before considering reauthentication. Use `auth codex --launch` for the supported native login when required.

Captures are immutable local evidence with an exact scope, source tool, timestamp and completeness. Local `ops` commands reject live mode; live tool commands reject local mode. Missing records on partial pages cannot be called deleted. Preserve capture provenance in comparisons. A bare `ops` leaf reports missing selection inputs with no fabricated result.

Keep `// pp:data-source local` on local command files and matching `pp:data-source`/`mcp:read-only` annotations. All custom live calls must use `boundCtx`, generated config loading, adaptive rate limiting and typed throttling errors. Never retry writes after ambiguous transport outcomes.

Tool invocations disable learning journals for argument privacy. Only non-sensitive structural hints belong in explicit teach/recall; credentials, customer data and verification values do not. Capture stores belong in private local data directories, never manuscript proofs or source archives.

Hand-authored MCP, capture and inspection packages are durable extensions. Record code-level contracts in `.printing-press-patches/`; keep generated shared runtime packages unchanged. Re-run behavioral tests, shipcheck and the source-bound live matrix after code changes. Do not hand-edit acceptance fingerprints or score fields.

This is a local print. Public publication and release/version stamping require a separate user request. No release tag or public install availability is implied by generated template references.

The generic raw JSONL import adapter is disabled and its write-resource mapping removed at process initialization. Do not reintroduce it as an alternate route to tools/call. Use sync --arguments for typed read filters. Inspection compaction must preserve rare before/after values and nested evidence keys; explicit user --select projections still apply.
