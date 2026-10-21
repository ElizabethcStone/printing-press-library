# n8n CLI Brief

## API Identity
- Domain: Self-hosted / cloud workflow automation (n8n). The Public API (v1, OpenAPI 3.0, spec version 1.1.1 as served by n8n 2.44.4) exposes 105 paths: workflows (incl. versions/history, publish/archive), executions, credentials, tags, variables, projects/folders, data tables, users/roles, audit, insights, source control, community packages, n8n packages (import/export), promotions, log streaming, SSO/LDAP/OTel settings, node/credential type policies, evaluation test runs.
- Users: (1) self-hosting operators running one or more n8n instances (dev/staging/prod), (2) automation builders/consultants maintaining dozens-hundreds of workflows for clients, (3) AI coding agents that read/patch workflows and triage failures.
- Data profile: Workflows are large nested JSON (nodes[], connections{}, settings, pinData). Executions are high-volume (thousands/day), cursor-paginated (default 100, max 250), with optional heavy `includeData`. Credentials are metadata-only (secrets never returned).
- Auth: API key in `X-N8N-API-KEY` header (env `N8N_API_KEY`). Spec also lists BearerAuth/CookieAuth; API key is the canonical CLI path. Base URL is per-instance (self-hosted default `http://localhost:5678`), API under `/api/v1`.

## Reachability Risk
- None for spec/build: official spec bundled by every n8n instance at `/api/v1/openapi.yml`; captured from the official `n8nio/n8n:2.44.4` image. Public source: github.com/n8n-io/n8n packages/cli/src/public-api/v1/openapi.yml (paths are generated at build time; bundled file used for generation).
- Low runtime risk: Public API can be disabled per instance (`N8N_PUBLIC_API_DISABLED`); some endpoints are license-gated (enterprise: projects, variables, source control, SSO, LDAP, log streaming, promotions) and return 403 with a license message on community edition. Preview/Beta tags (policies, n8n packages) may break without a major bump.
- Probe-safe endpoint: `GET /workflows?limit=1`.

## Top Workflows
1. **Failure triage**: "what broke since yesterday?" — list error executions across all workflows, grouped by workflow and failing node, with the error message, then retry or open.
2. **Find usage across workflows**: which workflows use credential X / node type Y / call webhook path Z / reference variable V / call sub-workflow W — impossible in the UI without opening each workflow.
3. **Backup / diff / promote**: export all workflows to git-friendly files, diff local vs instance or instance vs instance (dev→prod), see what changed in a workflow version history.
4. **Hygiene audit**: stale or never-run workflows, active workflows with no recent successful run, unused credentials, orphaned tags, workflows with error workflow unset.
5. **Agent edit loop**: get workflow JSON → patch a node parameter → update → wait for a test execution to finish → check result.

## Table Stakes
(from official `@n8n/cli` 0.24 (n8n-io/n8n packages/@n8n/cli), community MCP servers, and the prior PR #559)
- Full CRUD for workflows, executions (list/get/retry/stop/delete), credentials (list/get/schema/create/delete/transfer), projects + members, tags, variables, data tables + rows, users, audit, source-control pull, package export/import, promotions.
- Workflow activate/deactivate/publish/archive/transfer/tags.
- Output formats: table, JSON, id-only for xargs piping.
- Config via flags > env > config file. Official CLI uses `N8N_URL` + `N8N_API_KEY` — accept `N8N_URL` as an alias of `N8N_BASE_URL` for drop-in compatibility.
- Agent skill file.

## Data Layer
- Primary entities: workflows (with nodes flattened into a node table: workflow_id, node name, type, typeVersion, credentials refs, disabled), executions (id, workflowId, status, mode, startedAt, stoppedAt, finished, error node/message when fetched), credentials (id, name, type), tags, variables, projects, users, data tables.
- Sync cursor: executions by cursor + `startedAt` high-water mark; workflows full refresh by `updatedAt` (small N).
- FTS/search: workflow name + node names + node parameters text (finds webhook paths, URLs, code snippets, expression references); execution error messages.

## User Vision
- Contributing to the public printing-press-library as pp-n8n. Self-hosted first: default base URL `http://localhost:5678`, override `N8N_BASE_URL`. Evaluate PR #559 ideas without copying code: instance diff, health compare, workflows stale, credentials audit, node-inventory, deps (executeWorkflow chains), executions wait, rate-check, variables diff, workflows bulk, executions export.
- Live checks read-only against real instances; write paths only against a throwaway local docker n8n.

## Product Thesis
- Name: n8n-pp-cli (skill pp-n8n)
- Why it should exist: the official `@n8n/cli` is a thin 1:1 API mirror (no local data, no cross-workflow questions). n8n-pp-cli matches its full surface (all 105 paths) and adds a local SQLite mirror that answers the questions operators actually ask: "what's failing and where", "who uses this credential/node/webhook", "what changed", "what's dead" — offline, fast, agent-friendly JSON, plus an MCP server.

## Build Priorities
1. Full spec coverage with correct cursor pagination (`--all`), N8N_URL alias, localhost default.
2. Sync + store: workflows (with node table) and executions; FTS over node params.
3. Novel: `failures` triage, `workflows uses` (credential/node/webhook/variable/sub-workflow usage), `workflows deps`, `workflows stale`, `credentials audit` (unused/shared), `node-inventory`, `executions wait`, `workflows diff` (local file vs instance, instance vs instance), `export`/backup to directory.
4. Agent-safe writes: preview/confirm on destructive bulk ops.

## Reachability Gate
- Decision: PASS
- Evidence: GET /api/v1/workflows?limit=1 → 401 without key on local n8n 2.44.4 (expected), 200 with key on a real self-hosted instance (read-only).

## Endpoint notes
- Security audit: generated root command `audit` (pp:endpoint audit.generate, POST /audit). Distinct from the novel `credentials audit`, which is a local credential-usage audit.
