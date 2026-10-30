## Customer model

**Persona 1: Dana, recipe blogger on WordPress**

**Today (without this CLI):** Dana publishes two or three recipes a week. For each one she opens the Pin Generator web app, pastes the post URL, picks a template, generates a pin, chooses a board and queues it. Lately she has also asked Claude, through the vendor's beta MCP server, to "pin my latest post", but each chat starts from nothing. She cannot easily answer "which of my sitemap posts have never been pinned?" or "how many credits will this week's pins cost before I click generate?"

**Weekly ritual:** Publish post → `find_website_posts` / `get_post_content` → `generate_and_schedule_pin` (template, 1 credit) or `generate_and_schedule_ai_pin` (13-25 credits) → check `list_scheduled_pins`. This is Top Workflow 2.

**Frustration:** Copying title, image and URL from the post into the pin by hand for every post, with no way to tell which posts are still unpinned. The vendor's own support steers people like her toward sitemap automations for auto-pinning new posts (AppSumo Q&A).

**Persona 2: Marco, Etsy and Shopify home-goods seller**

**Today (without this CLI):** Marco has connected his Etsy and Shopify stores to Pin Generator. He looks through products page by page in the web UI and pins bestsellers one at a time. He cannot answer "which products did I already pin this month?" or "what will pinning the whole spring collection cost?"

**Weekly ritual:** `list_connected_stores` → `list_store_products` (search, paged) → generate and schedule a pin for each chosen product (Top Workflow 3).

**Frustration:** Turning a filtered set of products into a batch of scheduled pins without retyping each product's title, photo and link, and without accidentally re-pinning (and paying for) products he has already done.

**Persona 3: Priya, Pinterest VA for several small-brand clients**

**Today (without this CLI):** Priya keeps a separate Pin Generator account and access key for each client. She sets up design profiles and weekly or 28-day automations (Top Workflows 4 and 5), then checks each account's queue and credit balance by hand. She cannot answer "which days next week have no pins for this client?" or "at the current automation cadence, when does this client run out of credits?"

**Weekly ritual:** For each client: `get_account_status` → `list_automations` → `run_automation_batch` / `get_automation_batch_status` → review `list_scheduled_pins` for gaps and pile-ups.

**Frustration:** She has no view of each client's schedule laid out by day and board, and no warning about credit burn. She finds out a client is out of credits only when a batch fails.

**Persona 4: Sam, solo operator running Claude Code pipelines**

**Today (without this CLI):** Sam drives Pin Generator from agents through the beta MCP server. That path has no dry-run guard and nothing is scriptable or reproducible (see Competitors in the brief). Sam cannot answer "did yesterday's agent run already spend credits on this URL?" or "will this plan fail on a bad board ID before it spends anything?"

**Weekly ritual:** A scheduled agent run that reads new posts or products, generates pins and schedules them, then reports what was spent.

**Frustration:** Non-idempotent, credit-spending agent runs. A retry can double-spend, and nothing checks a plan or keeps a record of spending.

## Candidates (pre-cut)

| # | Name | Command | Description | Persona | Source | Long Description | Inline kill/keep check |
|---|---|---|---|---|---|---|---|
| C1 | Post to pin | `pin-post <post-url>` | Calls `get_post_content`, maps title, image and URL into `generate_and_schedule_pin` (or `generate_and_schedule_ai_pin` with `--ai`). Dry-run by default, needs `--execute`. `// pp:data-source live`, `// pp:client-call` | Dana, Sam | (b) Top Workflow 2 | Use this command to turn blog or website post URLs into scheduled pins. Do NOT use it for store products; use 'pin-products' instead. | Keep. Real tool calls, credit guard kept. |
| C2 | Products to pins | `pin-products --store-id X [--search q] [--all-pages]` | Calls `list_store_products` (auto-paging), then makes one dry-run `generate_and_schedule_pin` per product using its title, photo and link. `// pp:data-source live` | Marco | (b) Top Workflow 3 | Use this command to pin products from a connected store. Do NOT use it for blog posts; use 'pin-post' instead. | Keep. |
| C3 | Unpinned posts | `unpinned-posts --website-url U` | Live join of `find_website_posts` with `list_scheduled_pins` (and the local ledger), matched on destination URL with a fallback to normalized title. Lists posts with no pin. `// pp:data-source live` | Dana, Sam | (c) cross-entity join | Use this command to find sitemap posts that have no scheduled or ledgered pin. Do NOT use it to audit spending; use 'ledger' instead. | Keep. Verifiability flag: `list_scheduled_pins` fields have not been re-checked, so URL matching may fall back to title. |
| C4 | Preflight | `preflight --plan plan.jsonl` (or the same flags as pin commands) | Totals the credit cost of a planned batch from the tool cost table. Checks it against remaining credits from `get_account_status`. Confirms that profiles, boards and design profiles exist via `list_pinterest_profiles`, `list_boards` and `list_design_profiles`. Never spends. `// pp:data-source live` | Sam, Priya | (e) User Vision (no credit spend), (a) | Use this command to validate and price a plan before running it. Do NOT use it for an ongoing automation burn forecast; use 'credit-runway' instead. | Keep. |
| C5 | Spend ledger | `ledger [--since 7d] [--tool T]` | Local append-only JSONL with one line per `--execute` credit spend (tool, key args, estimated credits, returned IDs). Lets pin commands skip already-pinned URLs and product IDs. `// pp:data-source local` | Sam, Marco | (a), (e) | Use this command to see what this CLI has spent. Do NOT use it to find unpinned posts; use 'unpinned-posts' instead. | Keep. Cheap local state, not a sync. |
| C6 | Queue gaps | `queue-gaps [--days 14] [--per-day N]` | Groups `list_scheduled_pins` by day and board, then flags empty days and days above N. `// pp:data-source live` | Priya, Dana | (c) | none | Keep. Aggregates one read, which is more than a rename. |
| C7 | Credit runway | `credit-runway` | Joins `list_automations` (pins per day, cadence, design profile, so AI or template) with `get_account_status` remaining credits, then estimates weekly burn as a min/max range and days until exhausted. `// pp:data-source live` | Priya | (c), (b) | Use this command for the recurring automation burn forecast. Do NOT use it to price a one-off plan; use 'preflight' instead. | Keep. Verifiability flag: automation credits are "per pin", so the output is shown as a range. |
| C8 | Batch wait | `batch-wait <batch-id>` | Polls `get_automation_batch_status` until the batch finishes. `// pp:data-source live` | Priya, Sam | (b) | none | Scope is fine as a single poll loop, but it wraps only one tool. |
| C9 | Account snapshot | `snapshot` | One call fans out to account status, profiles, scheduled-pin count and automations. `// pp:data-source live` | Priya | (a) | none | Keep for scoring. Overlaps `doctor` (already covered). |
| C10 | Product export | `products-export --store-id X --csv` | Pages through every `list_store_products` page and writes CSV. `// pp:data-source live` | Marco | (c) | none | Keep for scoring. |
| C11 | Template variants | `variants <post-url> --count N` | Generates N template pins for one post (`RandomTemplate` × N), dry-run by default. `// pp:data-source live` | Dana | (b) | none | Keep for scoring. Multiplies credits, and the brief has no evidence of demand. |
| C12 | AI pin copy | `write-copy <post-url>` | Writes Pinterest titles and descriptions from post content. | Dana | (a) | none | **Kill: LLM dependency.** The mechanical part is already `get-post-content`. |
| C13 | Pin performance | `pin-stats` | Shows impressions and saves for scheduled pins. | Dana, Priya | (a) | none | **Kill: external service.** Needs the Pinterest v5 API, which is not in the spec and is covered by the separate `pinterest` CLI. |
| C14 | Client key switcher | `clients use <name>` | Stores several client access keys and switches between them. | Priya | (a) | none | **Kill: auth/scope.** Handles credentials locally and overlaps framework `auth`/`profile`. |
| C15 | Design profile diff | `design-diff A B` | Compares the colors, mood and keywords of two design profiles. | Priya | (b) | none | Keep for scoring. Niche. |

## Survivors and kills

Pass 3 answers for each survivor (1 Weekly use, 2 Wrapper vs leverage, 3 Transcendence proof, 4 Sibling kill, 5 Buildability, 6 Long-description validity):

- **pin-post**
  1. Dana runs it every time she publishes, 2-3 times a week.
  2. Leverage: it chains two tools and maps fields between them, and it has an `--ai` mode.
  3. The MCP server needs two calls plus copying fields by hand, and has no dry-run.
  4. It absorbs C11 (variants become `--template` repeats, if wanted later).
  5. hand-code.
  6. Valid, because pin-products survives.
- **pin-products**
  1. Marco uses it weekly.
  2. Leverage: auto-paging, one plan per product, and a ledger-based skip.
  3. No existing tool goes from products to pins in one step.
  4. It absorbs C10 (`--all-pages`).
  5. hand-code.
  6. Valid.
- **unpinned-posts**
  1. Dana uses it weekly. Sam uses it on every scheduled agent run.
  2. Leverage: it joins two reads plus the ledger.
  3. It answers "which posts are unpinned?", which no single tool can.
  4. No sibling to absorb.
  5. hand-code.
  6. Valid, because ledger survives.
- **preflight**
  1. Sam runs it before every agent run.
  2. Leverage: it reads 3-4 tools and adds a cost table.
  3. It catches bad board IDs or too few credits before anything is spent, which supports the User Vision.
  4. It absorbs the validation idea.
  5. hand-code.
  6. Valid, because credit-runway survives.
- **ledger**
  1. Sam and Marco use it weekly.
  2. This is local state, not a wrapper.
  3. The API has no spend history in the MCP surface.
  4. No sibling to absorb.
  5. hand-code.
  6. Valid.
- **queue-gaps**
  1. Priya uses it weekly for each client.
  2. Leverage: grouping and threshold logic on top of the read.
  3. The raw list cannot show empty days.
  4. It absorbs the queue part of C9.
  5. hand-code.
  6. none.
- **credit-runway**
  1. Priya uses it weekly.
  2. Leverage: it joins automations and account status and does the arithmetic.
  3. Nothing in the existing commands forecasts credit exhaustion.
  4. It absorbs the credits part of C9.
  5. hand-code.
  6. Valid, because preflight survives.

### Survivors

| # | Feature | Command | Score | Buildability | How It Works | Evidence | Long Description | Persona served |
|---|---|---|---|---|---|---|---|---|
| 1 | Post to pin | `pin-post <post-url> [--ai] [--template T] --pinterest-profile P --board-id B [--execute]` | 9/10 (D3 P2 F2 R2) | hand-code | Calls `get_post_content`, maps title, first image and URL into `generate_and_schedule_pin` (or `generate_and_schedule_ai_pin` with `--ai`) through the MCP client. Dry-run prints the resolved arguments and cost (1 or 13-25 credits). `--execute` fires the call and appends to the ledger. No external dependencies. | Brief Top Workflow 2. AppSumo Q&A shows demand for auto-pinning new posts. The prior CLI's credit guard contract. | Use this command to turn blog or website post URLs into scheduled pins. Do NOT use it for store products; use 'pin-products' instead. | Dana, Sam |
| 2 | Products to pins | `pin-products --store-id S [--search q] [--all-pages] --pinterest-profile P --board-id B [--execute]` | 8/10 (D3 P2 F2 R1) | hand-code | Pages through `list_store_products` (`page`), builds one `generate_and_schedule_pin` call per product (title, photo, link), and skips product URLs already in the ledger. Dry-run by default with a total credit count. No external dependencies. | Brief Top Workflow 3. Users section names Etsy, Shopify and WooCommerce sellers. | Use this command to pin products from a connected store. Do NOT use it for blog posts; use 'pin-post' instead. | Marco |
| 3 | Unpinned posts | `unpinned-posts --website-url U [--limit N]` | 8/10 (D3 P2 F1 R2) | hand-code | Live join of `find_website_posts` with `list_scheduled_pins` and the local ledger. Matches on normalized destination URL, falling back to normalized title, and outputs posts with no pin. No external dependencies. | Brief Top Workflows 2 and 5 (reviewing the queue). AppSumo Q&A on auto-pinning new posts. Low-confidence flag: scheduled-pin fields not re-verified. | Use this command to find sitemap posts that have no scheduled or ledgered pin. Do NOT use it to audit spending; use 'ledger' instead. | Dana, Sam |
| 4 | Plan preflight | `preflight [--plan plan.jsonl \| pin flags]` | 9/10 (D3 P2 F2 R2) | hand-code | Totals credits for the planned calls from the CLI's tool cost table (`tools.go` CreditNote). Compares the total with remaining credits from `get_account_status`. Checks that each profile, board and design profile exists via `list_pinterest_profiles`, `list_boards` and `list_design_profiles`. Never calls a credit-spending tool. No external dependencies. | User Vision: no credit spend, keep dry-run defaults. Brief Table Stakes credit guard. Brief data profile credit costs. | Use this command to validate and price a plan before running it. Do NOT use it for an ongoing automation burn forecast; use 'credit-runway' instead. | Sam, Priya |
| 5 | Spend ledger | `ledger [--since 7d] [--tool T] [--json]` | 7/10 (D2 P2 F2 R1) | hand-code | Reads a local append-only JSONL file written by `--execute` paths (tool, key args, estimated credits, returned IDs, timestamp) and filters or totals it. Pin commands use the same file to skip work already done. No external dependencies. | Brief: credit-metered generation, beta MCP has no dry-run or scripting (Competitors). Agent personas in Users. | Use this command to see what this CLI has spent. Do NOT use it to find unpinned posts; use 'unpinned-posts' instead. | Sam, Marco |
| 6 | Queue gaps | `queue-gaps [--days 14] [--per-day N] [--board B]` | 7/10 (D2 P2 F2 R1) | hand-code | Groups `list_scheduled_pins` by calendar day and board, then lists empty days and days over N. No external dependencies. | Brief Top Workflow 5 ("review the queue with list_scheduled_pins"). VA and agency users in Users. | none | Priya, Dana |
| 7 | Credit runway | `credit-runway` | 6/10 (D2 P2 F1 R1) | hand-code | Joins `list_automations` (pins per day, batch cadence, design profile, which tells AI from template) with remaining credits from `get_account_status`. Outputs min/max weekly burn and days to exhaustion. No external dependencies. | Brief data profile: credits per pin for automation batches, 12-24 for AI images. Low-confidence flag: per-pin automation cost is given as a range. | Use this command for the recurring automation burn forecast. Do NOT use it to price a one-off plan; use 'preflight' instead. | Priya |

### Killed candidates

| Feature | Kill reason | Closest surviving sibling |
|---|---|---|
| C8 Batch wait (`batch-wait`) | It is a thin poll wrapper over one tool (`get_automation_batch_status`) and is better as a `--wait` flag than as its own command. | credit-runway |
| C9 Account snapshot (`snapshot`) | It overlaps `doctor`, and its useful parts (queue and credits) are covered better by queue-gaps and credit-runway. | queue-gaps |
| C10 Product export (`products-export`) | Marco has no weekly need for a CSV, and auto-paging is absorbed by `pin-products --all-pages`. | pin-products |
| C11 Template variants (`variants`) | The brief has no evidence of demand, and it multiplies credit spend per post. | pin-post |
| C12 AI pin copy (`write-copy`) | It needs an LLM (killed by the rubric's LLM-dependency check), and the mechanical extraction is already `get-post-content`. | pin-post |
| C13 Pin performance (`pin-stats`) | It needs an external service (Pinterest v5 API) that is not in the spec and is covered by the separate `pinterest` CLI. | queue-gaps |
| C14 Client key switcher (`clients`) | It means handling credentials locally and duplicates the framework `auth`/`profile` commands. | preflight |
| C15 Design profile diff (`design-diff`) | Weekly use is niche, and the brief has no evidence for it (score below 5/10). | preflight |
