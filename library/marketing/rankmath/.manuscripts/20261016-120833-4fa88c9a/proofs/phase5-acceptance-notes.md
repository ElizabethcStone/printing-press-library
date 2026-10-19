# Phase 5 live acceptance notes

Environment: throwaway local WordPress 6.8.3 in Docker (`wordpress:6.8-php8.3-apache` + MariaDB 11.4) at `http://localhost:8889`, Rank Math 1.0.280 + Rank Math Pro 3.0.124, a dedicated admin user with an application password. All content is synthetic (`PRIVATE TEST ...`). No production site was touched.

## Live dogfood (`phase5-acceptance.json`)

`cli-printing-press dogfood --live --level full`: **pass**, 337 passed, 409 skipped, 6 unverified. The fingerprint matches the tree in this commit.

Skips are matrix-policy skips, not failures:

| Count | Reason |
|---|---|
| 100 | no positional argument |
| 68 | mutating command requires `--allow-destructive` |
| 53 | command does not honour `--dry-run` |
| 46 | mutating command, dry-run only |
| 44 + 6 + 5 | no list companion / parseable id for the positional |
| 21 | mutating command, error path would hit the live API |
| 12 | unsynthesizable body |
| 23 | non-id positional (`name`, `query`, `text`) |
| 8 | blocked fixture: required API parameter |
| 5 | destructive at auth |
| 4 | declared non-zero exit 3 (typed not-found) |

## Write round-trips (`phase5-live-roundtrip.sh` / `.txt`)

These cover the mutating surface the matrix skips. Each write is read back through the CLI and checked independently with `wp-cli` or `curl`:

- **Meta:** `meta update` sets `rank_math_title`/`rank_math_description`. `wp post meta get` returns the new title, and `head --url` renders it in `og:title` and the meta description.
- **Schema:** `schema update` adds an Article schema. `wp post meta list` shows `rank_math_schema_Article`, and `head` emits the JSON-LD headline.
- **Settings:** `settings save-module --module redirections` turns the module off and then on.
- **Redirection:** create a 301, then `curl` returns `301 -> /private-test-page/`. Remove it, then `curl` returns `200`.
- **Link Genius (Pro):** keyword map create, list, get, add variation, get variations, preview, preview results, delete. After the delete, `get` returns typed exit 3.
- **Reindex:** the `reindex --since 7d` preview, `indexing get-log`, `coverage` and `opportunities` all run authenticated and succeed.

## Not wireable locally

- **Search Console / Analytics-backed data:** the site has no connected Google account. `coverage` therefore reports `inspected: 0` with every URL `never_inspected`. `opportunities` reports `page_lookup: unavailable` with no keyword rows. The `gsc get-*-pagespeed` and `get-page-seoscore` commands have no analytics object IDs. The commands execute and degrade correctly, but the GSC data paths need a live connected site.
- **`reindex --confirm`:** this posts to `/rankmath/v1/in/submitUrls`, which Rank Math forwards to IndexNow. We ran it once against an earlier build of this branch. IndexNow rejected the localhost URLs (429 in the indexing log), and Rank Math returned `HTTP 500 submit_failed`. The CLI surfaced that error with a non-zero exit. We did not re-run it, to avoid repeat external submissions. The preview path above runs live on this tree.
