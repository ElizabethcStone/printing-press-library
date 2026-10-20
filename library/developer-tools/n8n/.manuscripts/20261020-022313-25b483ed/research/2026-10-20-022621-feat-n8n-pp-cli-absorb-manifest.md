## Absorb Manifest

### Absorbed (match or beat everything that exists)
| # | Feature | Best Source | Our Implementation | Added Value |
|---|---------|-------------|--------------------|-------------|
| 1 | Workflow list/get/create/update/delete | @n8n/cli workflow; leonardsellem MCP workflow_*; n8n-mcp n8n_list/get/create/delete_workflow | (generated endpoint) workflows list/get/create/update/delete | --all cursor pagination, --select, offline fallback from store |
| 2 | Workflow activate/deactivate/publish/unpublish/archive/unarchive/transfer | @n8n/cli workflow activate/deactivate/transfer | (generated endpoint) workflows activate/deactivate/publish/unpublish/archive/unarchive/transfer | --dry-run, typed exit codes |
| 3 | Workflow tags get/update | @n8n/cli workflow tags | (generated endpoint) workflows tags | agent JSON |
| 4 | Workflow version history + get version | n8n-mcp n8n_workflow_versions | (generated endpoint) workflows history / versions | — |
| 5 | Partial workflow update (set node param, rename/disable node) | n8n-mcp n8n_update_partial_workflow; n8nc node set/rename/rm | n8n-pp-cli workflows patch | node-by-name edits with --dry-run diff, no full JSON round-trip |
| 6 | Executions list/get/delete/retry/stop/stop-many/tags | @n8n/cli execution; leonardsellem execution_*; n8nc runs ls/get | (generated endpoint) executions list/get/delete/retry/stop/tags | status filters, --all, store-backed |
| 7 | Watch executions live | n8nc runs watch | (behavior in n8n-pp-cli tail) --resource executions --follow | framework tail |
| 8 | Credentials list/get/schema/create/update/delete/transfer/test | @n8n/cli credential; n8n-mcp n8n_manage_credentials | (generated endpoint) credentials * | — |
| 9 | Projects + members + folders | @n8n/cli project; n8n-mcp n8n_manage_folders | (generated endpoint) projects * / projects folders * | — |
| 10 | Tags CRUD | @n8n/cli tag | (generated endpoint) tags * | — |
| 11 | Variables CRUD | @n8n/cli variable | (generated endpoint) variables * | — |
| 12 | Data tables + columns + rows (insert/update/upsert/delete/clear) | @n8n/cli data-table; n8n-mcp n8n_manage_datatable | (generated endpoint) data-tables * | --stdin bulk rows |
| 13 | Users list/get/role, roles, role-mapping rules | @n8n/cli user | (generated endpoint) users * / roles * / role-mapping-rules * | — |
| 14 | Security audit | @n8n/cli audit; n8n-mcp n8n_audit_instance | (generated endpoint) audit | — |
| 15 | Source control status/pull/push | @n8n/cli source-control pull | (generated endpoint) source-control * | — |
| 16 | n8n package export/import/import-selection | @n8n/cli package | (generated endpoint) n8n-packages * | — |
| 17 | Promotions providers/connections/apply/promote | @n8n/cli promotion-* | (generated endpoint) promotions * | — |
| 18 | Community packages install/update/remove | (spec) | (generated endpoint) community-packages * | — |
| 19 | Insights summary | (spec) | (generated endpoint) insights summary | — |
| 20 | Evaluation test runs | n8n-mcp n8n_evaluations | (generated endpoint) workflows test-runs * | — |
| 21 | Settings: LDAP, OTel, SSO OIDC/SAML, security policy, log streaming | (spec) | (generated endpoint) settings * | — |
| 22 | Node/credential type policies | (spec) | (generated endpoint) node-type-policies * / credential-type-policies * | — |
| 23 | API capability discovery | (spec) | (generated endpoint) discover | — |
| 24 | Health check / connectivity | n8n-mcp n8n_health_check; 8Dvibes n8n-cli health; n8nc doctor | n8n-pp-cli doctor | auth + reachability + cache freshness |
| 25 | Output table/json/id-only | @n8n/cli --format | (behavior in n8n-pp-cli workflows list) --json/--agent/--select/--csv/--quiet | — |
| 26 | Config via url/api-key flags, env N8N_URL + N8N_API_KEY, config file | @n8n/cli config | (behavior in n8n-pp-cli doctor) N8N_BASE_URL with N8N_URL alias, N8N_API_KEY, profiles | drop-in env compat with official CLI |
| 27 | Pull all workflows to a git repo / backup | n8nc pull --all; npmove; n8n export:workflow --backup | n8n-pp-cli workflows export | one file per workflow, stable key order, git-friendly |
| 28 | Push local workflow file back | n8nc push; 8Dvibes workflows import | n8n-pp-cli workflows push | --dry-run diff before write |
| 29 | Agent skill install | @n8n/cli skill install; 8Dvibes skills install | (behavior in n8n-pp-cli agent-context) SKILL.md shipped by library | — |
| 30 | Offline search across workflows | none | n8n-pp-cli search | FTS over node names + parameters |

### Transcendence (only possible with our approach)
| # | Feature | Command | Buildability | Why Only We Can Do This | Long Description |
|---|---------|---------|--------------|------------------------|------------------|
| 1 | Failure triage | executions failures | hand-code | Groups synced error executions by workflow + failing node from cached includeData; no API call groups | Use this command for "what broke recently" and errors grouped by workflow and failing node. Do NOT use it to find workflows that are silent or never run; use 'workflows stale' instead. |
| 2 | Find usages | workflows uses | hand-code | SQL over local workflow_nodes table (credential refs, node type, webhook path, $vars refs, executeWorkflow targets) | Use this command for exact references to a credential, node type, webhook path, variable, or sub-workflow. Do NOT use it for free-text lookup of arbitrary strings in node parameters; use 'search' instead. |
| 3 | Stale and unprotected workflows | workflows stale | hand-code | Local join of workflows against latest successful execution per workflow + settings.errorWorkflow | Use this command for silent, dead, or unprotected workflows. Do NOT use it for workflows that are actively erroring; use 'executions failures' instead. |
| 4 | Credential audit | credentials usage | hand-code | Anti-join credentials vs workflow_nodes credential refs: unused, high-blast-radius, dangling refs | none |
| 5 | Wait for execution | executions wait | hand-code | Poll with backoff, status-mapped exit codes, failing node from includeData | Use this command to block on one execution until it finishes and get its result as an exit code. Do NOT use it for an ongoing live feed of executions; use 'tail --resource executions --follow' instead. |
| 6 | Semantic workflow diff | workflows diff | hand-code | Node/connection-graph diff across profiles or history versions, ignoring position/id/timestamp noise | Use this command to compare a workflow across two instances or two saved versions. Do NOT use it to preview pushing a local workflow file to the instance; use 'workflows push --dry-run' instead. |

Also hand-code from the absorbed table: workflows patch (#5), workflows export (#27), workflows push (#28).
