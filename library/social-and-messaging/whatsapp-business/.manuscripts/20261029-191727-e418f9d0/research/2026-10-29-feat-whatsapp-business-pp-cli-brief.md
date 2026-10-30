# WhatsApp Business Tools CLI Brief

Status: authenticated catalog discovered (24 tools); read-only businesses and ping smokes passed. No CLI generated.

## API Identity
- Target: official WhatsApp Business Tools MCP, https://mcp.facebook.com/whatsapp_business_tools.
- Source: https://developers.facebook.com/documentation/mcp/whatsapp-business-tools-mcp (raw Markdown HTTP 200).
- Users: developers and business administrators operating their own Meta businesses and WhatsApp-enabled apps.
- Transport: Streamable HTTP JSON-RPC; documentation describes 18 tools. Exact input/output schemas must come from authenticated tools/list, not guessed REST mappings.
- Required roles: business MANAGE, app admin, accepted WhatsApp Cloud API terms. Account availability remains unverified.

## Reachability Risk
- High until authenticated discovery succeeds: beta gradual rollout; anonymous initialize returns 401; GET returns 405 as expected.
- Protected-resource and authorization-server metadata return HTTP 200. OAuth uses authorization_code/refresh_token, S256 PKCE, bearer header, and business_management / whatsapp_business_management / whatsapp_business_messaging scopes.
- Own dynamic registration returned HTTP 400 invalid_client_metadata: Dynamic registration is not available for this client.
- Native Codex OAuth successfully created an authorization URL using its client identity; authentication succeeded and the native catalog returned 24 tools.
- Do not treat a Graph API system-user token as a validated MCP credential. Do not silently replace this service with Cloud API endpoints.

## Customer Model / User Research
The target user maintains WhatsApp Business integrations and needs to trace the business → account → phone hierarchy before changing setup. Agency operators may administer several businesses and apps; an incorrect target can affect real messaging. A developer diagnosing onboarding needs prerequisites and current state in one view. Template managers need to review account-specific approved templates before preparing a send. These personas derive from the official prerequisites, discovery ordering, and tool descriptions, rather than interviews.

Pain points supported by the source contract:
1. Business and app admin access are distinct; missing prerequisites block otherwise plausible operations.
2. Phone onboarding spans add, challenge, verify, and register states, making the next required step unclear without state inspection.
3. Template approval and free-form messaging eligibility differ; a CLI must preserve the server's delivery restrictions.
4. OAuth must be repeated after client restart per the current official guide; do not promise persistent access before testing refresh behavior.
5. Beta tool-set changes require catalog/schema drift detection rather than hardcoded undocumented assumptions.

## Top Workflows
1. Discover businesses, accounts, and phone states with explicit target identifiers.
2. Diagnose registration readiness and prepare the next onboarding operation.
3. List and inspect templates, preview a create/update/delete operation.
4. Prepare text/template sends with recipient review and explicit execution confirmation.
5. Inspect webhook and business setup capabilities, prepare configuration changes.

## Table Stakes
- All documented MCP capabilities, subject to live catalog confirmation.
- OAuth, schema-aware inputs, useful error reporting, JSON output and explicit write preview/confirmation.
- Respect MCP sessions, protocol negotiation, JSON and event-stream responses, rate-limit headers, and JSON-RPC/tool errors.
- Never claim a messaging-history or customer-service-window database: no history/list-messages tool is documented.

## Competitor Evidence
- Official incumbent: remote Meta MCP itself, the exact target service; no standalone CLI in its guide.
- Cursor WhatsApp Business plugin: https://github.com/cursor/plugins/blob/main/third_party/whatsapp-business/README.md. Cursor-hosted server, per-member account/phone/token context, messages/templates/contacts/phone discovery. It explicitly exposes no reusable public MCP endpoint. Its server schema cannot substitute for Meta's.
- PyWa: https://github.com/david-lev/pywa and https://pypi.org/project/pywa/. Python Cloud API framework with bot hosting, messaging, templates and broader resource operations. Adjacent product; direct Cloud API functionality beyond the requested MCP is outside scope unless approved.
- WhatsApp/WhatsApp-Nodejs-SDK is archived; repository metadata verified. Closed issue #31 announces pausing development. This SDK is unsuitable as the dependency for the new client.
- Public Printing Press registry checked (598 entries); no same-product WhatsApp Business entry. Respond.io and Unipile are separate provider services. blocked-apis.json HTTP 404, non-blocking.

## Data Layer
- Candidate local snapshots: tool catalog/schemas, business/account/phone inventory and templates, each labeled with capture time and scope.
- No documented change cursor; use explicit full read snapshots until the live schema proves pagination or cursors.
- Offline search can cover only actually captured entities. Never fabricate webhook state, message delivery state, payment readiness or verification status.
- Keep credentials outside research, source and archived evidence; persist live test outcomes without customer identifiers.

## Product Thesis
- Name: whatsapp-business-pp-cli (working slug; final scope awaiting absorb gate).
- Value: a terminal client for the official MCP with explicit targeting and reviewable writes, plus local evidence for integration diagnosis.
- Runtime must remain pure HTTP; no resident browser sidecar.

## Build Priorities
1. Obtain authenticated catalog and verify a low-risk discovery read.
2. Model the actual MCP contract; retain evolving schemas and correct tool names.
3. Review the absorb manifest and proposed differentiators with Bobe before generation.
4. Generate and implement the approved scope, then complete live read-only dogfood and shipcheck before promotion.

## User Vision
Bobe approved the scope of the linked documentation and authorized OAuth preparation plus read-only live tests. Live writes, messaging, onboarding changes and publication are not authorized by that approval.

## Authenticated Contract Update
The live catalog adds ping, onboarding_status, template_library, create_template_from_library, manage_templates_archival and publish_app. The catalog provides exact inputSchema/outputSchema and readOnlyHint annotations. Onboarding status can read webhook configuration and app publication state; earlier documentation-only uncertainty is resolved. Account and phone discovery expose separate owned/shared cursors; status exposes apps_after/accounts_after. Preserve partial-versus-total semantics.

## Generator Contract Decision
No official OpenAPI exists for this MCP. Build a faithful OpenAPI transport document with the real POST /whatsapp_business_tools operation and JSON-RPC request/result envelopes. Typed CLI tool commands must be hand-written on top of that transport using the authenticated tool schemas; never invent REST routes or claim unrelated Cloud API parity. OAuth interoperability is an implementation requirement: own DCR failed, native Codex login worked. Offer explicit use of that existing local OAuth context; independently registered clients require their own authorized client metadata. Do not borrow another application's identity for a custom OAuth implementation.

## Wrapper Issue Check
PyWa's issue search returned closed compatibility incidents for non-JSON errors (#230), a deprecated phone-number field (#221) and a removed API field (#214). These support robust errors and schema drift checks, not a claim that the official MCP is blocked. The archived official Node SDK has no open issues and is not selected as a dependency.

## Users
- Agency integration maintainer: revisits several authorized business/app configurations each week, checks the exact scope before preparing changes, and needs to distinguish owned and partner-shared accounts. Grounded in the official admin prerequisites and the live accounts/phone schemas.
- WhatsApp integration developer: resumes unfinished onboarding and webhook debugging, checks app-specific publication plus account subscriptions, and needs unknown states preserved. Grounded in the live onboarding_status checklist and its per-app/per-account caveats.
- Template operations owner: reviews approved templates for the selected account, prepares changes and sends for review, and needs a stable content comparison across captures. Grounded in the template and send schemas, not assumed message-history access.

## Ecosystem Source Reads
- Gonzalez8/whatsapp-business-mcp actual src/api.mjs and src/tools/templates.mjs: direct Graph API bearer auth; template GET/POST/DELETE resource calls; read-only/destructive annotations; cursor handling. This validates overlapping needs, but its REST paths are not the requested Meta MCP protocol.
- saravanaspar/WBMCP actual src/security/toolGuards.ts and src/whatsapp/graphClient.ts: boolean confirm/dryRun controls; response status handling; Retry-After parsing; retries limited to read-safe GET. Reuse those behavioral requirements, not its separate Graph API transport or expanded product scope.
- anthropics/claude-plugins-official external_plugins listing checked: no WhatsApp Business entry.
- Additional searches covered Claude plugins, MCP directories, SKILL.md packages, CLI/automation repos, npm and PyPI. Relevant adjacent services (Twilio, Wassenger, Wabery, Kapso, Respond.io, Unipile) require different provider credentials and are outside the requested official MCP scope.
- DeepWiki for david-lev/pywa was inaccessible through the web tool; no DeepWiki assertions used.

## Read-only Live Evidence
Native OAuth catalog discovery passed with 24 schemas. Direct authenticated Streamable HTTP initialize, whatsapp_biz_businesses(action=list) and ping all passed. Six business rows were returned; identifiers and names were not persisted to proofs. This proves current access for those operations, not account setup completeness or success of any mutation.
