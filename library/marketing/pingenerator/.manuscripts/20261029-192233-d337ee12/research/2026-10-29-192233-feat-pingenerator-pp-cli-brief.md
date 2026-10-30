# Pin Generator CLI Brief

Run: 20261029-192233-d337ee12 · Press v4.34.2 · Mode: degraded reprint (fresh research, prior hand-built CLI as reconciliation input)

## Evidence and gaps (read first)

| Source | Status in this run | What it gave |
|---|---|---|
| `pingenerator.com` (site, `/blog/api-documentation`, `/mcp`, `/llms.txt`) | **Blocked by session network policy** (proxy CONNECT 403; WebFetch DNS-denied) | Nothing fetched directly |
| `europe-west3-pingenerator-43a15.cloudfunctions.net/highMemLimitApi/mcp` | **Blocked by session network policy** | No live `tools/list` or `initialize` in this run |
| `api.wordpress.org` (official WP plugin source) | **Blocked by session network policy** | — |
| Web search index (2026-10-29) | Reachable | REST doc page lives at `pingenerator.com/blog/api-documentation`; documents **two** endpoints (generate image; generate + schedule pin) on the `highMemLimitApi` Cloud Function in `pingenerator-43a15`, authenticated with an **`access_key` header**. Homepage advertises "Pin Generator MCP for Claude, ChatGPT and Cursor" (labelled **beta**), and the "AI chat" feature. |
| AppSumo Q&A (search snippet) | Reachable via index | Vendor support recommends **automations** (sitemap-linked) over the REST API for auto-pinning new posts. |
| npm registry | Reachable | No Pin Generator SDK or wrapper exists (`adrian-pingenerator` is an unrelated PIN-code library). |
| Prior CLI (the prior CLI source, hand-built, tracked in the author's private skills repository) | Local | 21 MCP tool definitions with argument names, observed JSON-RPC response envelope (`result.content[0].text` JSON string, `result.isError`), Bearer auth, error-classification heuristics, 60 req/min rate limit note, credit costs per tool. These were observed live by the author; they are **not re-verified in this run**. |

Not available: prior `research.json`, prior manifests, prior scorecard, `.printing-press-patches/` (prior CLI predates Printing Press provenance). The run records this as a degraded reprint.

Discrepancy to keep open: the prior README says "3 REST endpoints"; the indexed docs page describes 2. The CLI does not ship REST commands, so this does not change the build, but it is recorded.

## API Identity
- Domain: Pinterest marketing automation SaaS (pingenerator.com). Turns blog posts, store products or a topic into designed Pinterest pins, then schedules them to connected Pinterest profiles.
- Users: bloggers, Etsy/Shopify/WooCommerce sellers, Pinterest marketing VAs and agencies, and now AI agents (vendor ships an MCP server, beta).
- Data profile: small per-account data (profiles, boards, scheduled pins, templates, design profiles, automations, stores/products). Credit-metered generation (1 credit template pin; 12–24 AI pin image; 13–25 AI pin generate+schedule; per-pin for automation batches).

## Auth truth
- **MCP endpoint** (`.../highMemLimitApi/mcp`): account **access key** from pingenerator.com/profile, sent as `Authorization: Bearer <key>` (prior CLI, live-observed). Env var kept: `PINGENERATOR_ACCESS_KEY`.
- **Documented REST** (same Cloud Function host): `access_key` header (indexed docs). Two endpoints only, both credit-spending writes. Not shipped as commands; MCP's `generate_pin_image` and `generate_and_schedule_pin` cover the same capability.
- **Web-app REST**: richer surface gated by a Firebase login session (prior CLI finding, user-confirmed). Out of scope: no credential uploads and no browser session in this run.

## Reachability Risk
- **Medium.** MCP is labelled beta by the vendor, so tool names and arguments may drift. Rate limit 60 requests/min per key (prior CLI). From this cloud session the vendor hosts are denied by policy. That is an environment limitation, not evidence the API is down.

## Top Workflows
1. Confirm connection and credits (`get_account_status`), then discover profiles and boards (`list_pinterest_profiles`, `list_boards`).
2. Blog to pins: `find_website_posts` from the sitemap → `get_post_content` → `generate_and_schedule_pin` (template) or `generate_and_schedule_ai_pin`.
3. Store to pins: `list_connected_stores` → `list_store_products` → generate and schedule.
4. Brand-consistent AI pins: `setup_design_profile` / `update_design_profile` → `generate_ai_pin_image` with `designProfileName`.
5. Hands-off pipelines: `create_automation` (website or manual source) → `run_automation_batch` → `get_automation_batch_status`; review the queue with `list_scheduled_pins`.

## Table Stakes
- Every one of the 21 MCP tools as a typed command with the exact argument names.
- Credit-spending tools must default to a dry run and require explicit `--execute` (prior safety contract, user-mandated).
- Free-but-stateful tools (`create_automation`, `setup_design_profile`, `update_design_profile`) support `--dry-run`.
- Agent-friendly JSON output, field selection, typed exit codes (2 usage, 3 auth, 4 credits, 5 rate limit), `doctor`.

## Data Layer
- Primary entities: profiles, boards, scheduled pins, automations, design profiles, templates.
- Sync cursor: none exposed by MCP (no pagination tokens except store-product `page`).
- FTS/search: low value. Accounts are small and the server is the source of truth. **No local store recommended**; sync would cost rate-limit budget for little gain.

## Competitors / incumbents
- The vendor's own MCP server, used directly from Claude/ChatGPT/Cursor (no CLI, no dry-run guard, no scripting).
- Pinterest-native MCP connectors (PostOnce, Outstand, Markifact, PinBridge and others) publish to Pinterest through the official Pinterest API but do **not** do Pin Generator's design/template/AI-image generation or sitemap automations.
- The public library's `pinterest` CLI (same printer) covers the Pinterest v5 API and analytics. Complementary: no overlap with Pin Generator's design engine.

## User Vision
- Fresh reprint of the house PinGenerator CLI. Keep slug `pingenerator` and binary `pingenerator-pp-cli`.
- Verify current public surface and auth truth rather than blindly using REST. Old access key authorizes MCP; richer web REST uses Firebase.
- Preserve dry-run defaults for generation, scheduling and automation.
- Credential-free work only: offline and mock tests. No production writes, no real pin generation or scheduling, no credit spend.

## Product Thesis
- Name: `pingenerator-pp-cli`
- Why it should exist: the only scriptable, agent-safe way to drive Pin Generator. One binary exposes all 21 MCP tools with typed flags, credit-guarded dry-run defaults, offline catalog/help, and stable exit codes. Batch pipelines (blog → pins, store → pins, automation runs) become reproducible shell steps instead of chat sessions.

## Build Priorities
1. Hand-authored JSON-RPC MCP client (Bearer, SSE strip, `content[0].text` unwrap, `isError` + HTTP status classification) as a regen-mergeable package; the generator's REST client cannot speak `tools/call`.
2. All 21 tools as typed commands with credit guard (`--execute`) and `--dry-run` for stateful tools. Offline `tools` catalog.
3. `doctor` that checks key presence and (when allowed) calls `get_account_status`.
4. Generated MCP server surface: credit-spending commands must not fire without explicit confirmation.
5. Offline `httptest` parity tests for envelope, auth, unwrap, error→exit-code mapping, and zero-network dry runs.
