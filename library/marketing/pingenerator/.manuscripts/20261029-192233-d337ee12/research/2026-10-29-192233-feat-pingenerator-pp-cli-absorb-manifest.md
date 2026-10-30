# Pin Generator CLI Absorb Manifest

Sources absorbed: the Pin Generator MCP server (vendor, beta; 21 tools) and the prior hand-built `pingenerator-pp-cli` (the author's private skills repository, `skills/pp-pingenerator`). There are no community SDKs on npm or PyPI and no competing Pin Generator CLI. Pinterest-native MCP connectors do not reach Pin Generator's design engine, so they contribute no rows.

Degraded reprint: there was no prior `research.json`, manifest or patches. The prior CLI's features are carried as absorbed rows, so nothing is dropped silently.

## Absorb Manifest

### Absorbed (match or beat everything that exists)
| # | Feature | Best Source | Our Implementation | Added Value |
|---|---------|-------------|--------------------|-------------|
| 1 | Account status and credits | MCP get_account_status | pingenerator-pp-cli get-account-status | --select, typed exit codes |
| 2 | Connected Pinterest profiles | MCP list_pinterest_profiles | pingenerator-pp-cli list-pinterest-profiles | agent output |
| 3 | Boards for a profile | MCP list_boards | pingenerator-pp-cli list-boards | --select ids for scripting |
| 4 | Scheduled pins queue | MCP list_scheduled_pins | pingenerator-pp-cli list-scheduled-pins | agent output |
| 5 | Sitemap posts | MCP find_website_posts | pingenerator-pp-cli find-website-posts | --limit, scripting |
| 6 | Post content | MCP get_post_content | pingenerator-pp-cli get-post-content | feeds pin-post |
| 7 | Connected stores | MCP list_connected_stores | pingenerator-pp-cli list-connected-stores | agent output |
| 8 | Store products | MCP list_store_products | pingenerator-pp-cli list-store-products | paging flags |
| 9 | Templates | MCP list_templates | pingenerator-pp-cli list-templates | offline-friendly select |
| 10 | Generate template pin image (1 credit) | MCP generate_pin_image | pingenerator-pp-cli generate-pin-image | dry-run default, --execute |
| 11 | Generate and schedule template pin (1 credit) | MCP generate_and_schedule_pin | pingenerator-pp-cli generate-and-schedule-pin | dry-run default, --execute |
| 12 | Schedule an existing image (1 credit) | MCP schedule_pin | pingenerator-pp-cli schedule-pin | dry-run default, --execute |
| 13 | Design profiles | MCP list_design_profiles | pingenerator-pp-cli list-design-profiles | agent output |
| 14 | Set up design profile | MCP setup_design_profile | pingenerator-pp-cli setup-design-profile | --dry-run |
| 15 | Update design profile | MCP update_design_profile | pingenerator-pp-cli update-design-profile | --dry-run, repeatable arrays |
| 16 | AI pin image (12-24 credits) | MCP generate_ai_pin_image | pingenerator-pp-cli generate-ai-pin-image | dry-run default, --execute |
| 17 | AI pin generate and schedule (13-25 credits) | MCP generate_and_schedule_ai_pin | pingenerator-pp-cli generate-and-schedule-ai-pin | dry-run default, --execute |
| 18 | Automations | MCP list_automations | pingenerator-pp-cli list-automations | agent output |
| 19 | Create automation | MCP create_automation | pingenerator-pp-cli create-automation | --dry-run, JSON boards |
| 20 | Run automation batch (credits per pin) | MCP run_automation_batch | pingenerator-pp-cli run-automation-batch | dry-run default, --execute |
| 21 | Batch status | MCP get_automation_batch_status | pingenerator-pp-cli get-automation-batch-status | agent output |
| 22 | Credit guard on spending tools | prior pingenerator-pp-cli | (behavior in pingenerator-pp-cli generate-pin-image) dry-run unless --execute; zero network calls in dry run | user-mandated safety |
| 23 | Dry-run for free-but-stateful tools | prior pingenerator-pp-cli | (behavior in pingenerator-pp-cli create-automation) --dry-run prints the JSON-RPC request | safe previews |
| 24 | Offline tool catalog | prior pingenerator-pp-cli | pingenerator-pp-cli tools | no key, no network |
| 25 | Health check | prior pingenerator-pp-cli | pingenerator-pp-cli doctor | key + reachability |
| 26 | Output shaping | prior CLI + generator | (behavior in pingenerator-pp-cli list-boards) --agent/--compact/--select dotted paths | context-efficient |
| 27 | Typed exit codes (2 usage, 3 auth, 4 credits, 5 rate limit) | prior pingenerator-pp-cli | (behavior in pingenerator-pp-cli get-account-status) | scriptable |
| 28 | Raw JSON-RPC escape hatch | generator endpoint mirror | (generated endpoint) mcp rpc | any future tool reachable |

Note on row 24 (resolved in Phase 3, 2026-10-29): the approval-time draft named `mcp-tools` in case `tools` was a reserved framework command. The generated v4.34.2 tree does not reserve `tools`, so the prior command name `tools` is kept unchanged. No alias was added.

### Transcendence (only possible with our approach)
| # | Feature | Command | Score | Buildability | How It Works | Evidence | Long Description |
|---|---------|---------|-------|--------------|--------------|----------|------------------|
| 1 | Post to pin | pin-post | 9/10 | hand-code | get_post_content → generate_and_schedule_pin (or AI with --ai); dry-run prints resolved args and cost; --execute fires and appends to ledger | Top Workflow 2; AppSumo auto-pin demand; prior credit guard | Use this command to turn blog or website post URLs into scheduled pins. Do NOT use it for store products; use 'pin-products' instead. |
| 2 | Plan preflight | preflight | 9/10 | hand-code | Prices planned calls from the tool cost table, checks remaining credits, and validates that profile, board and design profile exist; never calls a spending tool | User Vision (no spend); credit table | Use this command to validate and price a plan before running it. Do NOT use it for an ongoing automation burn forecast; use 'credit-runway' instead. |
| 3 | Products to pins | pin-products | 8/10 | hand-code | Pages list_store_products, plans one generate_and_schedule_pin per product, skips ledgered products; dry-run total | Top Workflow 3; seller users | Use this command to pin products from a connected store. Do NOT use it for blog posts; use 'pin-post' instead. |
| 4 | Unpinned posts | unpinned-posts | 8/10 | hand-code | Joins find_website_posts with list_scheduled_pins and the ledger on normalized URL, falling back to title | Top Workflows 2 and 5 | Use this command to find sitemap posts that have no scheduled or ledgered pin. Do NOT use it to audit spending; use 'ledger' instead. |
| 5 | Spend ledger | ledger | 7/10 | hand-code | Local append-only JSONL of --execute spends; filter by --since/--tool, total credits | Credit-metered tools; agent personas | Use this command to see what this CLI has spent. Do NOT use it to find unpinned posts; use 'unpinned-posts' instead. |
| 6 | Queue gaps | queue-gaps | 7/10 | hand-code | Groups list_scheduled_pins by day and board; flags empty and overloaded days | Top Workflow 5; VA users | none |
| 7 | Credit runway | credit-runway | 6/10 | hand-code | Joins list_automations cadence with remaining credits; min/max weekly burn and days to exhaustion | Credit profile; VA users | Use this command for the recurring automation burn forecast. Do NOT use it to price a one-off plan; use 'preflight' instead. |

Low-confidence flags: the response field names of `list_scheduled_pins`, `list_automations` and `get_account_status` were not re-verified live in this run. Commands 4, 6 and 7 parse those fields tolerantly (multiple candidate keys), and offline fixtures document the assumed shapes. Live proof is still owed.

Hand-code commitment: 21 typed tool commands (ported from the prior CLI) + 7 transcendence commands + the JSON-RPC client package. 0 spec-emits beyond the `mcp rpc` passthrough.

Killed in the cut (full reasons in the brainstorm file): batch-wait, snapshot, products-export, variants, write-copy (LLM dependency), pin-stats (external Pinterest API), clients (credential handling), design-diff.
