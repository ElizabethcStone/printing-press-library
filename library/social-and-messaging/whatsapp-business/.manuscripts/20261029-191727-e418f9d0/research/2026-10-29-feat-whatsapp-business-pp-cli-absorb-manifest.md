# WhatsApp Business Tools — proposed absorb manifest

Status: approved by Bobe. Nothing generated or promoted.

## Scope
Target is the official Meta WhatsApp Business Tools MCP. All 24 live tools are shipping scope. Other providers and direct Cloud API features absent from this MCP are excluded. No placeholder/stub implementations are proposed.

## Absorbed

| # | Feature | Best Source | Our Implementation | Added Value |
|---|---|---|---|---|
| 1 | whatsapp_biz_businesses (read) | Official authenticated Meta MCP catalog | whatsapp-business-pp-cli businesses list | Schema-aware inputs; JSON/agent output; explicit targeting; preview/confirmation for writes |
| 2 | ping (read) | Official authenticated Meta MCP catalog | whatsapp-business-pp-cli connection ping | Schema-aware inputs; JSON/agent output; explicit targeting; preview/confirmation for writes |
| 3 | whatsapp_biz_add_phone_number (state-changing) | Official authenticated Meta MCP catalog | whatsapp-business-pp-cli phones add | Schema-aware inputs; JSON/agent output; explicit targeting; preview/confirmation for writes |
| 4 | whatsapp_biz_get_template (read) | Official authenticated Meta MCP catalog | whatsapp-business-pp-cli templates get | Schema-aware inputs; JSON/agent output; explicit targeting; preview/confirmation for writes |
| 5 | whatsapp_biz_manage_templates_archival (state-changing) | Official authenticated Meta MCP catalog | whatsapp-business-pp-cli templates archival | Schema-aware inputs; JSON/agent output; explicit targeting; preview/confirmation for writes |
| 6 | whatsapp_biz_publish_app (read) | Official authenticated Meta MCP catalog | whatsapp-business-pp-cli apps publish | Schema-aware inputs; JSON/agent output; explicit targeting; preview/confirmation for writes |
| 7 | whatsapp_biz_list_templates (read) | Official authenticated Meta MCP catalog | whatsapp-business-pp-cli templates list | Schema-aware inputs; JSON/agent output; explicit targeting; preview/confirmation for writes |
| 8 | whatsapp_biz_send_verification_code (state-changing) | Official authenticated Meta MCP catalog | whatsapp-business-pp-cli phones send-code | Schema-aware inputs; JSON/agent output; explicit targeting; preview/confirmation for writes |
| 9 | whatsapp_biz_onboarding_status (read) | Official authenticated Meta MCP catalog | whatsapp-business-pp-cli onboarding status | Schema-aware inputs; JSON/agent output; explicit targeting; preview/confirmation for writes |
| 10 | whatsapp_biz_system_user_token (state-changing) | Official authenticated Meta MCP catalog | whatsapp-business-pp-cli auth system-user-link | Schema-aware inputs; JSON/agent output; explicit targeting; preview/confirmation for writes |
| 11 | whatsapp_biz_update_template (state-changing) | Official authenticated Meta MCP catalog | whatsapp-business-pp-cli templates update | Schema-aware inputs; JSON/agent output; explicit targeting; preview/confirmation for writes |
| 12 | whatsapp_biz_configure_webhooks (state-changing) | Official authenticated Meta MCP catalog | whatsapp-business-pp-cli webhooks configure | Schema-aware inputs; JSON/agent output; explicit targeting; preview/confirmation for writes |
| 13 | whatsapp_biz_accounts (read) | Official authenticated Meta MCP catalog | whatsapp-business-pp-cli accounts list | Schema-aware inputs; JSON/agent output; explicit targeting; preview/confirmation for writes |
| 14 | whatsapp_biz_create_template (state-changing) | Official authenticated Meta MCP catalog | whatsapp-business-pp-cli templates create | Schema-aware inputs; JSON/agent output; explicit targeting; preview/confirmation for writes |
| 15 | whatsapp_biz_create_template_from_library (state-changing) | Official authenticated Meta MCP catalog | whatsapp-business-pp-cli templates from-library | Schema-aware inputs; JSON/agent output; explicit targeting; preview/confirmation for writes |
| 16 | whatsapp_biz_phone_numbers (read) | Official authenticated Meta MCP catalog | whatsapp-business-pp-cli phones list | Schema-aware inputs; JSON/agent output; explicit targeting; preview/confirmation for writes |
| 17 | whatsapp_biz_subscribe_webhook (state-changing) | Official authenticated Meta MCP catalog | whatsapp-business-pp-cli webhooks subscribe | Schema-aware inputs; JSON/agent output; explicit targeting; preview/confirmation for writes |
| 18 | whatsapp_biz_send_message (state-changing) | Official authenticated Meta MCP catalog | whatsapp-business-pp-cli messages send | Schema-aware inputs; JSON/agent output; explicit targeting; preview/confirmation for writes |
| 19 | whatsapp_biz_delete_template (state-changing) | Official authenticated Meta MCP catalog | whatsapp-business-pp-cli templates delete | Schema-aware inputs; JSON/agent output; explicit targeting; preview/confirmation for writes |
| 20 | whatsapp_biz_verify_business (read) | Official authenticated Meta MCP catalog | whatsapp-business-pp-cli businesses verify | Schema-aware inputs; JSON/agent output; explicit targeting; preview/confirmation for writes |
| 21 | whatsapp_biz_template_library (read) | Official authenticated Meta MCP catalog | whatsapp-business-pp-cli templates library | Schema-aware inputs; JSON/agent output; explicit targeting; preview/confirmation for writes |
| 22 | whatsapp_biz_register_phone_number (state-changing) | Official authenticated Meta MCP catalog | whatsapp-business-pp-cli phones register | Schema-aware inputs; JSON/agent output; explicit targeting; preview/confirmation for writes |
| 23 | whatsapp_biz_verify_phone_number (state-changing) | Official authenticated Meta MCP catalog | whatsapp-business-pp-cli phones verify | Schema-aware inputs; JSON/agent output; explicit targeting; preview/confirmation for writes |
| 24 | whatsapp_biz_configure_payments (state-changing) | Official authenticated Meta MCP catalog | whatsapp-business-pp-cli payments configure | Schema-aware inputs; JSON/agent output; explicit targeting; preview/confirmation for writes |

## Shared implementation requirements
- Go CLI plus generated local stdio MCP surface; explicit JSON/agent output and typed exit errors.
- Use real MCP initialize/session handling, tools/list and tools/call over HTTP. No invented REST paths.
- Exact live schema validation; live catalog drift warnings; schema-aware errors.
- OAuth: explicit opt-in to reuse the native Codex OAuth context already validated on this Mac. Token values stay in credential storage and never enter source/proofs. No claim that an independently registered public client was approved.
- All state-changing tool paths default to a local preview. Actual execution requires explicit confirmation; even generic RPC paths must enforce this. No live mutations are authorized for implementation tests.
- Read snapshots carry capture timestamp, exact scope, source tool, pagination/completeness and unknown states. Partial snapshots cannot prove deletion or missing configuration.
- SQLite captured-read persistence and offline exact/FTS lookup; no invented chat history, delivery state, billing/cost estimates or message eligibility.
- Read-only retries honor bounded Retry-After/backoff; mutations never auto-retry on ambiguous transport failures. Inspired by source review of WBMCP's read-safe retry policy.
- Template cursor/input and JSON/HTTP error handling inspired by source review of Gonzalez8/whatsapp-business-mcp; domain operations remain Meta MCP calls.

## Live test boundaries
OAuth, catalog discovery, initialize, businesses listing and ping already passed. Test additional read commands only against explicitly selected available scopes. Test every write through a network-free preview or local fixture; never send, register, create, delete, configure, verify or publish real assets during dogfood.

## Transcendence

| # | Feature | Command | Score | Buildability | How It Works | Evidence | Long Description |
|---|---|---|---|---|---|---|---|
| 1 | Local scope map | ops scope-map | 9/10 | hand-code | This uses saved whatsapp_biz_businesses, whatsapp_biz_accounts and whatsapp_biz_phone_numbers responses and their capture-scope metadata to join known relationships and expose unresolved links with no external dependencies. | The live accounts schema distinguishes owned/shared accounts, account roles and parent WhatsApp accounts; phone discovery supports a number-specific account resolution. Cursor plugin documentation requires explicit account/phone context. | Use this command to inspect captured business, account and phone relationships for an explicitly selected scope. Do NOT use it for historical configuration changes; use 'ops changes' instead. |
| 2 | Captured asset changes | ops changes | 8/10 | hand-code | This uses two locally stored, time-stamped MCP response snapshots with identical business/app/account/filter scope to compute structural changes and completeness-aware additions or removals with no external dependencies. | Live onboarding_status exposes per-app and per-account state, done/todo/unknown checks and capped pages; the brief identifies resuming setup and template review as repeated rituals. PyWa issues #221 and #214 show that absent fields can reflect contract changes rather than real entity removal. | Use this command to compare earlier and later captures of the same selected business assets. Do NOT use it to inspect current captured relationships; use 'ops scope-map' instead, or use 'ops template-compare' for two templates from different accounts. |
| 3 | Cross-account template comparison | ops template-compare | 8/10 | hand-code | This uses two explicitly selected saved whatsapp_biz_get_template responses plus their account/template IDs and capture metadata to compute a field-and-component diff with no external dependencies. | Live get_template returns full state/category/rejection/component details and is account-scoped; live accounts distinguishes multiple account models and roles. No cross-account template comparison tool appears in the authenticated catalog. | Use this command to compare two explicitly selected saved templates, including templates in different accounts. Do NOT use it for a same-scope before/after inventory comparison; use 'ops changes' instead, or use 'ops template-checklist' for the values a template requires. |
| 4 | Template component checklist | ops template-checklist | 9/10 | hand-code | This uses a saved whatsapp_biz_get_template component document and the captured whatsapp_biz_send_message input contract to extract placeholder paths and supported versus unsupported parameter channels with no external dependencies. | The live send_message contract requires reading template components first, separates body/header/button values, rejects richer unsupported component cases and forbids using review examples as recipient data. The live create_template contract supports named placeholders and richer buttons, so creation and sending have different capabilities. | Use this command to extract component and placeholder requirements from one saved template before composing a message. Do NOT use it to compare templates across accounts; use 'ops template-compare' instead. This checklist does not establish delivery eligibility or approval. |
| 5 | MCP catalog drift | ops catalog-drift | 9/10 | hand-code | This uses two locally saved tools/list catalogs and their exact input/output schemas to compute tool additions/removals, required-input additions and schema-path changes with no external dependencies. | Official MCP documentation labels the server beta and evolving; live discovery returned 24 tools versus the documentation's 18. PyWa issues #221 and #214 demonstrate upstream field deprecation/removal as an actual integration failure pattern. | Use this command to compare the upstream MCP tool contract between two saved catalogs. Do NOT use it for changes to business accounts, phones or template data; use 'ops changes' instead. |

All five require hand-written Go. Zero novel features are emitted automatically by the transport spec. Each reads captured local data and rejects --data-source live. Capture freshness/unsynced hints, exact scope and completeness checks are shared requirements. Seven weaker candidates were cut in the persisted brainstorm.

## Approval
Approved by Bobe. Total: 24 live tools + 5 local compound features = 29 capabilities, plus framework/auth/snapshot support. No stubs.
