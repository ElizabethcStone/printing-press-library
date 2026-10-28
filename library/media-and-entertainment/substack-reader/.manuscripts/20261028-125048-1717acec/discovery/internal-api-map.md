# Substack internal API — endpoint catalog

Reconstructed 2026-07-06 from anonymous live GETs + NHagar/substack_api source. No official public API; this is the internal API the web app calls. Base = `https://<host>` where `<host>` = `<subdomain>.substack.com` or the publication's custom domain.

## Publication addressing
- Every pub is reachable at `<subdomain>.substack.com`; custom-domain form is optional.
- **`<subdomain>.substack.com` 301-redirects every path (incl `/api/v1/…`) to the custom domain** when one is set. Custom domain serves the API natively (200). CLI MUST follow cross-host 301s (Go default client does, anonymously).
- **Resolve custom domain → identity:** call the API on the custom-domain host directly (it works), or `GET /api/v1/homepage_data` → `…publication.subdomain` / `.custom_domain` / `.id`.
- **Name → pub:** `GET https://substack.com/api/v1/publication/search?query=&page=` (anon often returns `[]` — header/session sensitive).

## Endpoints (all GET; status = observed anonymously)

| Endpoint | Params | Pagination | Returns | Body? |
|---|---|---|---|---|
| `/api/v1/archive` | `sort`(new\|top\|community), `limit`, `offset`, `search` | **offset** (~12 default, up to ~50) | JSON **array** of post summaries | NO body (`body_html`/`truncated_body_text` null in list; `truncated_body_text` is the preview) |
| `/api/v1/posts/{slug}` | slug in path | single | JSON **object**, full post | **YES `body_html`** (free); empty for paid anon |
| `/api/v1/posts/by-id/{id}` | id in path | single | JSON object (wrapped/larger envelope — investigate) | TBD |
| `/api/v1/archive?search=<q>` | `search`,`sort`,`limit`,`offset` | offset | filtered array | **working per-pub search** |
| `/api/v1/post/search` | `query`,`offset`,`limit` | `more` bool | `{results[],more,…}` | anon `results:[]` — auth-sensitive; prefer `archive?search=` |
| `/api/v1/post/{postId}/comments` | `all_comments=true`,`sort` | one call | nested comment tree | — |
| `/api/v1/homepage_data` | — | single | `{newPosts,pinnedPosts,topPosts,recommendations, publication{}}` | best for pub metadata + domain resolution |
| `/api/v1/publication/search` (on substack.com) | `query`,`page` | page | `{results:[{id,name,subdomain,custom_domain}]}` | discovery |
| `/api/v1/categories` + `/api/v1/category/public/{id}/all?page=` | `page` | page (~21 cap) | category → newsletters | discovery |
| `/feed` | — | ~20 latest | **RSS XML**; paid posts truncated + "Read more" | podcast audio via `<enclosure>` |

## Body + gating
- Body: top-level **`body_html`** (HTML string) on the single-post object. `body_json` (ProseMirror) sometimes present. **The archive list has NO body — one `/posts/{slug}` call per post.**
- Gate field: **`audience`** = `everyone` (free) | `only_paid` | `founding`. Paywalled iff `audience != everyone`; anon → empty/teaser `body_html`. Supporting: `should_send_free_preview`, `free_unlock_required`, `post_preview_limit`, `truncated_body_text`, `meter_type`.

## Anti-bot / rate
- Behind Cloudflare but **permissive on read endpoints** — 200 with any/no UA, no "Just a moment", no JA3 gate. (Only bare `/api/v1/publication` → 403.) Plain HTTP works; no headless browser / TLS impersonation needed. `__cf_bm` cookie set; honoring cookies eases friction.
- **No rate-limit headers** on 200s. Server throttles under load (429). **Self-throttle** (small concurrency + delay); the build uses `cliutil.AdaptiveLimiter` at 2 rps.

## Build implications
Treat the user's host as base URL; always follow cross-host 301s (Tier-0); list via `archive` (offset, body absent); content via `posts/{slug}` reading `body_html`; gate on `audience`; search via `archive?search=`; keyless anonymous — no anti-bot workarounds for Tier 0.

Sources: live GETs against astralcodexten / www.astralcodexten.com / blog.bytebytego.com / www.thefp.com; NHagar/substack_api `newsletter.py`/`post.py`/`category.py`; substackexplorer.com/api-docs; iam.slys.dev + ignorance.ai reverse-engineering write-ups.
