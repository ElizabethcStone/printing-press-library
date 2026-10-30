# Phases 13–19: what actually ran (review fixes through 5ae04c7; final source 6ec66eb)

Phase 12 shipcheck recorded a hold, so the receipt ledger jumped 12 → 20 → 21. After that, the review phases ran in the cloud session against the working tree. They are recorded here, not as ledger receipts. Live proof (phase 18) belongs to the author.

| Phase | Status | Detail |
|---|---|---|
| 13 sync-param-drop gate | Not applicable | No `traffic-analysis.json` (no browser sniff), and no `internal/syncer` or transcendence sync package |
| 14 agentic SKILL review | Ran: independent reviewer | 2 errors, 13 warnings. Both errors fixed in `814e006`. Narrative warnings fixed at the source (`research.json`). Template boilerplate warnings went to the retro |
| 15 README/SKILL/AGENTS correctness audit | Ran, combined with 14 | Same reviewer and findings as 14 |
| 16 agentic output review | Ran inside polish (phase 19), offline only | Sampled `preflight` and `ledger` against a dead local base URL. One WARN, deferred (see below). No live output was reviewed |
| 17 local code review | Ran: independent correctness/security reviewer | 7 in-scope findings, all fixed: #6 in `814e006`, the rest in `5ae04c7`. Two out-of-scope generated-code findings went to the retro |
| 18 live dogfood | Author, locally | Not run in the cloud: no credentials or vendor network here, by design |
| 19 polish | Ran: `printing-press-polish` skill, network-fenced | Scorecard 81 → 83 (87 with `--spec`). Verify 100%, gosec 5 → 0 on the hand-written code, tools-audit 0, recommendation `ship` *conditional on the live gate*. Its Go edits are **not** in the frozen source; see `phase19-polish-proposed-go.patch` |

## Phase 14/15 errors (fixed)

1. `create-automation` ran immediately and could start server-side credit spend (`--first-batch now`). It now previews until `--execute`, like the other spending tools.
2. The generated `mcp` examples omitted the required `--method`. The spec now defaults `method` to `tools/list`.

Narrative warnings fixed in `research.json`:
- how to recognize a preview (`dry_run` / `executed`);
- that unconfirmed ledger rows exist;
- that skips are based on the ledger, not the account queue;
- the `unpinned-posts --select unpinned.*` recipe;
- the rate-limit wording (prior observation, unverified);
- which response shapes were verified live.

Generator-template warnings, sent to the retro:
- the `sync` command reference;
- `auth-status`;
- the nonexistent `list` command;
- the `import` command;
- the "not available in any other tool" claim;
- the Command Reference not listing the typed tools.

## Phase 17 findings (all fixed)

1. The passthrough accepted case-variant `name` keys (a spend bypass). Params are now decoded as a map and duplicates rejected.
2. Ambiguous-spend detection was too broad. It now covers only deadlines, cancellation, post-connect transport failures and 5xx. A 429, dial/DNS failures and refusals are not spends.
3. `pin-products --execute` exited 0 after failures and could double-pin. It now:
   - returns the first failure's exit code after printing the report;
   - stops on 4, 7 or no credits;
   - dedupes within a run;
   - stops on a repeated page.
4. `pin-products` re-pinned products that had no link. Linkless products are now skipped.
5. URL normalization dropped the whole query string, so `?p=` posts collided. Identifying query params are now kept; tracking params are still stripped.
6. `create-automation` could spend without a guard (fixed in `814e006`).
7. Low-severity fixes:
   - `queue-gaps --board` matches any board field;
   - `preflight` returns auth errors as exit 4, and fails per-pin plans when the balance is zero;
   - the ledger has no line-length cap;
   - image-only spends no longer mark a post as pinned;
   - AI detection matches whole tokens only;
   - `pgVarName`/`pgCtorName` are fed only by the fixed catalog, so no change was made there.

Initially classed as generated-code (retro) findings, then **fixed in this CLI** because both are credit-safety defects:
- `profile.go` let a saved run profile set `execute=true` (or `allow-unknown-tool=true`). Both are now reserved from profile apply and save.
- A static `Idempotency-Key` config header made credit-spending POSTs retryable, overriding an explicit replay-safe `false`. Now an explicit opt-out always wins, only a per-request key makes a POST/PATCH replayable, and `pgmcp` marks every paid or unknown `tools/call` replay-unsafe.

Both are recorded under `.printing-press-patches/` and remain retro items for the generator templates.

## Live-gate support fixes (`5ae04c7`)

- `local-data purge --dry-run --json` now includes `"action"`. This was a generator-template omission; the regression test is `TestLocalDataPurgeDryRunEnvelope`.
- Vendor not-found errors now exit 3 instead of 5. Examples: "Batch not found", "Could not read URL page (404)".
- Account-specific required inputs are declared as `example-value` fixtures:
  - `list-boards --pinterest-profile`
  - `get-post-content --post-url`
  - `list-store-products --store-id`
  - `get-automation-batch-status --batch-id`

  The live gate reports these as `blocked-fixture: required API parameter`. They are not passes, and no IDs were invented.

## Phase 16/19 deferred

- **Output-review WARN:** `preflight --agent` reports `meta.source: "live"` even when every read failed. The failures already show in `fetch_failures`, as "unknown" checks, as verdict "check" and as a stderr warning. Changing the label would mean changing the generated envelope's vocabulary.
- **gosec:** 45 findings in generated files are generator retro candidates.

## Tests at 5ae04c7 (cloud, offline)

- `go test -count=1 ./...`: 15 of 15 packages pass. The PinGenerator tests use an `httptest` MCP mock with synthetic values and the live field spellings the author reported.
- `go vet ./...`: clean.
- `verify-skill`: all checks pass. `validate-narrative --strict --full-examples`: 9 of 9 pass.
- Cloud shipcheck (`814e006`): 6 of 7 legs passed. The one failure was the dogfood placeholder-host DNS check, caused by the sandbox resolver. It passes locally.

## Local checks reported by the author (not run in the cloud)

- Generator v4.34.2 offline dogfood: PASS, with normal DNS.
- `govulncheck ./...`: exit 0, no reachable vulnerabilities.
- Authenticated read-only `get-account-status`, `list-pinterest-profiles`, `list-scheduled-pins`, `list-automations` and `list-boards` (with a real profile): work.
- First full live gate (301 tests on `e69f49f`): FAILED. The causes were placeholder inputs and the purge dry-run `action`, not auth.
- Fresh live harness on `5ae04c7`: 301 tests PASS.
- `publish validate` rejects hollow novel coverage for `pin-post`, `pin-products` and `unpinned-posts`, whose happy paths are declared fixture skips. Every other check passes, including provenance, govulncheck, vet, build, `--help`, `--version` and the skill check.
- `list-connected-stores` works but returns `stores: []`; the account has no connected store, so `pin-products` cannot run live. None was created.
- No `--skip-live-test` waiver is authorized. No live acceptance marker or skip marker has been written. No PR has been created.

## Test-only live fixtures (after 5ae04c7)

The author refused a live-test skip and provisioned a disposable test store. To exercise the novel previews in a source-bound live gate without committing account data, env-backed happy-args were added. They are active only under `PRINTING_PRESS_DOGFOOD=1` with every needed `PINGENERATOR_TEST_*` variable set, and they use a sentinel so recorded args never hold the values. No fixture adds a confirm flag. Patch records for the generated-code customizations were added under `.printing-press-patches/`: the purge dry-run action, and the passthrough credit guard. `go test ./...`: 15/15 packages; verify-skill and validate-narrative pass.

## Final state (source 6ec66eb, frozen)

- The author connected a user-authorized disposable WooCommerce test store containing one synthetic product. `list-store-products` passed live (1 product). The response shape is `store`, `page`, `products[]` and `hasMore`, with product fields `id`, `title`, `description`, `price`, `url` and `image`.
- `pin-products` preview passed live (planned 1, executed 0). `pin-post` passed live (`executed:false`). `unpinned-posts` passed live (exit 0). Nothing was spent.
- The final source-bound live gate is running locally on `6ec66eb` with the env fixtures. The author records the verdict. No skip waiver is used.
- Public evidence carries only synthetic fixtures and counts. No account names, emails, IDs, store URLs, login URLs or credentials appear in this repository.

## Credit-safety closure (final source)

Publication review raised the two findings above as in-scope blockers, and both are now fixed with regression tests that fail on the original code:
- **Replay:** before the fix, a paid call with a static config key reached the server 4 times after a 503. Now it reaches it exactly once. The tests are `TestReplayOptOutAndStaticKeyNeverResendPaidCalls`, `TestPaidCallNotResentWithStaticIdempotencyKey` and `TestMCPCallsMarkPaidAndUnknownToolsReplayUnsafe`. Read GETs and per-request-keyed POSTs still retry (`TestGenuineRetriesStillWork`).
- **Profiles:** before the fix, a saved profile armed a spend (1 paid call without `--execute`). Now it makes zero paid calls, and an explicit `--execute` still spends once. The tests are `TestSavedProfileCannotArmSpend` and `TestProfileApplyNeverSuppliesSpendConfirmations`.

`go test ./...`: 15/15 packages. verify-skill and validate-narrative both pass. **No unresolved in-scope review findings remain.** The deferred items are non-safety: the generator-template boilerplate in the docs, and the `meta.source` label.

Local live gate on the previous source `6ec66eb`: PASS. 301 tests, of which 162 passed and 139 were skipped or unverified, with no hollow coverage, using a real synthetic store and no execution. It will be rerun against the final source.
