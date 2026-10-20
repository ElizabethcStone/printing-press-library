# Airtable CLI

**Airtable REST API CLI with offline cross-base search, CSV merge-upserts, field-level diffs before bulk writes, and schema drift detection.**

Airtable's wrappers are libraries, and its official MCP is an online-only tool surface without delete, comments, webhooks, attachments, or a local store. This CLI mirrors your bases into local SQLite for full-text search across every base at once, runs CSV-to-merge-upserts, shows field-level diffs before bulk writes, and serves the whole API to agents over stdio or HTTP MCP.

## Install

The recommended path installs both the `airtable-pp-cli` binary and the `pp-airtable` agent skill (Claude Code, Codex, Cursor, Gemini CLI, GitHub Copilot, and other agents supported by the upstream [`skills`](https://github.com/vercel-labs/skills) CLI) in one shot:

```bash
npx -y @mvanhorn/printing-press-library install airtable
```

For CLI only (no skill):

```bash
npx -y @mvanhorn/printing-press-library install airtable --cli-only
```

For skill only — installs the skill into the same agents as the default command above, but skips the CLI binary (use this to update or reinstall just the skill):

```bash
npx -y @mvanhorn/printing-press-library install airtable --skill-only
```

To constrain the skill install to one or more specific agents (repeatable — agent names match the [`skills`](https://github.com/vercel-labs/skills) CLI):

```bash
npx -y @mvanhorn/printing-press-library install airtable --agent claude-code
npx -y @mvanhorn/printing-press-library install airtable --agent claude-code --agent codex
```

### Without Node (Go fallback)

If `npx` isn't available (no Node, offline), install the CLI directly via Go (requires Go 1.26.9 or newer):

```bash
go install github.com/mvanhorn/printing-press-library/library/productivity/airtable/cmd/airtable-pp-cli@latest
```

This installs the CLI only — no skill.

### Pre-built binary

Download a pre-built binary for your platform from the [latest release](https://github.com/mvanhorn/printing-press-library/releases/tag/airtable-current). On macOS, clear the Gatekeeper quarantine: `xattr -d com.apple.quarantine <binary>`. On Unix, mark it executable: `chmod +x <binary>`.

<!-- pp-hermes-install-anchor -->
## Install for Hermes

Install the CLI binary first. The installer writes binaries to a per-user managed bin directory by default: `$HOME/.local/bin` on macOS/Linux and `%LOCALAPPDATA%\Programs\PrintingPress\bin` on Windows.

```bash
npx -y @mvanhorn/printing-press-library install airtable --cli-only
```

Then install the focused Hermes skill.

From the Hermes CLI:

```bash
hermes skills install mvanhorn/printing-press-library/cli-skills/pp-airtable --force
```

Inside a Hermes chat session:

```bash
/skills install mvanhorn/printing-press-library/cli-skills/pp-airtable --force
```

Restart the Hermes session or gateway if the newly installed skill is not visible immediately.

## Install for OpenClaw
Install both the CLI binary and the focused OpenClaw skill. The installer defaults binaries to a per-user bin directory (`$HOME/.local/bin` on macOS/Linux, `%LOCALAPPDATA%\Programs\PrintingPress\bin` on Windows):

```bash
npx -y @mvanhorn/printing-press-library install airtable --agent openclaw
```

Restart the OpenClaw session or gateway if the newly installed skill is not visible immediately.

## Use with Claude Desktop

This CLI ships an [MCPB](https://github.com/modelcontextprotocol/mcpb) bundle — Claude Desktop's standard format for one-click MCP extension installs (no JSON config required).

To install:

1. Download the `.mcpb` for your platform from the [latest release](https://github.com/mvanhorn/printing-press-library/releases/tag/airtable-current).
2. Double-click the `.mcpb` file. Claude Desktop opens and walks you through the install.
3. Fill in `AIRTABLE_API_KEY` when Claude Desktop prompts you.

Requires Claude Desktop 1.0.0 or later. Pre-built bundles ship for macOS Apple Silicon (`darwin-arm64`) and Windows (`amd64`, `arm64`); for other platforms, use the manual config below.

<details>
<summary>Manual JSON config (advanced)</summary>

If you can't use the MCPB bundle (older Claude Desktop, unsupported platform), install the MCP binary and configure it manually.


```bash
go install github.com/mvanhorn/printing-press-library/library/productivity/airtable/cmd/airtable-pp-mcp@latest
```

Add to your Claude Desktop config (`~/Library/Application Support/Claude/claude_desktop_config.json` on macOS, `%APPDATA%\Claude\claude_desktop_config.json` on Windows):

```json
{
  "mcpServers": {
    "airtable": {
      "command": "airtable-pp-mcp",
      "env": {
        "AIRTABLE_API_KEY": "<your-key>"
      }
    }
  }
}
```

</details>

## Authentication

Create a Personal Access Token at airtable.com/create/tokens with the scopes you need (for example data.records:read, data.records:write, schema.bases:read) and the specific bases or workspaces it should reach. Set it as AIRTABLE_API_KEY, or store it with the auth commands. This CLI authenticates with personal access tokens only.

## Quick Start

```bash
# Preview the health check offline; drop --dry-run to verify auth and connectivity
airtable-pp-cli doctor --dry-run

# Find the base IDs your token can access
airtable-pp-cli bases list --json

# Mirror one table's records into the local store to power search/analytics/expand-links
airtable-pp-cli records sync-table --base appLkNDICXNqxSDhG --table Shipments

# Query live records with a formula filter, the core Airtable workflow
airtable-pp-cli records list --base appLkNDICXNqxSDhG --table Shipments --filter-formula "{Status}='Delayed'"

# Bulk-sync a CSV export straight into Airtable with merge-field upsert semantics
airtable-pp-cli records upsert-csv --base appLkNDICXNqxSDhG --table Leads --file leads.csv --merge-on Email

```

## Unique Features

### Bulk data sync
- **`records upsert-csv`** — Take a raw CSV export and merge-upsert it straight into an Airtable table, matched on the field you choose.

  _Reach for this whenever the input is a CSV export from another system and existing rows should be updated instead of duplicated._

  ```bash
  airtable-pp-cli records upsert-csv --base appLkNDICXNqxSDhG --table Leads --file leads.csv --merge-on Email
  ```
- **`records diff`** — See exactly which fields on which existing records would change before a bulk update or upsert actually runs.

  _Use before any agent-authored bulk update to confirm what will actually change, not just how many rows will be touched._

  ```bash
  airtable-pp-cli records diff --base appLkNDICXNqxSDhG --table Shipments --stdin < changes.json
  ```

### Local state that compounds
- **`search`** — Search text across every synced base and table at once from one local index.

  _Reach for this instead of iterating records list per table when the query might match any base or table already synced locally._

  ```bash
  airtable-pp-cli search "union square" --type records --agent --limit 10
  ```
- **`bases schema-diff`** — Show exactly what changed in a base's schema since the last time this command ran for that base.

  _Run before deploying pipeline changes that depend on a base's field names or types staying stable._

  ```bash
  airtable-pp-cli bases schema-diff appLkNDICXNqxSDhG
  ```

## Recipes

### Find delayed shipments and summarize by carrier

```bash
airtable-pp-cli records list --base appLkNDICXNqxSDhG --table Shipments --filter-formula "{Status}='Delayed'" --expand-links --agent --select fields.Carrier,fields.Status,fields.Address
```

Pulls only delayed shipments, resolves linked carrier IDs to names from the local mirror (run records sync-table on the linked Carriers table first), and trims the response to the fields a report needs.

### Bulk-sync a CRM export into Airtable

```bash
airtable-pp-cli records upsert-csv --base appLkNDICXNqxSDhG --table Leads --file leads.csv --merge-on Email --dry-run
```

Preview a merge-upsert from a CSV export before committing the write, matched on the Email field.

### Search every synced base for a customer name

```bash
airtable-pp-cli search "Acme Corp" --type records --json
```

One query across every base/table already synced locally, instead of repeating records list per table.

### Check what changed in a base's schema

```bash
airtable-pp-cli bases schema-diff appLkNDICXNqxSDhG
```

Flags added, removed, or retyped fields since the last time schema-diff ran for this base.

## Usage

Run `airtable-pp-cli --help` for the full command reference and flag list.

## Paths & environment variables

This CLI separates local files into four path kinds:

| Kind | Contents |
|------|----------|
| `config` | User-editable settings such as `config.toml` and saved profiles |
| `data` | Durable local data: `credentials.toml`, `data.db`, cookies, browser-session proof files, and other auth sidecars |
| `state` | Runtime state such as persisted queries, jobs, and `teach.log` |
| `cache` | Regenerable HTTP/cache files |

Each kind resolves independently. The ladder is:

1. Per-kind env var: `AIRTABLE_CONFIG_DIR`, `AIRTABLE_DATA_DIR`, `AIRTABLE_STATE_DIR`, or `AIRTABLE_CACHE_DIR`
2. `--home <dir>` for this invocation
3. `AIRTABLE_HOME` for a flat relocated root
4. XDG env vars: `XDG_CONFIG_HOME`, `XDG_DATA_HOME`, `XDG_STATE_HOME`, `XDG_CACHE_HOME`
5. Platform defaults matching existing installs

For containers and agent sandboxes, prefer a single relocated root:

```bash
export AIRTABLE_HOME=/srv/airtable
airtable-pp-cli doctor
```

Under `AIRTABLE_HOME=/srv/airtable`, the four dirs resolve to `/srv/airtable/config`, `/srv/airtable/data`, `/srv/airtable/state`, and `/srv/airtable/cache`.

On Windows, set the same variable in PowerShell:

```powershell
$env:AIRTABLE_HOME = "C:\srv\airtable"
airtable-pp-cli doctor
```

Under `$env:AIRTABLE_HOME = "C:\srv\airtable"`, the four dirs resolve to `C:\srv\airtable\config`, `C:\srv\airtable\data`, `C:\srv\airtable\state`, and `C:\srv\airtable\cache`.

MCP servers do not receive CLI flags from the host. Put relocation in the host `env` block:

```json
{
  "mcpServers": {
    "airtable": {
      "command": "airtable-pp-mcp",
      "env": {
        "AIRTABLE_HOME": "/srv/airtable"
      }
    }
  }
}
```

Precedence matters in fleets: an ambient per-kind variable such as `AIRTABLE_DATA_DIR` overrides an explicit `--home` for that kind. Use `AIRTABLE_HOME` or the per-kind variables for durable fleet relocation; treat `--home` as the weaker per-invocation lever.

Relocation is one-way. Unsetting `AIRTABLE_HOME` does not move files back to platform defaults, and `doctor` cannot find credentials left under a former root. Move the files manually before unsetting relocation variables.

Existing installs keep working because the platform-default rung matches the legacy layout. On the first auth write, stored secrets leave `config.toml` and are consolidated into `credentials.toml` under the data directory. Run `airtable-pp-cli doctor --fail-on warn` to check path and credential-location warnings in automation.

## Commands

### attachments

Direct attachment upload to a cell

- **`airtable-pp-cli attachments <field_id>`** - Upload a file (up to 5MB) directly to an attachment cell

### bases

Base-level metadata: list bases, get schema, collaborators, create bases

- **`airtable-pp-cli bases create`** - Create a base with tables and return its schema
- **`airtable-pp-cli bases get-collaborators`** - Get basic base info: name, permission level, and outstanding invites
- **`airtable-pp-cli bases get-schema`** - Return the schema (tables, fields, views) for a base
- **`airtable-pp-cli bases list`** - List bases the token can access

### comments

Threaded comments on records

- **`airtable-pp-cli comments create`** - Create a comment on a record
- **`airtable-pp-cli comments delete`** - Delete a comment you created (Enterprise Admins can delete any)
- **`airtable-pp-cli comments list`** - List comments on a record, newest first
- **`airtable-pp-cli comments update`** - Update a comment you created

### fields

Field (column) schema management within a table

- **`airtable-pp-cli fields create`** - Create a field in a table
- **`airtable-pp-cli fields update`** - Update a field's name, description, or options

### records

Record CRUD in a table (records have no fixed schema — fields are user-defined per base)

- **`airtable-pp-cli records create`** - Create one or more records
- **`airtable-pp-cli records delete`** - Delete a single record
- **`airtable-pp-cli records delete-multiple`** - Delete multiple records by ID
- **`airtable-pp-cli records get`** - Retrieve a single record
- **`airtable-pp-cli records list`** - List records in a table, with formula filtering, sorting, view scoping, and pagination
- **`airtable-pp-cli records replace`** - Destructively replace a single record; unspecified cell values are cleared
- **`airtable-pp-cli records replace-multiple`** - Destructively replace multiple records in one request (or upsert, clearing unspecified fields)
- **`airtable-pp-cli records update`** - Update a single record; only the fields you specify are changed (partial update)
- **`airtable-pp-cli records update-multiple`** - Update or upsert multiple records in one request (partial update per record)

### tables

Table schema management within a base

- **`airtable-pp-cli tables create`** - Create a table in a base
- **`airtable-pp-cli tables update`** - Update a table's name, description, or date-dependency settings

### user

Authenticated user identity and token scopes

- **`airtable-pp-cli user`** - Get the authenticated user's ID, and token scopes/email if the token has those scopes

### views

View metadata within a base

- **`airtable-pp-cli views delete`** - Delete a view
- **`airtable-pp-cli views get`** - Get metadata for a view
- **`airtable-pp-cli views list`** - List views in a base

### webhooks

Webhook subscriptions for base change notifications

- **`airtable-pp-cli webhooks create`** - Create a webhook on a base (max 10 per base)
- **`airtable-pp-cli webhooks delete`** - Delete a webhook
- **`airtable-pp-cli webhooks list`** - List webhooks registered on a base
- **`airtable-pp-cli webhooks list-payloads`** - Retrieve queued change payloads for a webhook (call after receiving a ping)
- **`airtable-pp-cli webhooks refresh`** - Extend a webhook's expiration by 7 days
- **`airtable-pp-cli webhooks set-notifications`** - Enable or disable notification pings for a webhook


### bases


- **`airtable-pp-cli bases schema-diff`** - Show exactly what changed in a base's schema since the last time this command ran for that base.


### records


- **`airtable-pp-cli records upsert-csv`** - Take a raw CSV export and merge-upsert it straight into an Airtable table, matched on the field you choose.

- **`airtable-pp-cli records diff`** - See exactly which fields on which existing records would change before a bulk update or upsert actually runs.




### Self-learning loop

This CLI caches per-question discovery so repeat queries skip the walk and structurally similar queries get answered via entity substitution. The loop also self-captures: every invocation is journaled locally, and failed-flag corrections plus fresh teaches surface as candidates on the next `recall` for confirm/reject judgment. Agents call `recall` before discovery and fire `teach &` after answering. See the `## Automatic learning` section in `SKILL.md` for the full protocol.

- **`airtable-pp-cli recall <query>`** - Look up cached resources for a query before running discovery
- **`airtable-pp-cli teach`** - Record a query -> resource mapping (silent on success, safe to background with `&`)
- **`airtable-pp-cli learnings list`** - Inspect taught rows
- **`airtable-pp-cli learnings forget <query>`** - Undo a teach
- **`airtable-pp-cli learnings candidates`** - List auto-captured candidates awaiting confirm/reject
- **`airtable-pp-cli learnings stats`** - Local loop metrics: recall hit rate, teach-to-reuse, playbook resolution, candidate counts
- **`airtable-pp-cli teach-pattern`** - Install a query/resource template up front
- **`airtable-pp-cli teach-lookup`** - Add an entity mapping (e.g. country code, team alias) for pattern substitution

Pass `--no-learn` or set `AIRTABLE_NO_LEARN=true` to disable the loop for deterministic flows.

The local store's schema version stamp is one-way: once this version of `airtable-pp-cli` opens the database, older binaries refuse it with a version error — upgrade the binary rather than downgrading.

## Output Formats

```bash
# Human-readable table (default in terminal, JSON when piped)
airtable-pp-cli bases list

# JSON for scripting and agents
airtable-pp-cli bases list --json
# Filter to specific fields by name
airtable-pp-cli bases list --json --select <field>[,<field>...]

# Dry run — show the request without sending
airtable-pp-cli bases list --dry-run

# Agent mode — JSON + compact + no prompts in one flag
airtable-pp-cli bases list --agent
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

## Freshness

This CLI owns bounded freshness for registered store-backed read command paths. In `--data-source auto` mode, covered commands check the local SQLite store before serving results; stale or missing resources trigger a bounded refresh, and refresh failures fall back to the existing local data with a warning. `--data-source local` never refreshes, and `--data-source live` reads the API without mutating the local store.

Set `AIRTABLE_NO_AUTO_REFRESH=1` to disable the pre-read freshness hook while preserving the selected data source.

Covered command paths:
- `airtable-pp-cli bases`
- `airtable-pp-cli bases get`
- `airtable-pp-cli bases list`
- `airtable-pp-cli bases search`

JSON outputs that use the generated provenance envelope include freshness metadata at `meta.freshness`. This metadata describes the freshness decision for the covered command path; it does not claim full historical backfill or API-specific enrichment.

## Health Check

```bash
airtable-pp-cli doctor
```

Verifies configuration, credentials, and connectivity to the API.

## Configuration

Run `airtable-pp-cli doctor` to see the resolved config, data, state, and cache directories. The platform-default config path is `~/.config/airtable-pp-cli/config.toml` on macOS/Linux and `%USERPROFILE%\.config\airtable-pp-cli\config.toml` on Windows; `--home`, `AIRTABLE_HOME`, and per-kind env vars can relocate it.

Static request headers can be configured under `headers`; per-command header overrides take precedence.

Environment variables:

| Name | Kind | Required | Description |
| --- | --- | --- | --- |
| `AIRTABLE_API_KEY` | per_call | Yes | Set to your API credential. |

### agentcookie (optional)

If you use agentcookie to sync secrets across machines, this CLI auto-adopts agentcookie-managed credentials with no extra setup. When the daemon writes to this CLI's config, `airtable-pp-cli doctor` reports `agentcookie: detected` and `auth-status` labels the source as `agentcookie`. Skip this section if you don't use agentcookie - the CLI works the same as any other.

## Troubleshooting
**Authentication errors (exit code 4)**
- Run `airtable-pp-cli doctor` to check credentials
- Verify the environment variable is set: `echo $AIRTABLE_API_KEY`
**Not found errors (exit code 3)**
- Check the resource ID is correct
- Run the `list` command to see available items

### API-specific
- **429 Too Many Requests** — Airtable enforces 5 req/s per base and 50 req/s per token; wait 30s before retrying, the client backs off automatically but persistent 429s mean too much concurrent traffic against one base.
- **403 INVALID_PERMISSIONS_OR_MODEL_NOT_FOUND** — Both the user AND the token need access: check that the token has the base added as a resource and the required scope (e.g. data.records:write), and that your user account is a collaborator on the base.
- **bases list returns an empty array despite having access** — On Enterprise plans this usually means an org-level 'Block API access to organization-owned apps & workspaces' setting is restricting the token; check Enterprise/org-unit Integrations settings.
- **records upsert-csv reports unmapped columns** — CSV header names must exactly match Airtable field names (or field IDs); rerun bases get-schema to see the exact field names for the target table.
- **search / analytics / --expand-links return nothing for a table** — Those commands only see data synced by 'records sync-table --base <id> --table <name>'; run it for that table first.

## Sources & Inspiration

This CLI was built by studying these projects and resources:

- [**airtable-mcp-cli**](https://github.com/Airtable/airtable-mcp-cli) — TypeScript
- [**airtable-mcp-server**](https://github.com/domdomegg/airtable-mcp-server) — TypeScript
- [**airtable.js**](https://github.com/Airtable/airtable.js) — JavaScript
- [**pyairtable**](https://github.com/gtalarico/pyairtable) — Python
- [**airrecord**](https://github.com/sirupsen/airrecord) — Ruby
- [**airtable.net**](https://github.com/ngocnicholas/airtable.net) — C#
- [**rashidazarang/airtable-mcp**](https://github.com/rashidazarang/airtable-mcp) — TypeScript
- [**arnoldadlv/airtable-cli**](https://github.com/arnoldadlv/airtable-cli) — TypeScript

Generated by [CLI Printing Press](https://github.com/mvanhorn/cli-printing-press)
