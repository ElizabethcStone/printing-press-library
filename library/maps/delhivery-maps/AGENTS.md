# Delhivery Maps Printed CLI Agent Guide

This directory is a generated `delhivery-maps-pp-cli` printed CLI. It was produced by [CLI Printing Press](https://github.com/mvanhorn/cli-printing-press), so treat systemic fixes as upstream Printing Press fixes first. Keep local edits narrow and document why a generated-tree patch belongs here.

## Local Operating Contract

Delhivery Maps is an India-only location API. This CLI is **read/compute only**: nothing it does creates, updates or deletes remote data. There is no sync and no list endpoint; the API is stateless. The gateway allows about **50 requests per 24 hours per token**, and `route-plan` (used by `fleet plan`) has its own separate limit of about 5 per 24 hours, so every live call spends real quota. The batch commands (`address clean`, `coverage check`, `trip quote`, `fleet plan`) keep a local budget guard: they count their own calls, lock out for 24 hours after a 429, and mark work they cannot afford as `deferred` (exit 0, with `rate_limited` in the JSON). `--max-calls` (on all four) can only lower the budget; `--max-calls 0` means no calls. The ledger and lockout are per token and base URL, and a 429 on one pool does not lock out the other. Batch commands exit 5 when every item failed for a non-quota reason. The local counter cannot see other consumers of the same token, and the 24-hour lockout is a local policy. The batch commands also take rows from `--stdin` (no files needed; `--out` is optional and rows then come back in the JSON). Per-command costs are in `README.md` ("Quota and call costs").

Start with checks that cost no quota:

```bash
delhivery-maps-pp-cli --version
delhivery-maps-pp-cli auth status
delhivery-maps-pp-cli agent-context --pretty
```

`doctor` makes a live request to the API and may spend quota; `doctor --dry-run` only prints "would run doctor" and verifies nothing. The token is usually a 24-hour web-session JWT and is not refreshed by the CLI.

Use runtime discovery instead of relying on a copied command list:

```bash
delhivery-maps-pp-cli which "toll cost for a truck lane" --json
delhivery-maps-pp-cli trip quote --help
```

Add `--agent` to command invocations for JSON, compact output, non-interactive defaults, and no color:

```bash
delhivery-maps-pp-cli geocode --address "flat 4b, lotus apt, sec 45, gurgaon, haryana 122003" --agent
```

Preview a request with `--dry-run` before spending quota:

```bash
delhivery-maps-pp-cli trip quote --from "Sector 45 Gurgaon" --to "Jaipur" --dry-run --agent
```

Pass `--yes` only where a command asks for it. The one command that deletes anything is `address purge`, which removes rows from the local SQLite database (raw addresses, coordinates, cached responses) and never calls the API: `address purge --older-than 30d`, or `address purge --all --yes` (`--all` and `--older-than 0` need `--yes`). `--agent` does not imply `--yes`.

## Novel Command Data Sources

Every hand-written novel command declares its strategy in a Go line comment, for example:

```go
// pp:data-source live
```

Use exactly one of `auto`, `local`, `live`, or `computed`, and keep `cmd.Annotations["pp:data-source"]` on the same command in sync (`--agent` envelopes read it for `meta.source`). In this CLI: `address clean`, `coverage check`, `fleet plan` and `trip quote` are `live` (they call the API, with a local response cache); `address report`, `address dupes` and `address purge` are `local` (they read or edit only the local database). There is no synced collection, so do not add `auto` commands that expect one.

## Self-Learning Loop

This CLI ships a self-capturing teach/recall loop backed by the local SQLite store. The CLI journals every invocation, derives `flag_alias` candidates from failed-flag + corrected-retry pairs, and synthesizes a playbook candidate when a family is taught without one - no manual failure bookkeeping. The agent's role is judgment:

1. On a new user question, call `delhivery-maps-pp-cli recall` FIRST, passing the question as an argv or MCP argument - never by interpolating it into a shell command, quoted string, or heredoc (apostrophes break quotes; a body line equal to the delimiter breaks a heredoc). If a shell is required, write the question with a non-shell file tool, then `QUERY=$(cat /path/to/question.txt)` and `delhivery-maps-pp-cli recall "$QUERY" --agent`. If `found=true` and the top result has `entity_match == "exact"` and `confidence >= 2`, use the returned command or notes instead of rediscovering them. If the store is cold (recall finds nothing and `learnings list` and `learnings candidates` are both empty), skip recall for the rest of the session.
2. When the envelope carries a `candidates` section (warning `candidates_present`), candidates are try-then-confirm, never facts: follow each candidate's two-step `next_action` verbatim (trial command first, then `learnings confirm <id>` only after the trial verified the behavior), and reject wrong ones with `learnings reject <id>`. Never re-teach something recall surfaced as a candidate; confirm or reject it instead.
3. After answering, always fire `delhivery-maps-pp-cli teach` in the background with the same argv/MCP or file-then-`$QUERY` passing rule as recall (`QUERY=$(cat /path/to/question.txt)` then `delhivery-maps-pp-cli teach --query "$QUERY" --resource-type command --resource "trip quote" &`) - teaching is unconditional and is the anchor that triggers playbook synthesis. Teach the structural question with identifiers stripped (no names, addresses, emails, phone numbers, account ids); the CLI warns on obvious PII shapes but does not block.
4. Use `learnings list` to inspect taught rows, `learnings forget "$QUERY" --all` (same file-then-`$QUERY` rule) to undo a bad teach, `learnings candidates` for the full open candidate set, and `learnings stats` for the loop's local metrics. `teach-pattern` and `teach-lookup` install manual generalization rules when one teach should cover a whole family (e.g. one city alias covering every query about that city).
5. If `learnings confirm` is an unknown command, you are driving an older binary - ignore the candidates guidance and keep the rest of the flow.

Annotations: `recall`, `learnings list`, `learnings candidates`, and `learnings stats` carry `mcp:read-only=true`; `teach`, `teach-playbook`, `playbook amend`, `learnings confirm`, `teach-pattern`, and `teach-lookup` carry `mcp:local-write=true` (writes land only in the CLI's own local store); `learnings forget` and `learnings reject` keep honest may-write/destructive defaults.

### Success definition

Measurement is local-only: the `learn_events` table and `learnings stats`; nothing leaves this machine. Judge the loop on recall hit rate and teach-to-reuse at a minimum denominator of 50+ recall events. Near-zero rates at that denominator mean the loop is not earning its keep for this CLI - surface that in retros. An empty or thin events table means insufficient adoption, not failure.

The store's schema stamp is one-way: once this binary opens the database, an older binary refuses it (README.md carries the upgrade note).

Disable the loop with `--no-learn` per-invocation or `DELHIVERY_MAPS_NO_LEARN=true` for the whole session - useful for deterministic agent flows that don't want a learning row to silently change subsequent query results.

## Platform Credential References

Normal API authentication is separate from optional platform-source credential
resolution. If this CLI uses indirect references for a tenant-gated platform
source, add the downstream registration in a preserved hand-authored file
under `internal/cli/` and provide both `CredentialResolverFactory` and
`ValidateSourceProfile` on `platformSourceRegistration` for any selected source
that has references. A source with no references may omit both hooks and receives
an empty credential map. Keep reference values opaque to shared profile code,
validate only the selected source in the downstream hook, and never persist
resolved credential bytes. Do not edit generator-owned `internal/platform`
packages; a reprint refreshes those files while retaining the downstream
registration file.

For install, auth, examples, and longer product guidance, read `README.md` and `SKILL.md`. This file intentionally stays small so repo-local agents get invariant local guidance without duplicating the generated docs.

## Release Ledger

`CHANGELOG.md` and `.printing-press-release.json` are the public library's per-CLI release ledger. Fresh prints carry an unstamped runtime version such as `0.0.0-dev`; the final `YYYY.M.N` CLI release version is assigned only after a publish PR merges in `mvanhorn/printing-press-library`. Do not hand-bump those files or edit `var version = ...` for release bookkeeping; preserve existing ledger files on reprint and let the library workflow stamp the next release.

## Local Customizations

This directory is **generated output** -- a fresh print can overwrite the whole tree, so ad-hoc hand-edits don't survive on their own. If you modify the generated code, record each change under `.printing-press-patches/` (parallel to `.printing-press.json`). Regen and publish-validate read those records and fail closed when a recorded file or call site is gone, so a dropped customization cannot ship as if it were still applied.

The entry shape, and the altitude to write it at -- a durable reprint-guard, not a changelog -- live in the public library's `AGENTS.md`, which is the single source of truth; this guide intentionally doesn't duplicate them.
