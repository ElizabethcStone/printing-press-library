# Crocoblock CLI research brief

Date: 2026-10-20 (America/Monterrey). Build-driving research, not a shipping claim.

## User Vision
The user selected WordPress plugin operations: JetEngine, forms, bookings and filters, and authorized the necessary WP Engine work. This is one Crocoblock ecosystem integration, not a vendor-account/license CLI. The API lives on each WordPress site and depends on plugin versions, enabled modules, registered CPT/CCT types, relations and permissions. Proposed binary: `crocoblock-pp-cli`.

The local/public CLI registry was checked: no same-product CLI found; the already-installed WordPress CLI covers core WordPress operations. Reuse its lessons without duplicating its entire content surface. Historical JetEngine research from 2026-07-21 is useful but does not establish current plugin behavior.

## Verified target and reachability
WP Engine API credentials are valid. Live inventory found 17 original environments; read-only SSH plugin inventories completed for all 17. None had JetEngine, JetFormBuilder, JetBooking/JetAppointment or JetSmartFilters. JetWooProductGallery 2.2.2 was present in one existing site production and development only; that is a distinct plugin.
A fresh development install `the-disposable-lab` was created under the existing the existing agency site site through the official WP Engine API (201). It is dedicated to synthetic tests. Its provisioning is now active (PHP 8.4), verified through a fresh live inventory. No original environment was modified.
WordPress CLI's old Frikko Application Password returned 401 incorrect_password; it was not used as a credential source for the new lab. SSH access works with the existing WP Engine key. The new lab needs a separate Application Password kept outside run artifacts.
The user confirms a Crocoblock license and will sign in to the account browser. Official paid packages are still pending. JetFormBuilder 3.6.7 is available freely at WordPress.org; its package metadata and official source repository were inspected. Vendor account access will be used only to retrieve entitled packages, never to archive license keys or session secrets.

## Source and contract evidence
- REST overview: https://crocoblock.com/knowledge-base/features/rest-api-overview/ (raw HTTP 200).
- CCT create/update and Application Password guide: https://crocoblock.com/knowledge-base/jetengine/jetengine-how-to-add-and-edit-cct-items-remotely-using-rest-api/ (raw HTTP 200). It still describes update as replacement of the item's data. Any partial-update CLI must preserve other fields and validate this live with current plugin source.
- Native MCP guide: https://crocoblock.com/knowledge-base/jetengine/how-to-use-mcp-server-with-vs-code-copilot/ (raw HTTP 200), documents `/wp-json/jet-engine/v1/mcp/` and Basic authorization using a WordPress Application Password.
- JetAppointment query: https://crocoblock.com/knowledge-base/jetengine/appointments-query-in-the-query-builder-overview/ (raw HTTP 200). Query Builder can register a custom namespace/path with restricted roles/capabilities and allowed arguments. Do not infer a universal appointments CRUD URL from this.
- JetBooking workflow: https://crocoblock.com/knowledge-base/jetbooking/how-to-display-bookings-dynamically-in-a-calendar/ . Documents JetBooking Query, date/status filters and JetFormBuilder booking fields. Actual administration routes require current source/live evidence.
- Filter indexer: https://crocoblock.com/knowledge-base/jetsmartfilters/jetsmartfilters-how-to-set-indexer-option-for-filters/ (raw HTTP 200). Auto reindex does not trigger for posts updated through REST/WP-CLI or created by JetFormBuilder. A safe workflow must report stale index state and offer a verified reindex operation.
- Official JetFormBuilder source: https://github.com/Crocoblock/jetformbuilder , inspected commit `0d93a7b937f7afa53f4e341edb422193c82124f3`, version 3.6.7. WordPress.org plugin metadata independently reports 3.6.7, updated 2026-10-14, requires WordPress 6.1 / PHP 7.0.

### JetFormBuilder source-confirmed details (live validation pending)
`components/rest-api/rest-api-endpoint-base.php` fixes namespace `jet-form-builder/v1`.
`modules/rest-api/endpoints/get-form-fields.php` defines GET `/{id}/fields`, requiring form-edit or configured records permission; returns `fields` entries with value/label/type and excludes secure parsers. It can return 204 for no fields.
`modules/form-record/rest-endpoints/fetch-records-page-endpoint.php` defines GET `/records/fetch-page`, response `{list,total}`. Its pagination trait takes `limit` (default 5), `page` (default 1), `sort`, and `filters` from query arguments. Record access requires `manage_options` or configured records-access capability.
`modules/post-type/module.php` registers the form CPT with `show_in_rest=true`, custom capability type `jet_fb_form` and a `Forms_Post_Type_Controller`. That controller forces edit-context permission checks even for reads because form configuration may contain sensitive settings. Never downgrade this permission requirement or expose raw action secrets by default.

## Users
These are concrete design personas derived from the requested plugin scope and the vendor's documented workflows; they are not claims about the operator’s personal schedule.
- Agency WordPress implementer: maintains JetEngine structures on client sites, checks the installed modules and fields before data changes, and repeats data/schema checks around weekly releases. Today the workflow spans wp-admin, Query Builder, REST documentation and one-off scripts. The vendor CCT partial-update warning does not reproduce on the installed JetEngine 3.9.0: the live PUT preserved omitted fields. Version-specific checks are required.
- Forms operations maintainer: reviews JetFormBuilder form fields, record status and downstream actions while maintaining inquiry and booking workflows. Today they switch between the Gutenberg form editor, stored records, action settings and filter indexer. They need to see which data edits leave filters stale and which fields/actions are affected, without exposing action credentials.
- Reservation site maintainer: checks date bookings or time appointments, status and provider/unit configuration during recurring site maintenance. Official JetBooking/JetAppointment guides and Crocoblock/suggestions#8302 show these are different models with distinct workflow effects. Do not fabricate a unified availability rule or assume every overlap is an error; capacity and valid-status semantics matter.

## Top Workflows
1. `discover` / `doctor`: identify installed supported plugins, versions, registered namespaces, permission gaps and real field schemas. Distinguish absent plugin, disabled REST surface, invalid credentials and denied capability.
2. `cct` / `relations`: query all pages, inspect one record, preview a field-level edit that retains omitted fields, traverse relation parents/children. Relation updates must make replacement vs append/disconnect explicit.
3. `forms` / `records`: list and inspect form schemas, preserve Gutenberg source on edits, audit configured actions with redaction, inspect submission records and export explicitly selected fields. Sending mail or initiating payment is not a default smoke test.
4. `bookings` / `appointments`: inspect availability, records and conflicts; preview changes with dates/timezone and effect before execution. Actual write coverage is conditional on verified official plugin interfaces.
5. `filters`: inspect filter definitions and dependencies, detect index maintenance needs after data edits, preview/apply reindex only after validating the real supported entrypoint.

## Competitors and table stakes
- WP-CLI: mature server-side WordPress operations, but requires shell access and lacks a generic typed Crocoblock integration. We have it in WP Engine for controlled lab preparation and source inspection.
- JetEngine's own MCP/Command Center: strongest direct incumbent for creating structures and examining site context. CLI should complement it with repeatable bulk workflows, stable JSON, previews, local comparisons and cross-plugin audits. Do not claim the native MCP is read-only or incapable of bulk work without runtime verification.
- WordPress PP CLI already provides generic core CRUD and site registration. Avoid advertising those as Crocoblock-specific innovation.
Searches for dedicated Crocoblock npm/PyPI clients returned no useful match; that is bounded search evidence, not proof none exists.
Official JetFormBuilder issues were checked for 403/blocked/broken/deprecated/rate-limit complaints. Results were predominantly editor/form compatibility bugs; no evidence from this bounded search establishes widespread API reachability failure. Relevant current examples: #698 WYSIWYG inside Listing Grid (open), #694 advanced date validation (closed), #701 WordPress 7.1 advanced choices (closed). Version-aware live testing matters.

## Concrete pain points
- Vendor documentation warns about omitted CCT fields, but the live JetEngine 3.9.0 test preserved them; comparisons should verify data rather than claim a current demonstrated loss bug.
- Schema and routes vary by installation and module, so a fixed guessed endpoint catalog can misrepresent capabilities.
- Filters may remain stale after REST, WP-CLI or JetFormBuilder writes; successful data mutation alone does not establish correct frontend results.
- Form settings may contain sensitive action configuration and must not be blindly dumped or indexed.

## Data layer
Candidate entities: site capabilities and schemas, CCT items (site/type/key), relation edges, redacted form definitions, selected form-record fields, bookings/appointments, filter dependencies and index check state. Keep site separation explicit. Sync record PII only through an explicit workflow and preserve raw block markup when needed for export. Distinguish WP pagination from plugin pagination. No incremental sync guarantee until confirmed cursor semantics exist; relations may require full comparison.
High-value local queries: orphan relations, cross-type search, schema drift, booking overlaps and stale filters. Store enough provenance (site, plugin version, sync time) to avoid presenting stale local answers as live.

## Product thesis and build priorities
A capability-aware Crocoblock CLI that safely operates real installed plugin contracts and explains cross-plugin effects. Primary value is reliable data edits and audits across CCTs, relations, forms, bookings and filters, with stable agent output.
1. Bring up the dedicated lab and install current official entitled plugin packages; preserve secrets outside run artifacts.
2. Inventory routes and OPTIONS schemas plus source permission semantics; establish one source-backed spec and explicit capability matrix.
3. Build useful reads and previews, then data-safe writes tested on disposable fixtures with outbound email/payment effects suppressed.
4. Add local mirror/diff and cross-plugin consistency checks where the API lacks them.
5. Complete required live dogfood and promotion gates; no CLI is shippable before those pass.

## Outstanding evidence
Official paid packages are now installed. Basic REST auth, native MCP tools/list, CCT CRUD, filter CRUD and form CRUD are verified. Booking/appointment mutations, relation traversal and record-field capture still require their own proofs. No absorb scope is approved yet.

## Ecosystem absorption research (2026-10-20)
The required bounded searches covered Claude plugins/skills, MCP, CLI, npm, PyPI, marketplace directories, GitHub automation and the anthropics/claude-plugins-official external_plugins listing. No dedicated same-product CLI or useful npm/PyPI client was found. The Anthropic listing has no Crocoblock entry. Unrelated Elementor migration kits and other Crocoblock plugins are outside the user's explicit four-area scope.
A useful independent source is https://github.com/EPEAKSTUDIO/wp-jetengine-skills (MIT): documentation plus tests of the relevant plugin internals, not a standalone remotely callable API. Its native MCP notes corroborate tool/resource names and distinguish nonce-gated admin routes from the Basic-auth MCP route. It reports eleven native MCP features: add CCT, CPT, taxonomy, meta box, query, listing, glossary; manage modules; read configuration, website configuration and macros. Its July 2026 tests are evidence to recheck, not proof of today's lab behavior. Read source directly from official paid packages before committing endpoint payloads.
DeepWiki returned no accessible wiki for Crocoblock/jetformbuilder; no semantic findings were used.
The current public laboratory REST index responds via index.php?rest_route=/ with 28 JetFormBuilder-related routes. Its initial plain permalink setup made /wp-json/ return HTML. Python's default User-Agent received Cloudflare 1010; an honestly identified CLI User-Agent reached the site normally, without changing any security rule. Dedicated noindex/robots and outbound-mail suppression guards are installed for synthetic fixtures.

## Authenticated upstream contract proof
The dedicated lab Application Password succeeds with admin capability. GET /wp-json/ returns HTTP200 and 28 JFB-related routes; forms collection and records/fetch-page return200. `discovery/lab-auth-smoke.json` records sanitized checks.
A synthetic draft was created via REST (201), read in edit context (200), published only inside the isolated lab, read through /jet-form-builder/v1/{id}/fields (200), updated by title with unchanged raw blocks (200), tested anonymously (401 rest_forbidden_context), then trashed (200). No form submission or mail action ran. `discovery/jfb-contract-smoke.json` records checks. This is upstream contract discovery, NOT generated CLI dogfood.
The field route returns204 for drafts despite nonempty block source: Get_Form_Fields calls Block_Helper::get_blocks_by_post, whose is_valid_form_post requires publish. Therefore draft inspection must read raw blocks and explain scope; 204 does not prove no configured fields. Earlier fixtures5 and7 plus final fixture9 were all trashed. There are no published test forms remaining.

## Additional MCP source evidence
https://github.com/wpmcp/wpmcp is a public WordPress MCP implementation. Its src/Integrations/JetEngine_Pack.php supplies three relevant operations: list declared post-type fields (name/title/type, excluding layout/HTML), get field values for a post, update declared field values. It rejects unknown field keys and HTML write targets; writes are opt-in and snapshotted for restoration. Source uses local PHP APIs, not a separate Crocoblock HTTP API. Its MCP transport is /wp-json/mcp/wpmcp-server with a separate OAuth surface, so do not assume this plugin is installed or these routes exist on a normal JetEngine site. Absorb field validation and reversible write behavior using verified native interfaces; any exposure requirement must be explicit. src/MCP/Transport_Guard.php confirms no-store headers and host mismatch handling. No third-party plugin was installed.

## Current official-package evidence (2026-10-20 local / 2026-10-21 UTC)
Authenticated account downloads installed JetEngine 3.9.0, JetBooking 4.1.3.1, JetAppointment 2.5.3 and JetSmartFilters 3.8.6.2 on the isolated development lab. Inventory hashes are in discovery/official-package-inventory.json. Initial full index registered 114 Jet routes; enabling CCT adds installation-specific routes. tools/list returned eleven native MCP tools, schemas preserved in discovery/native-mcp-tools.json. Use POST /jet-engine/v1/mcp with Basic auth; the alternate /mcp-tools/run route also demands a nonce and is not the runtime choice.
The CCT pp_probe definition was created with explicit manage_options REST capabilities. Create/read/partial-update/delete passed, omitted values survived and anonymous reads returned 401. Filter create/get/title-update/trash passed, metadata survived. Proof: discovery/paid-contract-smoke.json. Native configuration intentionally removes some admin flags; complete capture must use dedicated admin definition routes for those fields.
Booking add can silently adjust reversed/equal dates; reject them in the client first. Appointment add can return success:true with text saying the time is taken; success requires scoped read-back. Filter collection POST can both read and mutate depending on keys; read adapters must allowlist read keys. Reindex has no direct REST route; use explicitly configured SSH/WP-CLI and public index_filters(), with identity and result checks. These are source-derived implementation requirements, not proof that the future CLI exists.
