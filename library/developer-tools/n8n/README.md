# n8n CLI

**Every n8n Public API endpoint, plus a local mirror that answers what is failing, what uses what, and what changed.**

Manage workflows, executions, credentials, data tables, projects, and more on any self-hosted or cloud n8n instance. Sync once and get offline search, failure triage with 'executions failures', cross-workflow usage lookups with 'workflows uses', and node-level diffs between instances with 'workflows diff'.

Learn more at [n8n](https://n8n.io).

Created by [@ElizabethcStone](https://github.com/ElizabethcStone) (Mini).

## Install

The recommended path installs both the `n8n-pp-cli` binary and the `pp-n8n` agent skill (Claude Code, Codex, Cursor, Gemini CLI, GitHub Copilot, and other agents supported by the upstream [`skills`](https://github.com/vercel-labs/skills) CLI) in one shot:

```bash
npx -y @mvanhorn/printing-press-library install n8n
```

For CLI only (no skill):

```bash
npx -y @mvanhorn/printing-press-library install n8n --cli-only
```

For skill only — installs the skill into the same agents as the default command above, but skips the CLI binary (use this to update or reinstall just the skill):

```bash
npx -y @mvanhorn/printing-press-library install n8n --skill-only
```

To constrain the skill install to one or more specific agents (repeatable — agent names match the [`skills`](https://github.com/vercel-labs/skills) CLI):

```bash
npx -y @mvanhorn/printing-press-library install n8n --agent claude-code
npx -y @mvanhorn/printing-press-library install n8n --agent claude-code --agent codex
```

### Without Node (Go fallback)

If `npx` isn't available (no Node, offline), install the CLI directly via Go (requires Go 1.26.9 or newer):

```bash
go install github.com/mvanhorn/printing-press-library/library/developer-tools/n8n/cmd/n8n-pp-cli@latest
```

This installs the CLI only — no skill.

### Pre-built binary

Download a pre-built binary for your platform from the [latest release](https://github.com/mvanhorn/printing-press-library/releases/tag/n8n-current). On macOS, clear the Gatekeeper quarantine: `xattr -d com.apple.quarantine <binary>`. On Unix, mark it executable: `chmod +x <binary>`.

<!-- pp-hermes-install-anchor -->
## Install for Hermes

Install the CLI binary first. The installer writes binaries to a per-user managed bin directory by default: `$HOME/.local/bin` on macOS/Linux and `%LOCALAPPDATA%\Programs\PrintingPress\bin` on Windows.

```bash
npx -y @mvanhorn/printing-press-library install n8n --cli-only
```

Then install the focused Hermes skill.

From the Hermes CLI:

```bash
hermes skills install mvanhorn/printing-press-library/cli-skills/pp-n8n --force
```

Inside a Hermes chat session:

```bash
/skills install mvanhorn/printing-press-library/cli-skills/pp-n8n --force
```

Restart the Hermes session or gateway if the newly installed skill is not visible immediately.

## Install for OpenClaw
Install both the CLI binary and the focused OpenClaw skill. The installer defaults binaries to a per-user bin directory (`$HOME/.local/bin` on macOS/Linux, `%LOCALAPPDATA%\Programs\PrintingPress\bin` on Windows):

```bash
npx -y @mvanhorn/printing-press-library install n8n --agent openclaw
```

Restart the OpenClaw session or gateway if the newly installed skill is not visible immediately.

## Use with Claude Desktop

This CLI ships an [MCPB](https://github.com/modelcontextprotocol/mcpb) bundle — Claude Desktop's standard format for one-click MCP extension installs (no JSON config required).

To install:

1. Download the `.mcpb` for your platform from the [latest release](https://github.com/mvanhorn/printing-press-library/releases/tag/n8n-current).
2. Double-click the `.mcpb` file. Claude Desktop opens and walks you through the install.
3. Fill in `N8N_API_KEY` when Claude Desktop prompts you.

Requires Claude Desktop 1.0.0 or later. Pre-built bundles ship for macOS Apple Silicon (`darwin-arm64`) and Windows (`amd64`, `arm64`); for other platforms, use the manual config below.

<details>
<summary>Manual JSON config (advanced)</summary>

If you can't use the MCPB bundle (older Claude Desktop, unsupported platform), install the MCP binary and configure it manually.


```bash
go install github.com/mvanhorn/printing-press-library/library/developer-tools/n8n/cmd/n8n-pp-mcp@latest
```

Add to your Claude Desktop config (`~/Library/Application Support/Claude/claude_desktop_config.json` on macOS, `%APPDATA%\Claude\claude_desktop_config.json` on Windows):

```json
{
  "mcpServers": {
    "n8n": {
      "command": "n8n-pp-mcp",
      "env": {
        "N8N_API_KEY": "<your-key>"
      }
    }
  }
}
```

</details>

## Authentication

Create an API key in n8n under Settings > n8n API, then export N8N_API_KEY. Point at your instance with N8N_URL=https://n8n.example.com (the same variable the official n8n CLI uses; /api/v1 is appended) or N8N_BASE_URL=https://n8n.example.com/api/v1. The default is http://localhost:5678/api/v1. The public API must be enabled on the instance, and some resources (projects, variables, source control, SSO, LDAP, log streaming, promotions) need an n8n license.

## Quick Start

```bash
# Check configuration without touching the network
n8n-pp-cli doctor --dry-run

# Confirm the API key works and see your workflows
n8n-pp-cli workflows list --agent --select data.id,data.name,data.active

# Build the local mirror the cross-workflow commands read from
n8n-pp-cli sync --resources workflows,executions,credentials --since 7d

# See what broke recently, grouped by workflow and node
n8n-pp-cli executions failures --since 24h

```

## Unique Features

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

## Usage

Run `n8n-pp-cli --help` for the full command reference and flag list.

## Paths & environment variables

This CLI separates local files into four path kinds:

| Kind | Contents |
|------|----------|
| `config` | User-editable settings such as `config.toml` and saved profiles |
| `data` | Durable local data: `credentials.toml`, `data.db`, cookies, browser-session proof files, and other auth sidecars |
| `state` | Runtime state such as persisted queries, jobs, and `teach.log` |
| `cache` | Regenerable HTTP/cache files |

Each kind resolves independently. The ladder is:

1. Per-kind env var: `N8N_CONFIG_DIR`, `N8N_DATA_DIR`, `N8N_STATE_DIR`, or `N8N_CACHE_DIR`
2. `--home <dir>` for this invocation
3. `N8N_HOME` for a flat relocated root
4. XDG env vars: `XDG_CONFIG_HOME`, `XDG_DATA_HOME`, `XDG_STATE_HOME`, `XDG_CACHE_HOME`
5. Platform defaults matching existing installs

For containers and agent sandboxes, prefer a single relocated root:

```bash
export N8N_HOME=/srv/n8n
n8n-pp-cli doctor
```

Under `N8N_HOME=/srv/n8n`, the four dirs resolve to `/srv/n8n/config`, `/srv/n8n/data`, `/srv/n8n/state`, and `/srv/n8n/cache`.

On Windows, set the same variable in PowerShell:

```powershell
$env:N8N_HOME = "C:\srv\n8n"
n8n-pp-cli doctor
```

Under `$env:N8N_HOME = "C:\srv\n8n"`, the four dirs resolve to `C:\srv\n8n\config`, `C:\srv\n8n\data`, `C:\srv\n8n\state`, and `C:\srv\n8n\cache`.

MCP servers do not receive CLI flags from the host. Put relocation in the host `env` block:

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

Precedence matters in fleets: an ambient per-kind variable such as `N8N_DATA_DIR` overrides an explicit `--home` for that kind. Use `N8N_HOME` or the per-kind variables for durable fleet relocation; treat `--home` as the weaker per-invocation lever.

Relocation is one-way. Unsetting `N8N_HOME` does not move files back to platform defaults, and `doctor` cannot find credentials left under a former root. Move the files manually before unsetting relocation variables.

Existing installs keep working because the platform-default rung matches the legacy layout. On the first auth write, stored secrets leave `config.toml` and are consolidated into `credentials.toml` under the data directory. Run `n8n-pp-cli doctor --fail-on warn` to check path and credential-location warnings in automation.

## Commands

### audit

Operations about security audit

- **`n8n-pp-cli audit`** - Generate a security audit for your n8n instance.

### community-packages

Operations about community packages

- **`n8n-pp-cli community-packages install-package`** - Install a community package by npm name and optional version.
- **`n8n-pp-cli community-packages list-installed-packages`** - Retrieve all installed community packages with pending update info.
- **`n8n-pp-cli community-packages uninstall-package`** - Uninstall a community package by name.
- **`n8n-pp-cli community-packages update-package`** - Update an installed community package to a new version.

### credential-type-policies

Manage credential type policies

- **`n8n-pp-cli credential-type-policies create-credential-type-policy-document`** - Creates a reusable policy document that is not yet attached to any scope. Rule ids must be unique within the list. `warnings` lists rules that an earlier rule already shadows.
- **`n8n-pp-cli credential-type-policies delete-credential-type-policy-document`** - Deletes a policy document. A document that is still attached to a scope is rejected with 409; detach it first.
- **`n8n-pp-cli credential-type-policies get-credential-type-instance-policy`** - Returns the composed instance-scope policy: its default action and the rules of every attached policy document in evaluation order. An instance that was never configured reports `scopeId: null`, no rules, `defaultAction: allow`, and `version: 0`.
- **`n8n-pp-cli credential-type-policies get-credential-type-policy-document`** - Retrieve a credential type policy document
- **`n8n-pp-cli credential-type-policies get-credential-type-project-policy`** - Returns the project's own composed policy, not the result of combining it with the instance policy. A project that was never configured reports `scopeId: null`, no rules, `defaultAction: allow`, and `version: 0`.
- **`n8n-pp-cli credential-type-policies list-credential-type-policy-documents`** - Returns a cursor-paginated list of reusable policy documents.
- **`n8n-pp-cli credential-type-policies put-credential-type-instance-policy`** - Sets the instance default action and replaces the rules of its single policy document, creating both on first write. `version` must equal the version last read; a stale value is rejected with 409. Rule ids must be unique within the list.
- **`n8n-pp-cli credential-type-policies put-credential-type-project-policy`** - Sets the project's default action and replaces the rules of its single policy document, creating both on first write. The `delegate` action is not accepted at project scope. `version` must equal the version last read; a stale value is rejected with 409, as is a project document that is shared with another scope.
- **`n8n-pp-cli credential-type-policies replace-credential-type-policy-attachments`** - Replaces every attachment on a scope. `scopeId` comes from the scope's `GET` response once it has been written. Each `policyId` and each `(isFloor, priority)` pair must be unique within the list. Last write wins; the scope's version is bumped.
- **`n8n-pp-cli credential-type-policies update-credential-type-policy-document`** - Replaces the document's whole rule list. `version` must equal the version last read; a stale value is rejected with 409. Every scope the document is attached to has its version bumped. A `delegate` rule is rejected when the document is attached to a project scope.

### credentials

Operations about credentials

- **`n8n-pp-cli credentials create`** - Creates a credential that can be used by nodes of the specified type.
- **`n8n-pp-cli credentials delete`** - Deletes a credential from your instance. You must be the owner of the credential.
- **`n8n-pp-cli credentials get`** - Retrieves a credential by ID. Credential data (secrets) is not included.
- **`n8n-pp-cli credentials get-type`** - Show credential data schema
- **`n8n-pp-cli credentials list`** - Retrieve all credentials from your instance. Only available for the instance owner and admin. Credential data (secrets) is not included.
- **`n8n-pp-cli credentials test`** - Tests a credential by ID using the stored credential data.
- **`n8n-pp-cli credentials transfer`** - Transfer a credential to another project.
- **`n8n-pp-cli credentials update`** - Updates an existing credential. You must be the owner of the credential.

### data-tables

Operations about data tables and their rows

- **`n8n-pp-cli data-tables create`** - Create a new data table in your personal project or a team project you have access to.
- **`n8n-pp-cli data-tables delete`** - Delete a data table. This will also delete all rows in the table.
- **`n8n-pp-cli data-tables get`** - Retrieve a specific data table by ID.
- **`n8n-pp-cli data-tables list`** - Retrieve a list of all data tables with optional filtering, sorting, and pagination. Each table includes `sizeBytes`, the value the `size` sort option orders by.
- **`n8n-pp-cli data-tables update`** - Update a data table's name.

### discover

API capability discovery

- **`n8n-pp-cli discover`** - Returns a filtered capability map based on the caller's API key scopes. Each resource includes the operations and endpoints accessible to the authenticated API key. Use query parameters to narrow the response.

### executions

Operations about executions

- **`n8n-pp-cli executions delete`** - Deletes an execution from your instance.
- **`n8n-pp-cli executions get`** - Retrieve an execution from your instance.
- **`n8n-pp-cli executions get-tags`** - Get annotation tags for an execution.
- **`n8n-pp-cli executions list`** - Retrieve all executions from your instance.
- **`n8n-pp-cli executions retry`** - Retry an execution from your instance.
- **`n8n-pp-cli executions stop`** - Stop an execution by id.
- **`n8n-pp-cli executions stop-many`** - Stop multiple executions from your instance based on filter criteria.
- **`n8n-pp-cli executions update-tags`** - Update annotation tags of an execution.

### insights

Operations about insights

- **`n8n-pp-cli insights`** - Retrieve the insights summary for the selected date range.

### n8n-packages

Beta — breaking changes may still occur without major version bump.

- **`n8n-pp-cli n8n-packages create`** - **Beta** — breaking changes may still occur without major version bump.

Export agents, workflows, and folders, or whole projects, as a gzipped tar
archive (.n8np). Provide `agentIds`/`workflowIds`/`folderIds`, or `projectIds`.
Do not combine these two groups. Each exported folder includes its nested
folders. Whole projects include their agents when the agents module is enabled.
Explicit agent selections require the agents module. Empty projects export
project metadata only. `versionPolicy` selects the version for both agents and
workflows and defaults to `latest`. `dependencyPolicy` applies to both entity
types and defaults to `fail` when a required definition is absent from the
package. The response is streamed as `application/gzip` with a `Content-Disposition`
attachment header. Requires the n8n Packages feature to be licensed.

API key scopes: `agent:export` is required for explicitly selected agents.
`workflow:export` is required for explicitly selected workflows or folders.
`project:export` is sufficient for whole-project exports, including their
agents and workflows. Included dependencies need no additional API key scope.
The caller's project permissions apply to all included entities. When
`includeVariableValues` is true (the default) and exported entities reference
variables, `variable:list` is also required. Exports that reference no variables
do not need it.

Agent definitions include referenced skills, custom tools, and schedules.
Knowledge base files and runtime data are excluded. Agent package imports
remain unavailable.
- **`n8n-pp-cli n8n-packages import-package`** - **Beta** — breaking changes may still occur without major version bump.

Imports a gzip-compressed tar package (`.n8np`) into the target project. Send the archive as the multipart field `package`. Optional routing uses form fields `projectId` and `folderId` (omit or send empty for defaults). Every optional mode/policy field (credential, workflow, project, folder, data table, variable, and tag) takes its default when omitted; the one non-fixed default is `folderConflictPolicy`, which follows `projectConflictPolicy` on a project package. The required `workflowConflictPolicy` field controls what happens when a package workflow matches an existing workflow by source id in the target project. Maximum upload size is `N8N_ENDPOINTS_PAYLOAD_SIZE_MAX` MB (default 16).

The package must declare its manifest at `manifest.json` and include every file the manifest references.

Credential references are resolved before any workflow is written.
- **`n8n-pp-cli n8n-packages import-package-selection`** - **Beta** — breaking changes may still occur without major version bump.

Imports a chosen subset of workflows from one source project of a gzip-compressed tar package (`.n8np`), instead of the whole package. Send the archive as the multipart field `package`. The import is **additive** (cherry-pick): it never removes a workflow the selection omits. It creates or updates only the workflows named in `selectedWorkflowIds`, and deletes only the destination workflows named in `deletedWorkflowIds`.

`selectedProjectId` names the single source project the selection is scoped to (its id as it appears in the package). The selected workflows are written into the instance project whose id matches that source project, which is created if it does not yet exist; there is no separate target-project field. `selectedWorkflowIds` are source ids within that project; ids that belong to another project are dropped. `deletedWorkflowIds` are destination ids in the same project; an absent id is a tolerated no-op, and a delete needs the `workflow:delete` scope. A delete id must not match the destination id of a selected workflow, including when `workflowConflictPolicy=skip`. This overlap returns a 409 before any writes.

The package must be a project package (a workflow package is rejected with a 400). The cherry-pick conflict policies (folder, tag, and project) are fixed and are not accepted here; only `workflowConflictPolicy`, `workflowIdPolicy`, and `overwriteDeletionPolicy` are configurable. Maximum upload size is `N8N_ENDPOINTS_PAYLOAD_SIZE_MAX` MB (default 16). The caller is authorised through the `workflow:import` scope.

### node-type-policies

Manage node type policies

- **`n8n-pp-cli node-type-policies create-node-type-policy-document`** - Creates a reusable policy document that is not yet attached to any scope. Rule ids must be unique within the list. `warnings` lists rules that an earlier rule already shadows.
- **`n8n-pp-cli node-type-policies delete-node-type-policy-document`** - Deletes a policy document. A document that is still attached to a scope is rejected with 409; detach it first.
- **`n8n-pp-cli node-type-policies get-node-type-instance-policy`** - Returns the composed instance-scope policy: its default action and the rules of every attached policy document in evaluation order. An instance that was never configured reports `scopeId: null`, no rules, `defaultAction: allow`, and `version: 0`.
- **`n8n-pp-cli node-type-policies get-node-type-policy-document`** - Retrieve a node type policy document
- **`n8n-pp-cli node-type-policies get-node-type-project-policy`** - Returns the project's own composed policy, not the result of combining it with the instance policy. A project that was never configured reports `scopeId: null`, no rules, `defaultAction: allow`, and `version: 0`.
- **`n8n-pp-cli node-type-policies list-node-type-policy-documents`** - Returns a cursor-paginated list of reusable policy documents.
- **`n8n-pp-cli node-type-policies put-node-type-instance-policy`** - Sets the instance default action and replaces the rules of its single policy document, creating both on first write. `version` must equal the version last read; a stale value is rejected with 409. Rule ids must be unique within the list.
- **`n8n-pp-cli node-type-policies put-node-type-project-policy`** - Sets the project's default action and replaces the rules of its single policy document, creating both on first write. The `delegate` action is not accepted at project scope. `version` must equal the version last read; a stale value is rejected with 409, as is a project document that is shared with another scope.
- **`n8n-pp-cli node-type-policies replace-node-type-policy-attachments`** - Replaces every attachment on a scope. `scopeId` comes from the scope's `GET` response once it has been written. Each `policyId` and each `(isFloor, priority)` pair must be unique within the list. Last write wins; the scope's version is bumped.
- **`n8n-pp-cli node-type-policies update-node-type-policy-document`** - Replaces the document's whole rule list. `version` must equal the version last read; a stale value is rejected with 409. Every scope the document is attached to has its version bumped. A `delegate` rule is rejected when the document is attached to a project scope.

### projects

Operations about projects

- **`n8n-pp-cli projects create`** - Create a team project on your instance.
- **`n8n-pp-cli projects delete`** - Delete a project from your instance.
- **`n8n-pp-cli projects list`** - Retrieve projects from your instance.
- **`n8n-pp-cli projects update`** - Update a project on your instance.

### promotions

Operations about promotion providers, connections, and package operations

- **`n8n-pp-cli promotions add-project-to-connection`** - Links a team project to a project-scoped connection. A project can be linked to only one connection.
- **`n8n-pp-cli promotions apply-package`** - Checks the full package at the configured branch tip. Optionally send expectedSource with the configId, branchName, and full commitSha from the reviewed change preview; status `source-changed` means the branch moved since that review and nothing was imported. Returns status `blocked` before import writes if bindings need setup. Retain configId and git for Continue. Status `applied` includes counts and warnings. Inspect status before reading counts. Existing target variable values, including empty strings, are preserved. Requires a cloned Apply direction on an instance connection. The API key needs the gitConnection:pull scope. The importer checks user write permissions; granular API-key write scopes are not passed to it.
- **`n8n-pp-cli promotions apply-project-selection`** - Applies selected workflow changes from the project's Apply branch. Send the selected change-list IDs in workflowIds. The server imports IDs present on the branch for this project. It removes instance workflows owned by this project when their IDs are absent from the branch. Duplicate or invalid IDs reject the whole request. Unselected content stays unchanged. Optionally send expectedSource with the configId, branchName, and full commitSha from the reviewed change preview. Status `source-changed` means the source changed since that review and nothing was imported. Status `blocked` returns binding details before import writes when the selection needs binding setup. Retain configId and git for Continue. Status `applied` includes counts and warnings. Inspect status before reading counts. Requires a cloned Apply direction for the project's resolved promotion connection. The API key needs the gitConnection:pull scope. The importer checks user write permissions.
- **`n8n-pp-cli promotions clone-checkout`** - Clones the configured branch into local storage on the instance that handles the request. Safe to call repeatedly. Cloning one direction does not make the other ready.
- **`n8n-pp-cli promotions continue-apply-package`** - Rechecks the configured source and current target bindings. Send expectedSource with the configId, branchName, and full commitSha from the reviewed Apply result. Send `confirmDestructiveChanges: true` to also apply destructive data table changes. Status `source-changed` requires a new Apply review. Status `blocked` returns fresh binding details without import writes. Status `applied` includes counts and warnings. Inspect status before reading counts. Existing target variable values are preserved. Requires the same cloned instance connection and permissions as Apply. The importer checks user write permissions; granular API-key write scopes are not passed to it. No server session or exactly-once guarantee is provided.
- **`n8n-pp-cli promotions continue-apply-project-selection`** - Rechecks the project's Apply source and the current bindings for the selection. Resend the full selection in workflowIds. Send the required expectedSource with the configId, branchName, and full commitSha from the reviewed Apply result. Send `confirmDestructiveChanges: true` to also apply destructive data table changes. The server does not keep a session. Status `source-changed` requires a new Apply review. Status `blocked` returns fresh binding details before import writes. Status `applied` includes counts and warnings. Inspect status before reading counts. Unselected content stays unchanged. Requires the same cloned Apply direction and permissions as the initial selection apply. The API key needs the gitConnection:pull scope. The importer checks user write permissions.
- **`n8n-pp-cli promotions create-connection`** - Creates a connection on an existing provider, with up to one initial configuration for each direction. Only one instance connection can exist.
- **`n8n-pp-cli promotions create-provider`** - Creates a provider and its authentication material. A Git host provider validates authenticated API access before it saves. For GitLab, the service uses n8n as the Git transport username. Its auth.username is optional. The service ignores any supplied username. Plain Git token authentication requires auth.username. An SSH provider returns the generated public key; add it to the remote as a deploy key.
- **`n8n-pp-cli promotions delete-config`** - Deletes the configuration and its local checkout. Nothing in Git changes.
- **`n8n-pp-cli promotions delete-connection`** - Deletes the connection with its configurations, project links, and local checkouts. Its provider is kept.
- **`n8n-pp-cli promotions delete-provider`** - A provider used by a connection cannot be deleted.
- **`n8n-pp-cli promotions disconnect-checkout`** - Removes the local checkout and keeps the configuration, its credentials, and the trusted SSH host keys.
- **`n8n-pp-cli promotions get-changes`** - Compares a team project on this instance with the branch of its promotion configuration and lists the workflows that differ. For `promote` the rows are what a promotion sends to the branch, and the key needs the gitConnection:push scope. For `apply` the rows are what applying the branch changes on this instance, and the key needs the gitConnection:pull scope. `commitSha` is the commit the rows were read from. Requires the direction to be cloned first.
- **`n8n-pp-cli promotions get-connection`** - Returns the connection with its provider summary and its configurations.
- **`n8n-pp-cli promotions get-connection-projects`** - List projects linked to a promotion connection
- **`n8n-pp-cli promotions get-provider`** - Returns the provider with its public configuration. Secrets are never returned.
- **`n8n-pp-cli promotions list-connections`** - Returns a cursor-paginated list of connections with their configurations. Filter by `scope` for the instance connection, or by `providerId` to see which connections a provider edit affects.
- **`n8n-pp-cli promotions list-providers`** - Returns a cursor-paginated list of providers, without their public configuration.
- **`n8n-pp-cli promotions promote-package`** - Exports every team project, commits it, and pushes to the configured base branch or a new timestamped branch. The response reports the target branch in `git.branchName`. Personal projects are ignored. Requires the Promote direction to be cloned first, and is available on the instance connection only. The API key also needs variable:list when the workflows reference variables.
- **`n8n-pp-cli promotions promote-project-selection`** - Promotes a chosen set of a team project's workflows to the Promote branch of the project's promotion configuration. Send workflow ids only; the server reads each one now, so the push carries the current state. Live and archived workflows the project owns are exported, so an archived id stays on the branch as archived; an id the project no longer owns (its workflow is gone, or moved to another project) leaves the branch, matching the project's change list. A deletion the branch does not hold under this project rejects the whole request before any write. Requires the Promote direction to be cloned first, and a promotion connection to resolve for the project (its own connection, otherwise the instance connection). The API key also needs variable:list when the workflows reference variables.
- **`n8n-pp-cli promotions remove-project-from-connection`** - Unlink a project from a promotion connection
- **`n8n-pp-cli promotions update-connection`** - Updates the name, target, or provider. Scope cannot change, and configurations have their own routes.
- **`n8n-pp-cli promotions update-provider`** - Renames the provider, and replaces its credentials when `auth` is sent. A Git host provider can also update its base URL. A different API destination requires `auth` in the same request. Changed credentials and host settings are validated before they save. If another request changes the credentials or configuration before this update saves, the update returns 409. Reload the provider and try again. For GitLab, the service uses n8n as the Git transport username. Its auth.username is optional. The service ignores any supplied username. Plain Git token authentication requires auth.username. New credentials apply to every connection that uses this provider. The authentication method cannot change.
- **`n8n-pp-cli promotions upsert-apply-config`** - Replaces the whole configuration, so an omitted name resets it to "Apply". Answers 200 whether it created or replaced.
- **`n8n-pp-cli promotions upsert-promote-config`** - Replaces the whole configuration, so an omitted name resets it to "Promote" and `createBranchOnPromotion` is always required. Answers 200 whether it created or replaced.

### role-mapping-rules

Operations about identity-provider role-mapping rules

- **`n8n-pp-cli role-mapping-rules create`** - Creates a rule that maps an identity-provider claim expression to a role. Set `type` to `instance` for a rule granting a global role, or `project` for a rule granting a project role on the projects named in `projectIds`. Omitting `order` appends the rule to the end of the evaluation order for its type.
- **`n8n-pp-cli role-mapping-rules delete`** - Deletes a role-mapping rule. The remaining rules of the same type close the gap, so their `order` values stay a contiguous sequence starting at 0.
- **`n8n-pp-cli role-mapping-rules list`** - Returns the configured role-mapping rules. `order` is the rule's evaluation position within its own `type`, so instance and project rules each have their own sequence starting at 0 — filter by `type` to retrieve a single evaluation order.
- **`n8n-pp-cli role-mapping-rules update`** - Updates a rule's claim expression, role, and/or project assignments. A rule's type cannot be changed once created, so `type` isn't accepted here, and reordering is handled by the move endpoint.

### roles

Operations about roles

- **`n8n-pp-cli roles create`** - Creates a custom role. Set `roleType` to `global` for an instance-wide role or `project` for a project role.
- **`n8n-pp-cli roles delete`** - Deletes a custom role. System roles cannot be deleted. A role with users assigned cannot be deleted unless `reassignRoleSlug` is set to move those users to another role first.
- **`n8n-pp-cli roles get`** - Returns a single role with its scopes. Set `withUsageCount` to include how many users and projects use the role.
- **`n8n-pp-cli roles list`** - Returns all roles grouped by type (global and project). Set `withUsageCount` to include how many users and projects use each role.
- **`n8n-pp-cli roles update`** - Replaces a custom role's display name, description, and scopes. System roles cannot be updated.

### settings

Manage settings

- **`n8n-pp-cli settings create-log-streaming-destination`** - Create a log streaming destination. The destination takes effect exactly as it would from the UI, using the same validation. Requires the `eventBusDestination:create` scope and the Log Streaming feature to be licensed. When destinations are managed via environment variables, the write is rejected with 409 and nothing is created; reads still return the current values.
- **`n8n-pp-cli settings delete-log-streaming-destination`** - Remove a log streaming destination. Requires the `eventBusDestination:delete` scope and the Log Streaming feature to be licensed. When destinations are managed via environment variables, the delete is rejected with 409 and nothing is removed; reads still return the current values.
- **`n8n-pp-cli settings get-ldap-configuration`** - Retrieve the current LDAP configuration, including every field exposed in the UI. The binding admin password is redacted on read. Requires the `ldap:manage` scope and the LDAP feature to be licensed.
- **`n8n-pp-cli settings get-ldap-sync`** - Retrieve the history of LDAP synchronizations, most recent first. Requires the `ldap:sync` scope and the LDAP feature to be licensed.
- **`n8n-pp-cli settings get-log-streaming-destination`** - Retrieve a single log streaming destination by id. Requires the `eventBusDestination:read` scope and the Log Streaming feature to be licensed.
- **`n8n-pp-cli settings get-log-streaming-event-types`** - List the event types that can be streamed to a log streaming destination. Requires the `eventBusDestination:list` scope and the Log Streaming feature to be licensed.
- **`n8n-pp-cli settings get-oidc-configuration`** - Retrieve the current OIDC SSO configuration, including every field exposed in the UI. The client secret is redacted on read and is never echoed back in plaintext. Requires the `oidc:manage` scope and the OIDC feature to be licensed.
- **`n8n-pp-cli settings get-otel`** - Retrieve the current OpenTelemetry configuration, including every field exposed in the UI. Requires the `otel:manage` scope.
- **`n8n-pp-cli settings get-saml-configuration`** - Retrieve the current SAML SSO configuration, including every field exposed in the UI plus the service provider entity ID and ACS return URL. Signing private keys, signing certificates, and identity provider metadata are redacted on read. Requires the `saml:manage` scope and the SAML feature to be licensed.
- **`n8n-pp-cli settings get-security-policy`** - Retrieve the instance security policy: personal-space publishing and sharing, the execution-data redaction enforcement floor, and the read-only usage counts shown in the UI. Requires the `securitySettings:manage` scope and the Personal Space Policy feature to be licensed.
- **`n8n-pp-cli settings list-log-streaming-destinations`** - List the configured log streaming destinations. Requires the `eventBusDestination:list` scope and the Log Streaming feature to be licensed.
- **`n8n-pp-cli settings run-ldap-sync`** - Manually trigger an LDAP synchronization. The response returns the new sync history record. Requires the `ldap:sync` scope and the LDAP feature to be licensed.
- **`n8n-pp-cli settings set-oidc-configuration`** - Set the OIDC SSO configuration. The update takes effect exactly as it would from the UI, using the same validation. `clientId`, `clientSecret` and `discoveryEndpoint` are required; submit the redacted client secret sentinel to keep the stored secret unchanged. Requires the `oidc:manage` scope and the OIDC feature to be licensed. The client secret is redacted in the response. When the configuration is managed declaratively (via environment variables), the write is rejected with 409 and no changes are made.
- **`n8n-pp-cli settings test-log-streaming-destination`** - Send a test message to the destination to verify it is reachable and configured correctly. Requires the `eventBusDestination:test` scope and the Log Streaming feature to be licensed.
- **`n8n-pp-cli settings test-otel-trace`** - Send a single test span to the given OTLP collector and report whether it was accepted. This tests the supplied connection details without changing the stored configuration. Fields managed declaratively via environment variables are overridden with their effective value before the test is sent. Requires the `otel:manage` scope.
- **`n8n-pp-cli settings update-ldap-configuration`** - Replace the LDAP configuration with the provided full object (partial updates are not supported). For bindingAdminPassword, submit the blanking placeholder from a prior GET to keep the stored password unchanged. Requires the `ldap:manage` scope and the LDAP feature to be licensed. Setting loginEnabled to false is destructive and it deletes all stored LDAP user identities and disables synchronization.
- **`n8n-pp-cli settings update-log-streaming-destination`** - Replace an existing log streaming destination. The update takes effect exactly as it would from the UI, using the same validation. Requires the `eventBusDestination:update` scope and the Log Streaming feature to be licensed. When destinations are managed via environment variables, the write is rejected with 409 and nothing is changed; reads still return the current values.
- **`n8n-pp-cli settings update-otel`** - Set the OpenTelemetry configuration. This is a full replacement: every field must be provided, and a partial body is rejected. The exceptions are `exporterProtocol`, which defaults to `http/protobuf` when omitted, and `emitWorkflowStartSpan` and `emitNodeStartSpan`, which default to `false` when omitted. The update takes effect exactly as it would from the UI, using the same validation, and is applied to the running instance immediately. Fields managed declaratively via environment variables are read-only: attempting to change one is rejected with 409, while re-submitting its current value (as returned by GET) is accepted. Requires the `otel:manage` scope.
- **`n8n-pp-cli settings update-saml-configuration`** - Replace the SAML SSO configuration with the provided full object. Every writable field must be sent; use empty strings or empty arrays when a value is unset. Read-only `entityID` / `returnUrl` from GET are ignored if included, so a GET response can be sent back as a PUT body. Redacted secret placeholders keep the stored values unchanged. The update takes effect exactly as it would from the UI, using the same validation. Requires the `saml:manage` scope and the SAML feature to be licensed. When the configuration is managed via environment variables, the write is rejected with 409 and no changes are made.
- **`n8n-pp-cli settings update-security-policy`** - Replace the instance security policy with the provided full object. Every writable field must be sent. Read-only usage counts from GET are ignored if included, so a GET response can be sent back as a PUT body. The update takes effect exactly as it would from the UI, using the same validation. Requires the `securitySettings:manage` scope and the Personal Space Policy feature to be licensed. When the group is managed via environment variables, the write is rejected with 409 and no changes are made; a read still returns the current values.

### source-control

Operations about source control

- **`n8n-pp-cli source-control get-status`** - Previews the pending changes between the instance and the connected Git branch in either the `push` or `pull` direction.
- **`n8n-pp-cli source-control pull`** - Fetches changes from the connected Git branch into the instance.
- **`n8n-pp-cli source-control push`** - Commits and pushes the selected files to the connected Git branch. Each entry in `fileNames` is resolved against a fresh preview of the pending changes.

### tags

Operations about tags

- **`n8n-pp-cli tags create`** - Create a tag in your instance.
- **`n8n-pp-cli tags delete`** - Deletes a tag.
- **`n8n-pp-cli tags get`** - Retrieves a tag.
- **`n8n-pp-cli tags list`** - Retrieve all tags from your instance.
- **`n8n-pp-cli tags update`** - Update a tag.

### users

Operations about users

- **`n8n-pp-cli users create`** - Create one or more users.
- **`n8n-pp-cli users delete`** - Delete a user from your instance.
- **`n8n-pp-cli users get`** - Retrieve a user from your instance. Only available for the instance owner.
- **`n8n-pp-cli users list`** - Retrieve all users from your instance. Only available for the instance owner.

### variables

Operations about variables

- **`n8n-pp-cli variables create`** - Create a variable in your instance.
- **`n8n-pp-cli variables delete`** - Delete a variable from your instance.
- **`n8n-pp-cli variables list`** - Retrieve variables from your instance.
- **`n8n-pp-cli variables update`** - Update a variable from your instance.

### workflows

Operations about workflows

- **`n8n-pp-cli workflows activate`** - Deprecated: use POST /workflows/{id}/publish instead. Publish a workflow. In n8n v1, this action was termed activating a workflow.
- **`n8n-pp-cli workflows archive`** - Soft-deletes a workflow by archiving it. Idempotent: archiving an already archived workflow returns 200 with the current workflow.
- **`n8n-pp-cli workflows create`** - Create a workflow in your instance.
- **`n8n-pp-cli workflows deactivate`** - Deprecated: use POST /workflows/{id}/unpublish instead. Deactivate a workflow.
- **`n8n-pp-cli workflows delete`** - Delete a workflow.
- **`n8n-pp-cli workflows get`** - Retrieve a workflow.
- **`n8n-pp-cli workflows get-deprecated-version`** - Deprecated: use GET /workflows/{workflowId}/versions/{workflowVersionId} instead. Retrieves a specific version of a workflow from workflow history.
- **`n8n-pp-cli workflows get-history`** - Returns a paginated list of workflow versions (version IDs and metadata) for a workflow.
- **`n8n-pp-cli workflows get-tags`** - Get workflow tags.
- **`n8n-pp-cli workflows get-version`** - Retrieve a single version of a workflow from its version history.
- **`n8n-pp-cli workflows list`** - Retrieve all workflows from your instance.
- **`n8n-pp-cli workflows publish`** - Publish a workflow. In n8n v1, this action was termed activating a workflow.
- **`n8n-pp-cli workflows transfer`** - Transfer a workflow to another project.
- **`n8n-pp-cli workflows unarchive`** - Restores an archived workflow.
- **`n8n-pp-cli workflows unpublish`** - Unpublish a workflow. In n8n v1, this action was termed deactivating a workflow.
- **`n8n-pp-cli workflows update`** - Update a workflow. If the workflow is published, the updated version will be automatically re-published unless `publishIfActive` is set to `false`. Because that re-publication puts a new version live, it additionally requires the `workflow:activate` API key scope and the `workflow:publish` project permission. A caller without either can still save: the new version is stored as a draft and the response is a `403` naming the missing permission, leaving the published version live. Saving an unpublished workflow, or saving with `publishIfActive=false`, only needs `workflow:update`.
- **`n8n-pp-cli workflows update-tags`** - Update tags of a workflow.


### Hand-written commands

- **`n8n-pp-cli workflows patch`** - Edit one node by name (set a parameter, rename, enable or disable); previews the node-level diff and writes only with `--apply`.
- **`n8n-pp-cli workflows push`** - Push a local workflow JSON file; previews the diff and writes only with `--apply` (`--create` to create a new workflow).
- **`n8n-pp-cli workflows export`** - Write every workflow to `--dir` as one stable, git-friendly JSON file per workflow.
- **`n8n-pp-cli workflows uses`** - Find every workflow and node that uses a credential, node type, webhook path, variable, or sub-workflow.
- **`n8n-pp-cli workflows stale`** - Find active workflows with no recent successful run, workflows that never ran, and active workflows without an error workflow.
- **`n8n-pp-cli workflows diff`** - Compare a workflow across two instances or two saved versions at the node and connection level, ignoring layout noise.
- **`n8n-pp-cli executions failures`** - See what broke recently, grouped by workflow and failing node, with the latest execution to retry.
- **`n8n-pp-cli executions wait`** - Block until an execution finishes and exit non-zero if it failed, with the failing node and message.
- **`n8n-pp-cli credentials usage`** - List unused credentials, credentials many workflows depend on, and workflow references to credentials that no longer exist.

The local-store commands (`workflows uses`, `workflows stale`, `credentials usage`, `executions failures`) read the synced mirror: run `n8n-pp-cli sync --resources workflows,executions,credentials` first.


### Self-learning loop

This CLI caches per-question discovery so repeat queries skip the walk and structurally similar queries get answered via entity substitution. The loop also self-captures: every invocation is journaled locally, and failed-flag corrections plus fresh teaches surface as candidates on the next `recall` for confirm/reject judgment. Agents call `recall` before discovery and fire `teach &` after answering. See the `## Automatic learning` section in `SKILL.md` for the full protocol.

- **`n8n-pp-cli recall <query>`** - Look up cached resources for a query before running discovery
- **`n8n-pp-cli teach`** - Record a query -> resource mapping (silent on success, safe to background with `&`)
- **`n8n-pp-cli learnings list`** - Inspect taught rows
- **`n8n-pp-cli learnings forget <query>`** - Undo a teach
- **`n8n-pp-cli learnings candidates`** - List auto-captured candidates awaiting confirm/reject
- **`n8n-pp-cli learnings stats`** - Local loop metrics: recall hit rate, teach-to-reuse, playbook resolution, candidate counts
- **`n8n-pp-cli teach-pattern`** - Install a query/resource template up front
- **`n8n-pp-cli teach-lookup`** - Add an entity mapping (e.g. country code, team alias) for pattern substitution

Pass `--no-learn` or set `N8N_NO_LEARN=true` to disable the loop for deterministic flows.

The local store's schema version stamp is one-way: once this version of `n8n-pp-cli` opens the database, older binaries refuse it with a version error — upgrade the binary rather than downgrading.

## Output Formats

```bash
# Human-readable table (default in terminal, JSON when piped)
n8n-pp-cli credentials list

# JSON for scripting and agents
n8n-pp-cli credentials list --json
# Filter to specific fields
n8n-pp-cli credentials list --json --select createdAt,id,name

# Dry run — show the request without sending
n8n-pp-cli credentials list --dry-run

# Agent mode — JSON + compact + no prompts in one flag
n8n-pp-cli credentials list --agent
```

`--csv` prefixes a leading `'` on cells and header keys that start with `=`, `+`, `-`, `@`, a tab, or a line break (including fullwidth `＝＋－＠`) so a spreadsheet does not run them as formulas. Plain signed numbers such as `-5` and `+2.5e3` are left unchanged.

## Agent Usage

This CLI is designed for AI agent consumption:

- **Non-interactive** - never prompts, every input is a flag
- **Pipeable** - `--json` output to stdout, errors to stderr
- **Filterable** - `--select <field>[,<field>...]` returns only fields you need
- **Previewable** - `--dry-run` shows the request without sending
- **Explicit retries** - add `--idempotent` to create retries and add `--ignore-missing` to delete retries when a no-op success is acceptable
- **Explicit confirmation** - `--agent` does not imply `--yes`; pass `--yes` separately only after the target, arguments, and side effects are clear
- **Piped input** - write commands can accept structured input when their help lists `--stdin`
- **Offline-friendly** - sync/search commands can use the local SQLite store when available
- **Agent-safe by default** - no colors or formatting unless `--human-friendly` is set

Exit codes: `0` success, `2` usage error, `3` not found, `4` auth error, `5` API error, `6` partial failure, `7` rate limited, `10` config error.

`executions wait` uses its own codes: `0` the execution succeeded, `1` it ended in error, crashed, or was canceled, `6` `--max-wait` elapsed first.

## Freshness

This CLI owns bounded freshness for registered store-backed read command paths. In `--data-source auto` mode, covered commands check the local SQLite store before serving results; stale or missing resources trigger a bounded refresh, and refresh failures fall back to the existing local data with a warning. `--data-source local` never refreshes, and `--data-source live` reads the API without mutating the local store.

Set `N8N_NO_AUTO_REFRESH=1` to disable the pre-read freshness hook while preserving the selected data source.

Covered command paths:
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

JSON outputs that use the generated provenance envelope include freshness metadata at `meta.freshness`. This metadata describes the freshness decision for the covered command path; it does not claim full historical backfill or API-specific enrichment.

## Health Check

```bash
n8n-pp-cli doctor
```

Verifies configuration, credentials, and connectivity to the API.

## Configuration

Run `n8n-pp-cli doctor` to see the resolved config, data, state, and cache directories. The platform-default config path is `~/.config/n8n-pp-cli/config.toml` on macOS/Linux and `%USERPROFILE%\.config\n8n-pp-cli\config.toml` on Windows; `--home`, `N8N_HOME`, and per-kind env vars can relocate it.

Static request headers can be configured under `headers`; per-command header overrides take precedence.

Environment variables:

| Name | Kind | Required | Description |
| --- | --- | --- | --- |
| `N8N_API_KEY` | per_call | Yes | Set to your API credential. |
| `N8N_URL` | config | No | Instance root, e.g. `https://n8n.example.com`; `/api/v1` is appended (same variable as the official n8n CLI). |
| `N8N_BASE_URL` | config | No | Full API base including `/api/v1`; wins over `N8N_URL`. Default `http://localhost:5678/api/v1`. |
| `N8N_HOME` | config | No | Relocates config, data, state, and cache directories. |

### agentcookie (optional)

If you use agentcookie to sync secrets across machines, this CLI auto-adopts agentcookie-managed credentials with no extra setup. When the daemon writes to this CLI's config, `n8n-pp-cli doctor` reports `agentcookie: detected` and `auth status` labels the source as `agentcookie`. Skip this section if you don't use agentcookie - the CLI works the same as any other.

## Troubleshooting
**Authentication errors (exit code 4)**
- Run `n8n-pp-cli doctor` to check credentials
- Verify the environment variable is set: `echo $N8N_API_KEY`
**Not found errors (exit code 3)**
- Check the resource ID is correct
- Run the `list` command to see available items

### API-specific
- **401 Unauthorized** — Set N8N_API_KEY to a key from Settings > n8n API on the same instance as N8N_BASE_URL
- **404 on every request** — N8N_BASE_URL must end in /api/v1 (for example https://n8n.example.com/api/v1); or set N8N_URL to the bare instance root and /api/v1 is added for you
- **403 with a license message** — That resource needs an n8n Enterprise or Pro license; use the community-edition resources instead
- **workflows uses, workflows stale, credentials usage, or executions failures returns nothing** — Run n8n-pp-cli sync --resources workflows,executions,credentials first

## Sources & Inspiration

This CLI was built by studying these projects and resources:

- [**n8n-mcp**](https://github.com/czlonkowski/n8n-mcp) — TypeScript (23102 stars)
- [**n8n-mcp-server**](https://github.com/leonardsellem/n8n-mcp-server) — TypeScript (1630 stars)
- [**n8n-cli (n8n-toolkit)**](https://github.com/8Dvibes/n8n-cli) — Python (17 stars)
- [**n8nc**](https://github.com/rvben/n8nc) — Python (2 stars)
- [**@n8n/cli (official)**](https://github.com/n8n-io/n8n/tree/master/packages/@n8n/cli) — TypeScript

Generated by [CLI Printing Press](https://github.com/mvanhorn/cli-printing-press)
