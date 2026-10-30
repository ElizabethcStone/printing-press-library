# Phase 4.8 — SKILL semantic review

PASS — no findings after remediation.

Re-review covered all seven checks against current SKILL, staged binary help, README, approved absorb manifest, research planned/built lists and implementation. The exact five-command set matches novel_features_built. Re-ran help for all five inspection leaves, onboarding/status, accounts/list, connection/ping and auth/codex; every command resolved successfully. No external requests or real mutations were used.

Trigger phrases map to actual capabilities. All five inspection descriptions match help and disclose local capture selection. Auth narrative accurately describes explicit native Mac OAuth reuse and independent-registration limitations. Recipes match output shapes; the replacement ping projection selects actual tool/data fields. Descriptions are concrete and avoid marketing promises.

All four former findings are fixed: --confirm is the domain execution switch; bounded read retries and no automatic write retries are documented; learning prose explicitly excludes domain invocation journals; schema-aware ping replaces the raw RPC projection example.

The former findings below are retained only as a closed audit trail, not current blockers.

---

# Phase 4.8 — SKILL semantic review

Status: findings (4 errors; no warning-only approval needed). Review used the staged binary help for all five inspection leaves and auth/codex, SKILL, README, approved absorb manifest, research planned/built lists and command implementation. No remote requests or mutations.

1. **Recipe/output claims — error — SKILL.md:195.** Agent guidance says pass `--yes` as confirmation, but domain writes require separate `--confirm`; `--yes` cannot execute them. Replace this bullet with the actual review-then-confirm contract already stated at line 518.
2. **Recipe/output claims — error — SKILL.md:196.** The generic `--idempotent` create-retry guidance does not apply to schema-aware MCP tools; their execution path never consumes that flag and mutations are never retried. State this limitation here rather than directing agents to use it.
3. **Recipe/output claims — error — SKILL.md:229.** “Every invocation is journaled” contradicts line 227 and domain `PreRunE`, which disables learning. Scope the generic learning description to commands that retain journaling, and keep sensitive domain inputs excluded.
4. **Recipe/output claims — error — SKILL.md:191.** The raw RPC example used to demonstrate field selection ignores `--select` and returns a preview plan (`dry_run/action/would`), not `error/id/jsonrpc`. Replace it with a schema-aware command whose actual output supports projection. Observed locally with the staged binary, exit 0, no network.

Other checks: trigger phrases map to real commands; exact five-feature verified-set alignment; all five descriptions match leaf help; capture-selection and no-data limitations are disclosed; native OAuth limitations and `auth codex --launch` match implementation; prose is concrete rather than marketing.

Additional downstream documentation-audit candidates (outside the seven semantic checks): README doctor claims connectivity despite `network_checked:false`; README troubleshooting prints credential value with echo; generic public-install references retain Go 1.26.9 despite local floor 1.26.10. Parent notified for Phase 15.
