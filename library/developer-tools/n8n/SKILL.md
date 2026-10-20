---
name: pp-n8n
description: "Every n8n Public API endpoint, plus a local mirror that answers what is failing, what uses what, and what changed. Trigger phrases: `what n8n workflows are failing`, `which workflows use this credential`, `diff this workflow against prod`, `list my n8n workflows`, `use n8n`, `run n8n-pp-cli`."
author: "Mini"
license: "Apache-2.0"
argument-hint: "<command> [args] | install cli|mcp"
allowed-tools: "Read Bash"
metadata:
  openclaw:
    requires:
      bins:
        - n8n-pp-cli
    install:
      - kind: go
        bins: [n8n-pp-cli]
        module: github.com/mvanhorn/printing-press-library/library/developer-tools/n8n/cmd/n8n-pp-cli
---

# n8n — Printing Press CLI

## Prerequisites: Install the CLI

This skill drives the `n8n-pp-cli` binary. **You must verify the CLI is installed before invoking any command from this skill.** If it is missing, install it first:

1. Install via the Printing Press installer. It defaults binaries to `$HOME/.local/bin` on macOS/Linux and `%LOCALAPPDATA%\Programs\PrintingPress\bin` on Windows:
   ```bash
   npx -y @mvanhorn/printing-press-library install n8n --cli-only
   ```
2. Verify: `n8n-pp-cli --version`
3. Ensure the reported install directory is on `$PATH` for the agent/runtime that will invoke this skill.

If the `npx` install fails (no Node, offline, etc.), fall back to a direct Go install (requires Go 1.26.9 or newer). This installs into `$GOPATH/bin` (default `$HOME/go/bin`), so add that directory to `$PATH` instead:

```bash
go install github.com/mvanhorn/printing-press-library/library/developer-tools/n8n/cmd/n8n-pp-cli@latest
```

If `--version` reports "command not found" after install, the runtime cannot see the binary directory on `$PATH`. Do not proceed with skill commands until verification succeeds.

Manage workflows, executions, credentials, data tables, projects, and more on any self-hosted or cloud n8n instance. Sync once and get offline search, failure triage with 'executions failures', cross-workflow usage lookups with 'workflows uses', and node-level diffs between instances with 'workflows diff'.

## When to Use This CLI

Use this CLI to operate an n8n instance from a terminal or agent: list and edit workflows, triage failed executions, find which workflows use a credential or node, compare workflows between instances, and manage data tables, tags, variables, and projects through the public API.

## Anti-triggers

Do not use this CLI for:
- Designing new workflows from scratch with node documentation lookup; use n8n-mcp or the n8n editor
- Instance administration that needs the server shell (database backups, encryption keys, n8n export:credentials --decrypted)
- Reading credential secret values; the public API never returns them

## Unique Capabilities

> The local-store commands (`workflows uses`, `workflows stale`, `credentials usage`, `executions failures`) read the synced mirror. Run `n8n-pp-cli sync --resources workflows,executions,credentials` first and re-sync to refresh.

### Operations triage
- **`executions failures`** — See what broke recently, grouped by workflow and failing node, with the latest execution to retry.

  _Reach for this first when asked what is failing in n8n instead of paging through executions one by one._

  ```bash
  n8n-pp-cli executions failures --since 24h --agent
  ```
- **`workflows stale`** — Find active workflows with no recent successful run, workflows that never ran, and active workflows without an error workflow.

  _Use for hygiene sweeps to catch silently dead automations._

  ```bash
  n8n-pp-cli workflows stale --days 7 --agent
  ```

### Cross-workflow insight
- **`workflows uses`** — Find every workflow and node that uses a credential, node type, webhook path, variable, or sub-workflow.

  _Use before rotating a credential, retiring a webhook, or editing a shared sub-workflow to know the blast radius._

  ```bash
  n8n-pp-cli workflows uses --node-type n8n-nodes-base.slack --agent
  ```
- **`workflows diff`** — Compare a workflow across two instances or two saved versions at the node and connection level, ignoring layout noise.

  _Use before promoting dev to prod or after an edit to see exactly which nodes changed._

  ```bash
  n8n-pp-cli workflows diff 0vB2cZqgHEmb3RKs --against-config ~/.config/n8n-pp-cli/prod.toml --agent
  ```
- **`credentials usage`** — List unused credentials, credentials many workflows depend on, and workflow references to credentials that no longer exist.

  _Use before credential rotation or client handoff cleanup._

  ```bash
  n8n-pp-cli credentials usage --unused --agent
  ```

### Agent-native plumbing
- **`executions wait`** — Block until an execution finishes and exit non-zero if it failed, with the failing node and message.

  _Use in an edit-then-test loop to branch on whether the run passed without writing a polling script._

  ```bash
  n8n-pp-cli executions wait 1234 --max-wait 5m --agent
  ```

## Command Reference

**audit** — Operations about security audit

- `n8n-pp-cli audit` — Generate a security audit for your n8n instance.

**community-packages** — Operations about community packages

- `n8n-pp-cli community-packages install-package` — Install a community package by npm name and optional version.
- `n8n-pp-cli community-packages list-installed-packages` — Retrieve all installed community packages with pending update info.
- `n8n-pp-cli community-packages uninstall-package` — Uninstall a community package by name.
- `n8n-pp-cli community-packages update-package` — Update an installed community package to a new version.

**credential-type-policies** — Manage credential type policies

- `n8n-pp-cli credential-type-policies create-credential-type-policy-document` — Creates a reusable policy document that is not yet attached to any scope. Rule ids must be unique within the list.
- `n8n-pp-cli credential-type-policies delete-credential-type-policy-document` — Deletes a policy document. A document that is still attached to a scope is rejected with 409; detach it first.
- `n8n-pp-cli credential-type-policies get-credential-type-instance-policy` — Returns the composed instance-scope policy
- `n8n-pp-cli credential-type-policies get-credential-type-policy-document` — Retrieve a credential type policy document
- `n8n-pp-cli credential-type-policies get-credential-type-project-policy` — Returns the project's own composed policy, not the result of combining it with the instance policy.
- `n8n-pp-cli credential-type-policies list-credential-type-policy-documents` — Returns a cursor-paginated list of reusable policy documents.
- `n8n-pp-cli credential-type-policies put-credential-type-instance-policy` — Sets the instance default action and replaces the rules of its single policy document, creating both on first write.
- `n8n-pp-cli credential-type-policies put-credential-type-project-policy` — Sets the project's default action and replaces the rules of its single policy document, creating both on first write.
- `n8n-pp-cli credential-type-policies replace-credential-type-policy-attachments` — Replaces every attachment on a scope. `scopeId` comes from the scope's `GET` response once it has been written.
- `n8n-pp-cli credential-type-policies update-credential-type-policy-document` — Replaces the document's whole rule list. `version` must equal the version last read; a stale value is rejected with 409.

**credentials** — Operations about credentials

- `n8n-pp-cli credentials create` — Creates a credential that can be used by nodes of the specified type.
- `n8n-pp-cli credentials delete` — Deletes a credential from your instance. You must be the owner of the credential.
- `n8n-pp-cli credentials get` — Retrieves a credential by ID. Credential data (secrets) is not included.
- `n8n-pp-cli credentials get-type` — Show credential data schema
- `n8n-pp-cli credentials list` — Retrieve all credentials from your instance. Only available for the instance owner and admin.
- `n8n-pp-cli credentials test` — Tests a credential by ID using the stored credential data.
- `n8n-pp-cli credentials transfer` — Transfer a credential to another project.
- `n8n-pp-cli credentials update` — Updates an existing credential. You must be the owner of the credential.

**data-tables** — Operations about data tables and their rows

- `n8n-pp-cli data-tables create` — Create a new data table in your personal project or a team project you have access to.
- `n8n-pp-cli data-tables delete` — Delete a data table. This will also delete all rows in the table.
- `n8n-pp-cli data-tables get` — Retrieve a specific data table by ID.
- `n8n-pp-cli data-tables list` — Retrieve a list of all data tables with optional filtering, sorting, and pagination.
- `n8n-pp-cli data-tables update` — Update a data table's name.

**discover** — API capability discovery

- `n8n-pp-cli discover` — Returns a filtered capability map based on the caller's API key scopes.

**executions** — Operations about executions

- `n8n-pp-cli executions delete` — Deletes an execution from your instance.
- `n8n-pp-cli executions get` — Retrieve an execution from your instance.
- `n8n-pp-cli executions get-tags` — Get annotation tags for an execution.
- `n8n-pp-cli executions list` — Retrieve all executions from your instance.
- `n8n-pp-cli executions retry` — Retry an execution from your instance.
- `n8n-pp-cli executions stop` — Stop an execution by id.
- `n8n-pp-cli executions stop-many` — Stop multiple executions from your instance based on filter criteria.
- `n8n-pp-cli executions update-tags` — Update annotation tags of an execution.

**insights** — Operations about insights

- `n8n-pp-cli insights` — Retrieve the insights summary for the selected date range.

**n8n-packages** — Beta — breaking changes may still occur without major version bump.

- `n8n-pp-cli n8n-packages create` — **Beta** — breaking changes may still occur without major version bump.
- `n8n-pp-cli n8n-packages import-package` — **Beta** — breaking changes may still occur without major version bump. Imports a gzip-compressed tar package (`.
- `n8n-pp-cli n8n-packages import-package-selection` — **Beta** — breaking changes may still occur without major version bump.

**node-type-policies** — Manage node type policies

- `n8n-pp-cli node-type-policies create-node-type-policy-document` — Creates a reusable policy document that is not yet attached to any scope. Rule ids must be unique within the list.
- `n8n-pp-cli node-type-policies delete-node-type-policy-document` — Deletes a policy document. A document that is still attached to a scope is rejected with 409; detach it first.
- `n8n-pp-cli node-type-policies get-node-type-instance-policy` — Returns the composed instance-scope policy
- `n8n-pp-cli node-type-policies get-node-type-policy-document` — Retrieve a node type policy document
- `n8n-pp-cli node-type-policies get-node-type-project-policy` — Returns the project's own composed policy, not the result of combining it with the instance policy.
- `n8n-pp-cli node-type-policies list-node-type-policy-documents` — Returns a cursor-paginated list of reusable policy documents.
- `n8n-pp-cli node-type-policies put-node-type-instance-policy` — Sets the instance default action and replaces the rules of its single policy document, creating both on first write.
- `n8n-pp-cli node-type-policies put-node-type-project-policy` — Sets the project's default action and replaces the rules of its single policy document, creating both on first write.
- `n8n-pp-cli node-type-policies replace-node-type-policy-attachments` — Replaces every attachment on a scope. `scopeId` comes from the scope's `GET` response once it has been written.
- `n8n-pp-cli node-type-policies update-node-type-policy-document` — Replaces the document's whole rule list. `version` must equal the version last read; a stale value is rejected with 409.

**projects** — Operations about projects

- `n8n-pp-cli projects create` — Create a team project on your instance.
- `n8n-pp-cli projects delete` — Delete a project from your instance.
- `n8n-pp-cli projects list` — Retrieve projects from your instance.
- `n8n-pp-cli projects update` — Update a project on your instance.

**promotions** — Operations about promotion providers, connections, and package operations

- `n8n-pp-cli promotions add-project-to-connection` — Links a team project to a project-scoped connection. A project can be linked to only one connection.
- `n8n-pp-cli promotions apply-package` — Checks the full package at the configured branch tip.
- `n8n-pp-cli promotions apply-project-selection` — Applies selected workflow changes from the project's Apply branch. Send the selected change-list IDs in workflowIds.
- `n8n-pp-cli promotions clone-checkout` — Clones the configured branch into local storage on the instance that handles the request. Safe to call repeatedly.
- `n8n-pp-cli promotions continue-apply-package` — Rechecks the configured source and current target bindings.
- `n8n-pp-cli promotions continue-apply-project-selection` — Rechecks the project's Apply source and the current bindings for the selection.
- `n8n-pp-cli promotions create-connection` — Creates a connection on an existing provider, with up to one initial configuration for each direction.
- `n8n-pp-cli promotions create-provider` — Creates a provider and its authentication material.
- `n8n-pp-cli promotions delete-config` — Deletes the configuration and its local checkout. Nothing in Git changes.
- `n8n-pp-cli promotions delete-connection` — Deletes the connection with its configurations, project links, and local checkouts. Its provider is kept.
- `n8n-pp-cli promotions delete-provider` — A provider used by a connection cannot be deleted.
- `n8n-pp-cli promotions disconnect-checkout` — Removes the local checkout and keeps the configuration, its credentials, and the trusted SSH host keys.
- `n8n-pp-cli promotions get-changes` — Compares a team project on this instance with the branch of its promotion configuration and lists the workflows that
- `n8n-pp-cli promotions get-connection` — Returns the connection with its provider summary and its configurations.
- `n8n-pp-cli promotions get-connection-projects` — List projects linked to a promotion connection
- `n8n-pp-cli promotions get-provider` — Returns the provider with its public configuration. Secrets are never returned.
- `n8n-pp-cli promotions list-connections` — Returns a cursor-paginated list of connections with their configurations.
- `n8n-pp-cli promotions list-providers` — Returns a cursor-paginated list of providers, without their public configuration.
- `n8n-pp-cli promotions promote-package` — Exports every team project, commits it, and pushes to the configured base branch or a new timestamped branch.
- `n8n-pp-cli promotions promote-project-selection` — Promotes a chosen set of a team project's workflows to the Promote branch of the project's promotion configuration.
- `n8n-pp-cli promotions remove-project-from-connection` — Unlink a project from a promotion connection
- `n8n-pp-cli promotions update-connection` — Updates the name, target, or provider. Scope cannot change, and configurations have their own routes.
- `n8n-pp-cli promotions update-provider` — Renames the provider, and replaces its credentials when `auth` is sent.
- `n8n-pp-cli promotions upsert-apply-config` — Replaces the whole configuration, so an omitted name resets it to 'Apply'. Answers 200 whether it created or replaced.
- `n8n-pp-cli promotions upsert-promote-config` — Replaces the whole configuration

**role-mapping-rules** — Operations about identity-provider role-mapping rules

- `n8n-pp-cli role-mapping-rules create` — Creates a rule that maps an identity-provider claim expression to a role.
- `n8n-pp-cli role-mapping-rules delete` — Deletes a role-mapping rule.
- `n8n-pp-cli role-mapping-rules list` — Returns the configured role-mapping rules.
- `n8n-pp-cli role-mapping-rules update` — Updates a rule's claim expression, role, and/or project assignments.

**roles** — Operations about roles

- `n8n-pp-cli roles create` — Creates a custom role. Set `roleType` to `global` for an instance-wide role or `project` for a project role.
- `n8n-pp-cli roles delete` — Deletes a custom role. System roles cannot be deleted.
- `n8n-pp-cli roles get` — Returns a single role with its scopes. Set `withUsageCount` to include how many users and projects use the role.
- `n8n-pp-cli roles list` — Returns all roles grouped by type (global and project).
- `n8n-pp-cli roles update` — Replaces a custom role's display name, description, and scopes. System roles cannot be updated.

**settings** — Manage settings

- `n8n-pp-cli settings create-log-streaming-destination` — Create a log streaming destination.
- `n8n-pp-cli settings delete-log-streaming-destination` — Remove a log streaming destination.
- `n8n-pp-cli settings get-ldap-configuration` — Retrieve the current LDAP configuration, including every field exposed in the UI.
- `n8n-pp-cli settings get-ldap-sync` — Retrieve the history of LDAP synchronizations, most recent first.
- `n8n-pp-cli settings get-log-streaming-destination` — Retrieve a single log streaming destination by id.
- `n8n-pp-cli settings get-log-streaming-event-types` — List the event types that can be streamed to a log streaming destination.
- `n8n-pp-cli settings get-oidc-configuration` — Retrieve the current OIDC SSO configuration, including every field exposed in the UI.
- `n8n-pp-cli settings get-otel` — Retrieve the current OpenTelemetry configuration, including every field exposed in the UI.
- `n8n-pp-cli settings get-saml-configuration` — Retrieve the current SAML SSO configuration
- `n8n-pp-cli settings get-security-policy` — Retrieve the instance security policy: personal-space publishing and sharing
- `n8n-pp-cli settings list-log-streaming-destinations` — List the configured log streaming destinations.
- `n8n-pp-cli settings run-ldap-sync` — Manually trigger an LDAP synchronization. The response returns the new sync history record.
- `n8n-pp-cli settings set-oidc-configuration` — Set the OIDC SSO configuration. The update takes effect exactly as it would from the UI, using the same validation.
- `n8n-pp-cli settings test-log-streaming-destination` — Send a test message to the destination to verify it is reachable and configured correctly.
- `n8n-pp-cli settings test-otel-trace` — Send a single test span to the given OTLP collector and report whether it was accepted.
- `n8n-pp-cli settings update-ldap-configuration` — Replace the LDAP configuration with the provided full object (partial updates are not supported).
- `n8n-pp-cli settings update-log-streaming-destination` — Replace an existing log streaming destination.
- `n8n-pp-cli settings update-otel` — Set the OpenTelemetry configuration.
- `n8n-pp-cli settings update-saml-configuration` — Replace the SAML SSO configuration with the provided full object.
- `n8n-pp-cli settings update-security-policy` — Replace the instance security policy with the provided full object. Every writable field must be sent.

**source-control** — Operations about source control

- `n8n-pp-cli source-control get-status` — Previews the pending changes between the instance and the connected Git branch in either the `push` or `pull` direction.
- `n8n-pp-cli source-control pull` — Fetches changes from the connected Git branch into the instance.
- `n8n-pp-cli source-control push` — Commits and pushes the selected files to the connected Git branch.

**tags** — Operations about tags

- `n8n-pp-cli tags create` — Create a tag in your instance.
- `n8n-pp-cli tags delete` — Deletes a tag.
- `n8n-pp-cli tags get` — Retrieves a tag.
- `n8n-pp-cli tags list` — Retrieve all tags from your instance.
- `n8n-pp-cli tags update` — Update a tag.

**users** — Operations about users

- `n8n-pp-cli users create` — Create one or more users.
- `n8n-pp-cli users delete` — Delete a user from your instance.
- `n8n-pp-cli users get` — Retrieve a user from your instance. Only available for the instance owner.
- `n8n-pp-cli users list` — Retrieve all users from your instance. Only available for the instance owner.

**variables** — Operations about variables

- `n8n-pp-cli variables create` — Create a variable in your instance.
- `n8n-pp-cli variables delete` — Delete a variable from your instance.
- `n8n-pp-cli variables list` — Retrieve variables from your instance.
- `n8n-pp-cli variables update` — Update a variable from your instance.

**workflows** — Operations about workflows

- `n8n-pp-cli workflows activate` — Deprecated: use POST /workflows/{id}/publish instead. Publish a workflow.
- `n8n-pp-cli workflows archive` — Soft-deletes a workflow by archiving it.
- `n8n-pp-cli workflows create` — Create a workflow in your instance.
- `n8n-pp-cli workflows deactivate` — Deprecated: use POST /workflows/{id}/unpublish instead. Deactivate a workflow.
- `n8n-pp-cli workflows delete` — Delete a workflow.
- `n8n-pp-cli workflows get` — Retrieve a workflow.
- `n8n-pp-cli workflows get-deprecated-version` — Deprecated: use GET /workflows/{workflowId}/versions/{workflowVersionId} instead.
- `n8n-pp-cli workflows get-history` — Returns a paginated list of workflow versions (version IDs and metadata) for a workflow.
- `n8n-pp-cli workflows get-tags` — Get workflow tags.
- `n8n-pp-cli workflows get-version` — Retrieve a single version of a workflow from its version history.
- `n8n-pp-cli workflows list` — Retrieve all workflows from your instance.
- `n8n-pp-cli workflows publish` — Publish a workflow. In n8n v1, this action was termed activating a workflow.
- `n8n-pp-cli workflows transfer` — Transfer a workflow to another project.
- `n8n-pp-cli workflows unarchive` — Restores an archived workflow.
- `n8n-pp-cli workflows unpublish` — Unpublish a workflow. In n8n v1, this action was termed deactivating a workflow.
- `n8n-pp-cli workflows update` — Update a workflow.
- `n8n-pp-cli workflows update-tags` — Update tags of a workflow.


**Hand-written commands**

- `n8n-pp-cli workflows patch` — Edit one node by name (set a parameter, rename, enable or disable); previews the node-level diff and writes only with `--apply`.
- `n8n-pp-cli workflows push` — Push a local workflow JSON file; previews the diff and writes only with `--apply` (`--create` to create a new workflow).
- `n8n-pp-cli workflows export` — Write every workflow to `--dir` as one stable, git-friendly JSON file per workflow.
- `n8n-pp-cli workflows uses` — Find every workflow and node that uses a credential, node type, webhook path, variable, or sub-workflow.
- `n8n-pp-cli workflows stale` — Find active workflows with no recent successful run, workflows that never ran, and active workflows without an error workflow.
- `n8n-pp-cli workflows diff` — Compare a workflow across two instances or two saved versions at the node and connection level, ignoring layout noise.
- `n8n-pp-cli executions failures` — See what broke recently, grouped by workflow and failing node, with the latest execution to retry.
- `n8n-pp-cli executions wait` — Block until an execution finishes and exit non-zero if it failed, with the failing node and message.
- `n8n-pp-cli credentials usage` — List unused credentials, credentials many workflows depend on, and workflow references to credentials that no longer exist.



## Freshness Contract

This printed CLI owns bounded freshness only for registered store-backed read command paths. In `--data-source auto` mode, those paths check `sync_state` and may run a bounded refresh before reading local data. `--data-source local` never refreshes. `--data-source live` reads the API and does not mutate the local store. Set `N8N_NO_AUTO_REFRESH=1` to skip the freshness hook without changing source selection.

Covered paths:

- `n8n-pp-cli community-packages`
- `n8n-pp-cli credential-type-policies`
- `n8n-pp-cli credentials`
- `n8n-pp-cli credentials get`
- `n8n-pp-cli credentials list`
- `n8n-pp-cli data-tables`
- `n8n-pp-cli data-tables get`
- `n8n-pp-cli data-tables list`
- `n8n-pp-cli executions`
- `n8n-pp-cli executions get`
- `n8n-pp-cli executions list`
- `n8n-pp-cli node-type-policies`
- `n8n-pp-cli projects`
- `n8n-pp-cli projects list`
- `n8n-pp-cli promotions`
- `n8n-pp-cli role-mapping-rules`
- `n8n-pp-cli role-mapping-rules list`
- `n8n-pp-cli roles`
- `n8n-pp-cli roles get`
- `n8n-pp-cli roles list`
- `n8n-pp-cli settings`
- `n8n-pp-cli source-control`
- `n8n-pp-cli tags`
- `n8n-pp-cli tags get`
- `n8n-pp-cli tags list`
- `n8n-pp-cli users`
- `n8n-pp-cli users get`
- `n8n-pp-cli users list`
- `n8n-pp-cli variables`
- `n8n-pp-cli variables list`
- `n8n-pp-cli workflows`
- `n8n-pp-cli workflows get`
- `n8n-pp-cli workflows list`

When JSON output uses the generated provenance envelope, freshness metadata appears at `meta.freshness`. Treat it as current-cache freshness for the covered command path, not a guarantee of complete historical backfill or API-specific enrichment.

### Finding the right command

When you know what you want to do but not which command does it, ask the CLI directly:

```bash
n8n-pp-cli which "<capability in your own words>"
```

`which` resolves a natural-language capability query to the best matching command from this CLI's curated feature index. Exit code `0` means at least one match; exit code `2` means no confident match — fall back to `--help` or use a narrower query. `--json` (and other machine formats) keep that exit-2 contract and write `{"matches":[]}` on stdout so agents can inspect the envelope without treating a miss as success.

## Recipes

### Morning failure triage

```bash
n8n-pp-cli executions failures --since 24h --agent
```

Groups error executions by workflow and failing node so one broken credential shows as one row.

### Blast radius before rotating a credential

```bash
n8n-pp-cli workflows uses --credential 'Slack Bot' --agent
```

Lists every workflow and node that references the credential.

### Narrow a large workflow payload

```bash
n8n-pp-cli workflows get 0vB2cZqgHEmb3RKs --agent --select name,active,nodes.name,nodes.type
```

Workflow JSON is large; select only node names and types.

### Promote check

```bash
n8n-pp-cli workflows diff 0vB2cZqgHEmb3RKs --against-config ~/.config/n8n-pp-cli/prod.toml
```

Shows node and connection changes between this instance and the instance configured in prod.toml.

## Auth Setup

Create an API key in n8n under Settings > n8n API, then export N8N_API_KEY. Point at your instance with N8N_URL=https://n8n.example.com (the same variable the official n8n CLI uses; /api/v1 is appended) or N8N_BASE_URL=https://n8n.example.com/api/v1. The default is http://localhost:5678/api/v1. The public API must be enabled on the instance, and some resources (projects, variables, source control, SSO, LDAP, log streaming, promotions) need an n8n license.

Run `n8n-pp-cli doctor` to verify setup.

## Agent Mode

Add `--agent` to any command. Expands to: `--json --compact --no-input --no-color`.

Global format flags share one contract on promoted, novel, sync, and `--deliver` paths:

- `--json` — one JSON document on stdout (sync progress events go to stderr)
- `--compact` — keep identity/status/timestamp fields; does not change the document vs stream shape
- `--csv` / `--plain` — tabular rows (collection envelopes unwrap to the row array). `--csv` prefixes `'` on a cell or header key that starts with `=`, `+`, `-`, `@`, tab, CR, or LF (fullwidth `＝＋－＠` too) so a spreadsheet does not run it as a formula; a plain signed number such as `-5` or `+2.5e3` is unchanged.
- `--quiet` — one identity value per row, no envelope

- **Pipeable** — JSON on stdout, errors on stderr
- **Filterable** — `--select` keeps a subset of fields. Dotted paths descend into nested structures; arrays traverse element-wise. Critical for keeping context small on verbose APIs:

  ```bash
  n8n-pp-cli credentials list --agent --select createdAt,id,name
  ```
- **Previewable** — `--dry-run` shows the request without sending
- **Offline-friendly** — sync/search commands can use the local SQLite store when available
- **Non-interactive** — never prompts, every input is a flag
- **Explicit confirmation** — `--agent` does not imply `--yes`; pass `--yes` separately only after the target, arguments, and side effects are clear
- **Explicit retries** — use `--idempotent` only when an already-existing create should count as success, and use `--ignore-missing` only when a missing delete target should count as success

### Response envelope

Commands that read from the local store or the API wrap output in a provenance envelope:

```json
{
  "meta": {"source": "live" | "local", "synced_at": "...", "reason": "..."},
  "results": <data>
}
```

Parse `.results` for data and `.meta.source` to know whether it's live or local. A human-readable `N results (live)` summary is printed to stderr only when stdout is a terminal AND no machine-format flag (`--json`, `--csv`, `--compact`, `--quiet`, `--plain`, `--select`) is set — piped/agent consumers and explicit-format runs get pure JSON on stdout.

## Paths and state

Agents should treat the CLI's path resolver as part of the runtime contract:

- Use `--home <dir>` for one invocation, or set `N8N_HOME=<dir>` to relocate all four path kinds under one root.
- Use per-kind env vars only when a specific kind must diverge: `N8N_CONFIG_DIR`, `N8N_DATA_DIR`, `N8N_STATE_DIR`, `N8N_CACHE_DIR`.
- Resolution order is per-kind env var, `--home`, `N8N_HOME`, XDG (`XDG_CONFIG_HOME`, `XDG_DATA_HOME`, `XDG_STATE_HOME`, `XDG_CACHE_HOME`), then platform defaults.
- `config` contains settings like `config.toml` and profiles. `data` contains `credentials.toml`, `data.db`, cookies, and auth sidecars. `state` contains persisted queries, jobs, and `teach.log`. `cache` contains regenerable HTTP/cache files.
- Stored secrets live in `credentials.toml` under the data dir. Existing legacy `config.toml` secrets are read for compatibility and leave `config.toml` on the first auth write.
- Run `n8n-pp-cli doctor --fail-on warn` to surface path and credential-location warnings. `agent-context` exposes a schema v4 `paths` block for agents that need the resolved dirs.
- For MCP, pass relocation through the MCP host config. The MCP binary does not inherit CLI flags:

  ```json
  {
    "mcpServers": {
      "n8n": {
        "command": "n8n-pp-mcp",
        "env": {
          "N8N_HOME": "/srv/n8n"
        }
      }
    }
  }
  ```

Fleet precedence: an inherited per-kind env var overrides an explicit `--home` for that kind. Use `N8N_HOME` or per-kind vars as durable fleet levers, and use `--home` only for a single invocation. Relocation is not reversible by unsetting env vars; move files manually before clearing `N8N_HOME`, or `doctor` will not find credentials left under the former root.

## Automatic learning

This CLI ships a self-capturing learning loop. The CLI does its own bookkeeping: every invocation is journaled locally, a failed flag followed by a corrected retry auto-derives a `flag_alias` candidate, and a `teach` on a query family without a playbook auto-synthesizes a `playbook_candidate` from the session's journal. Your job is judgment only: `recall` first, act on surfaced candidates, `teach` the final answer, `playbook amend` when you observe a correction. You never record failures by hand.

### Step 1: `recall` before any discovery

Before list/search/drill commands on a new user question, pass the question as an argv or MCP tool argument to `recall --agent`. Do not interpolate user-controlled text into a shell command line.

Quoted `recall "<question>"` breaks on an apostrophe, which is ordinary English. A quoted heredoc breaks when a body line equals the delimiter, and that delimiter is published in these docs. Write the question with a non-shell file-writing tool, then read it back as data:

```bash
# Write the question verbatim with your file-writing tool (no shell involved).
# Command substitution on a file only ever yields data — the shell never
# parses the file's bytes as syntax.
QUERY=$(cat /path/to/question.txt)
n8n-pp-cli recall "$QUERY" --agent
```

Prefer MCP: pass the question as the tool's query argument. `"$QUERY"` after a file read is argv-safe; putting the question itself in the command text is not.

The response envelope:

```json
{
  "query": "...",
  "normalized": "<normalized form>",
  "query_entities": ["..."],
  "found": true | false,
  "match_score": 0.0,
  "results": [
    { "resource_id": "...", "resource_type": "...", "venue": "...",
      "confidence": 2, "entity_match": "exact|partial|unknown",
      "source": "taught|preseed|pattern", "warnings": ["..."] }
  ],
  "mismatches": [ /* only when --debug-mismatches */ ],
  "warnings": [ /* top-level */ ],
  "candidates": [
    { "id": 12, "class": "flag_alias | playbook_candidate",
      "summary": "...", "sightings": 3, "last_seen": "...",
      "rationale": "...",
      "next_action": ["<trial command>", "n8n-pp-cli learnings confirm 12"] }
  ],
  "playbook": {
    "query_family": "...",
    "playbook": {
      "steps": [ { "cmd": "<command with {slot} substitution>", "purpose": "..." } ],
      "entity_slots": ["$ENTITY"],
      "expected_tool_calls": 3
    },
    "slots_resolved": { "$ENTITY": { "token": "<live token>", "canonical": "<canonical>" } },
    "notes": "<workarounds + gotchas for this query family>"
  },
  "notes": "<duplicate surface for non-playbook callers>"
}
```

Empty-store short-circuit: if the store has no learnings, playbooks, or candidates yet (recall finds nothing and `learnings list` and `learnings candidates` are both empty), skip recall for the rest of this session instead of taxing every query; resume recall-first once something has been taught.

### Step 2: decision tree

Read `candidates`, `playbook`, `notes`, `results[0]`, and warnings in that order:

```
if Candidates present (warnings include "candidates_present"):
    -> candidates are try-then-confirm, never facts. Follow each candidate's
       two-step next_action verbatim: run the trial command first, then run
       `learnings confirm <id>` only after the trial verified the behavior.
       Reject a wrong candidate with `learnings reject <id>`.
    -> NEVER re-teach something recall surfaced as a candidate; confirm or
       reject that candidate instead of teaching a duplicate.
    -> candidates ride alongside playbooks and resource hits, not instead of
       them; continue with the branches below after acting on them.

if Playbook present:
    -> READ Playbook.notes verbatim FIRST (workarounds + gotchas the CLI surface doesn't expose)
    -> replay Playbook.steps in order, substituting Playbook.slots_resolved entries
       for the entity slot tokens. If a step's slot is unresolved, fall back to
       discovery for that step only.
    -> the Playbook's expected_tool_calls is a budget; if you find yourself running
       materially more, record the divergence via `n8n-pp-cli playbook amend`
       at end-of-session.

elif Notes present (no Playbook):
    -> read Notes verbatim before any discovery step; they carry known gotchas
       for this query family even when no structured choreography exists yet.

elif Found AND Results[0].EntityMatch == "exact" AND Results[0].Confidence >= 2:
    -> skip discovery; fetch live data for Results[*].ResourceID in parallel

elif Found AND Results[0].EntityMatch == "partial":
    -> candidate hint, NOT a hit; read the resource title to validate before trusting

elif (any row in Mismatches[] when --debug-mismatches was passed):
    -> treat as cold start; the stored learning is for a different entity
       (different canonical resolved from query_entities)

else:  // Found == false, no playbook, no notes
    -> cold start; run discovery normally; teach the answer afterward (Step 4).
       If the family has no playbook yet, that teach auto-synthesizes a
       playbook candidate from this session's journal - you do not need to
       record one by hand.
```

Playbook and Notes are orthogonal to the per-resource path. A recall response can carry both a Playbook AND a `Results[]` hit - use both: the Playbook tells you which choreography to run; the resource hits short-circuit specific steps. Default to skipping `mismatches`; pass `--debug-mismatches` only when investigating cold-start surprises.

Candidate judgment details: `learnings confirm <id>` prints the candidate's full payload before materializing it - check that the printed payload matches the behavior you verified. `learnings reject <id>` tombstones the derivation signature so the same candidate does not resurface. The envelope carries only the few candidates worth acting on now; `n8n-pp-cli learnings candidates` lists the full open set.

Graceful degradation: if `learnings confirm` is an unknown command, you are driving an older binary - ignore the candidates guidance and follow the rest of the protocol.

### Step 3: always read `warnings`

- `low_confidence`: row exists at `confidence<2`. Treat as a hint, not a skip-discovery hit.
- `resource_not_in_store`: the local store doesn't have the resource the learning points at. The match validator couldn't classify entities — direct-fetch and re-evaluate.
- `cross_alias_match` (per-result): the row was taught under a different alias and matched the live query's canonical via `entity_lookups` (e.g., a "USA" teach satisfying a "United States" recall). Trust the resource_id.
- `similar_shape_different_entity:<canonical>` (top-level): a structurally matching row exists but its canonical entity differs from the live query's. Treated as cold start; the warning carries the conflicting canonical as a hint, but the row is NOT promoted into Results.
- `ambiguous_alias` (top-level): a single query entity resolved to multiple canonicals (e.g., "Cards" → Arizona Cardinals + St. Louis Cardinals). Surface the ambiguity from context before committing to a resource.
- `candidates_present` (top-level): the envelope carries a `candidates` section. Handle it via the candidates branch in Step 2 before anything else.
- `lookup_refresh_available` (top-level): an entity in the query has no lookup row yet, but synced data could provide one. Run `n8n-pp-cli sync` to refresh entity lookups.
- Top-level `no_learnings_for_query_family`: the table had no rows above the Jaccard floor. Pure cold start.

### Step 4: `teach &` after finalizing your response - always

Teaching is unconditional. After resolving a query the store could not answer, background-teach the final resource mapping - no call-count threshold, no judging whether it was "worth" learning. The teach is the anchor of the loop: it triggers playbook synthesis for a family without a playbook, and same-referent phrasings fold into one family so near-duplicate teaches do not fragment the store. Fire it after assembling your user-facing response but BEFORE emitting it, with a shell `&` so the call returns immediately. Pass the query the same way as recall — argv/MCP, or file-then-`$QUERY`. Do not splice the question into the command text:

```bash
QUERY=$(cat /path/to/question.txt)
n8n-pp-cli teach --query "$QUERY" --resource-type <type> --resource <id1> --resource <id2>
# (append shell `&` to background it)
```

Silent on success. Errors only land in `teach.log` under the resolved state dir. Teach the **most specific** resource - if the user asked a broad question and you walked through parent records to find the specific answer, teach the leaf id, not the parent. The CLI uses seeded `entity_lookups` for cross-alias resolution at recall time, so a teach under one alias (e.g., "Niners") satisfies future queries under another alias (e.g., "49ers", "San Francisco") automatically.

PII rule: teach the structural question with identifiers stripped - never include names, emails, phone numbers, account ids, or other personal identifiers in taught queries or notes. The CLI scans teach queries for obvious email/phone shapes and warns, but does not block; strip before teaching rather than relying on the warning.

### Step 5: playbooks - optional flags, automatic synthesis

You do not need to decide whether a session "deserves" a playbook: a teach on a family without one auto-synthesizes a `playbook_candidate` from the session's journal, and the next session judges it via confirm/reject. Attach explicit playbook flags only when you already hold choreography worth recording verbatim - workarounds the CLI didn't surface (silently-dropped flags, undocumented params, pagination tricks, payload gotchas). Prefer the **integrated one-call form** - record the resource learning and the playbook in the same `teach` invocation:

```bash
# Common case: record both the resource learning AND the playbook in one call.
QUERY=$(cat /path/to/question.txt)
n8n-pp-cli teach \
  --query "$QUERY" \
  --resource <id> \
  --playbook-file ~/playbooks/<shape>.json \
  --playbook-notes-file ~/playbooks/<shape>-notes.md
# (append shell `&` to background it)

# Alternate: playbook-only (no resource to record alongside).
QUERY=$(cat /path/to/question.txt)
n8n-pp-cli teach-playbook \
  --query "$QUERY" \
  --playbook-file ~/playbooks/<shape>.json \
  --notes-file ~/playbooks/<shape>-notes.md
```

Playbook files are JSON with `steps`, `entity_slots`, `expected_tool_calls`. Notes files are markdown carrying the gotchas verbatim. File-free callers (MCP-only agents) pass the same content inline: `--playbook-json` and `--playbook-notes` on the integrated `teach` form, `--playbook-json` and `--notes` on `teach-playbook`. On the integrated `teach` form, the playbook flags are optional - omit them entirely for a resource-only teach. On the standalone `teach-playbook` form, at least one of the playbook and notes flags must be set; both empty is rejected. Playbooks are keyed on the structural query family (entities stripped) so a recipe taught from one entity-shaped query applies to every other query of the same shape, with `slots_resolved` binding the live query's canonical at recall time.

When you DO find a playbook on a future recall, treat it as ground truth: replay the steps with `slots_resolved` substitutions, skip the discovery that the choreography already documents, and read `notes` before any step.

### Step 6: `playbook amend &` when your debug response identifies a correction

If your debug-protocol response identifies a concrete correction the notes or playbook should know — a workaround, an undocumented endpoint shape, a stale field name, observed schema drift, an empty-payload fallback — fire `playbook amend` BEFORE emitting your user-facing response. Same fire-and-forget posture as `teach`. Pass the query and note as argv/MCP arguments, or write each with a non-shell file tool and read them back (`QUERY=$(cat ...)`, `NOTE=$(cat ...)`). Do not interpolate either string into the command text:

```bash
QUERY=$(cat /path/to/question.txt)
NOTE=$(cat /path/to/note.txt)
n8n-pp-cli playbook amend \
  --query "$QUERY" \
  --add-note "$NOTE"
# (append shell `&` to background it)
```

What counts as worth amending: a behavior you OBSERVED this session that future-you would benefit from knowing. Examples worth amending:

- A workaround for a CLI surface that silently drops or misorders a flag.
- An undocumented endpoint shape (response wrapped in `{meta, results}`, payload nested two levels deeper than the docs claim).
- Observed schema drift (a field renamed, an index that shifted between seasons, a category label that the API now returns lower-cased).

What does NOT belong in notes:

- The year-specific or entity-specific answer to the user's question. That's the response, not a learning.
- Per-team / per-athlete / per-row data the playbook already retrieves at runtime.
- Statements that paraphrase what the existing notes already say.

The amend command appends to the family's existing notes with a timestamped marker (`[amend YYYY-MM-DDTHH:MMZ]: <text>`). Multiple amends accumulate; the audit trail is visible. If no playbook exists yet for the family, amend creates a notes-only one (so cold-start corrections still land).

#### PII discipline for amend notes

`playbook amend` notes are designed to potentially flow upstream as shared knowledge in future versions of the Printing Press. Keep them clean of user-identifying content so the upstream-contribution path stays open without retroactive scrubbing:

- **Do NOT embed** paths to user filesystems, personal API keys or tokens, user email addresses, user GitHub handles, or specific query histories tied to a single user.
- **Acceptable**: endpoint shapes, undocumented field names, API gotchas, observed schema drift, workarounds for CLI surfaces, generalizable pagination or retry tactics.

If a correction is only meaningful with user-specific context, it belongs in a personal note, not in the playbook amend.

### Measuring the loop

`n8n-pp-cli learnings stats` reports recall hit rate, teach-to-reuse, playbook resolution rate, and candidate confirm/reject counts from the local `learn_events` table. Rates are null until they have a denominator; everything stays on this machine. Use it to check whether the loop is earning its keep for this CLI.

### Disabling learning

- `--no-learn` on a single command short-circuits both `recall` and the `teach` write path. Use for deterministic agent flows or tests that must not be affected by accumulated learnings.
- `N8N_NO_LEARN=true` in the environment globally disables the pipeline.

## Agent Feedback

When you (or the agent) notice something off about this CLI, record it:

```
n8n-pp-cli feedback "the --since flag is inclusive but docs say exclusive"
n8n-pp-cli feedback --stdin < notes.txt
n8n-pp-cli feedback list --json --limit 10
```

Entries are stored locally as `feedback.jsonl` under the resolved data dir. They are never POSTed unless `N8N_FEEDBACK_ENDPOINT` is set AND either `--send` is passed or `N8N_FEEDBACK_AUTO_SEND=true`. Default behavior is local-only.

Write what *surprised* you, not a bug report. Short, specific, one line: that is the part that compounds.

## Output Delivery

Every command accepts `--deliver <sink>`. The output goes to the named sink in addition to (or instead of) stdout, so agents can route command results without hand-piping. Three sinks are supported:

| Sink | Effect |
|------|--------|
| `stdout` | Default; write to stdout only |
| `file:<path>` | Atomically write output to `<path>` as a private 0600 file (exclusive randomly named temp in the destination directory, then rename; a planted symlink is not followed). Binary-response commands write decoded payload bytes (not the base64 JSON envelope) and print a small JSON receipt on stdout; `--json`/`--csv` do not refuse when this sink is set. |
| `webhook:<url>` | POST the output body to the URL (`application/json`). Redirects are not followed; a 3xx response fails the delivery and the body is not sent to the `Location`. |

Unknown schemes are refused with a structured error naming the supported set. Webhook failures, including redirects, return non-zero and log the URL and HTTP status on stderr.

## Named Profiles

A profile is a saved set of flag values, reused across invocations. Use it when a scheduled or recurring agent reuses the same saved flags while providing different input each run.

```
n8n-pp-cli profile save briefing --json
n8n-pp-cli --profile briefing credentials list
n8n-pp-cli profile list --json
n8n-pp-cli profile show briefing
n8n-pp-cli profile delete briefing --yes
```

Explicit flags always win over profile values; profile values win over defaults. `agent-context` lists all available profiles under `available_profiles` so introspecting agents discover them at runtime.

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 2 | Usage error (wrong arguments) |
| 3 | Resource not found |
| 4 | Authentication required |
| 5 | API error (upstream issue) |
| 6 | Partial failure |
| 7 | Rate limited (wait and retry) |
| 10 | Config error |

`executions wait` uses its own codes: `0` the execution succeeded, `1` it ended in error, crashed, or was canceled, `6` `--max-wait` elapsed first.

## Argument Parsing

Parse `$ARGUMENTS`:

1. **Empty, `help`, or `--help`** → show `n8n-pp-cli --help` output
2. **Starts with `install`** → ends with `mcp` → MCP installation; otherwise → see Prerequisites above
3. **Anything else** → Direct Use (execute as CLI command with `--agent`)

## MCP Server Installation

1. Install the MCP server:
   ```bash
   go install github.com/mvanhorn/printing-press-library/library/developer-tools/n8n/cmd/n8n-pp-mcp@latest
   ```
2. Register with Claude Code:
   ```bash
   claude mcp add n8n-pp-mcp -- n8n-pp-mcp
   ```
3. Verify: `claude mcp list`

## Direct Use

1. Check if installed: `which n8n-pp-cli`
   If not found, offer to install (see Prerequisites at the top of this skill).
2. Match the user query to the best command from the Unique Capabilities and Command Reference above.
3. Execute with the `--agent` flag:
   ```bash
   n8n-pp-cli <command> [subcommand] [args] --agent
   ```
4. If ambiguous, drill into subcommand help: `n8n-pp-cli <command> --help`.
