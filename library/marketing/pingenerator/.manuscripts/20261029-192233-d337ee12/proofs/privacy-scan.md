# Privacy and secret scan: final (source 5ae04c7)

Scope: everything under `skills/pp-pingenerator/printing-press-reprint/`, which covers the CLI source tree, the manuscripts, and the README, RECONCILIATION and RETRO docs.

## Redactions applied to manuscripts (not to source)

| Pattern | Replaced with |
|---|---|
| container run paths (`<runstate><scope>/runs/<run>[/working/pingenerator-pp-cli]`) | `<run-dir>` / `<cli-dir>` |
| `/root/printing-press`, `/root/printing-press/manuscripts` | `<press-home>` / `<press-manuscripts>` |
| session scratch paths, repo checkout paths, runstate scope id | `<scratch>`, `<repo>`, `<scope>` |
| the author's private repository and organisation name | "the author's private skills repository" |
| path of the prior hand-built CLI | "the prior CLI source" |
| operator role naming in research artifacts | "the author" |

Files touched:
- `research/*brief.md` and `research/*absorb-manifest.md`;
- `research.json` (one entry in `gaps`);
- `proofs/shipcheck-run1/2/3/final.txt`, `proofs/dogfood-phase3.json`, `proofs/publish-validate.json`, `proofs/publish-package.json`;
- `proofs/phase-13-19-reviews.md`.

The publish-* captures were also split into valid JSON plus a `.stderr.txt` file (the CLI's own "validation failed" line).

The CLI source tree was **not** modified. It is byte-identical to `5ae04c7` and contains none of these patterns.

## Scan results after redaction

| Check | Result |
|---|---|
| Credential patterns (AWS, GitHub PAT, private key blocks, JWT, Stripe-style keys, Slack tokens, long Bearer values) | none |
| `PINGENERATOR_ACCESS_KEY` set to a real-looking value | none (only `<placeholder>` text and the mock tests' fake `test-key-not-real`) |
| Email addresses | synthetic fixtures only: `@example.com`, `@example.co.uk`, `<synthetic-email>`, and `<synthetic-url-userinfo>` in a URL-parsing test |
| Local paths | none in manuscripts or docs; generated test fixtures use synthetic `<cli-dir>/.config/example-pp-cli/...` and `<cli-dir>/...` |
| Session URLs | none in files (commit messages carry the required session attribution trailer) |
| Private repo or org names | none in manuscripts or CLI source |
| `cli-printing-press pii-audit <cli> --manuscripts-dir <manuscripts>` | no findings |
| `cli-printing-press tools-audit <cli>` | no findings |

## Attribution kept on purpose

- `.printing-press.json` `creator`: `{"handle":"ElizabethcStone","name":"Elizabeth Stone"}`, with printer and owner matching.
- SKILL `author: "Elizabeth Stone"`.
- NOTICE: "Created by Elizabeth Stone (@ElizabethcStone)".
- Source headers: "Copyright 2026 Elizabeth Stone and contributors".

No live account values (credit counts, emails, profile names, board IDs, URLs) appear anywhere. The live field names reported by the author are used only as synthetic test fixtures.

## Final re-scan (source 6ec66eb, after the disposable-store live checks)

| Check | Result |
|---|---|
| URL hosts | Only synthetic (`example.*`, `*.test`, `evil.*`), loopback, the documented vendor endpoint, and public tooling hosts. No disposable-store, admin or login URLs |
| Credential patterns, including WooCommerce `ck_`/`cs_` keys | none |
| `PINGENERATOR_TEST_*` / `PINGENERATOR_ACCESS_KEY` values | none (only variable names and `...` placeholders) |
| Emails | synthetic test fixtures only |
| Account or site names, profile or store IDs | none. Live results are recorded as counts and field names only |
| `pii-audit` (CLI and manuscripts) | no findings |
| Frozen Go source | unchanged since `6ec66eb`; `go test ./...` 15/15 |

`RECONCILIATION.md` (an internal handoff doc in this private repo, not part of the publish package) names the prior module path so the `install.sh` caveat stays accurate.

Public attribution is limited to Elizabeth Stone / ElizabethcStone.
