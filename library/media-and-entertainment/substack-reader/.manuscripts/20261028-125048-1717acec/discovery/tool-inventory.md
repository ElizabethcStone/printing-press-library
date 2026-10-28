# Substack content-access tools — inventory (clone/beat targets)

Read-only research, 2026-07-06. Full sources at bottom. Scope: tools that READ/archive/search Substack post content (not account-publishing).

## Readers / libraries / CLIs

| Name | URL | Lang | Keyless? | Paid? | Surface | Notes |
|---|---|---|---|---|---|---|
| **substack-api** (NHagar) | github.com/NHagar/substack_api | Python 217★ | Yes; opt cookies | Yes (cookies.json) | Newsletter.get_posts/search_posts/get_podcasts/get_authors; Post.get_metadata/get_content/is_paywalled; User.get_subscriptions | **The reference endpoint library.** Active (v1.3.0 May 2026). Calls the stable `/api/v1/` surface. |
| **sbstck-dl** | github.com/alexferrari88/sbstck-dl | Go 224★ | Yes; cookie for private | Partial (issue #23: paid body not returned) | download/list/version; --format html/md/txt, --rate, --after/--before, --download-images, --create-archive | Archiver-CLI bar. Discovery via sitemap.xml — **now breaking** (Substack removed it). |
| **Substack2Markdown** | github.com/timf34/Substack2Markdown | Python 492★ | No (Selenium) | Yes (email/pw) | --url/--premium/--images, MD+HTML index | Highest-starred; heavy browser dep. |
| **substack_downloader** | github.com/Automatic365/substack_downloader | Python 0★ | Yes; cookie | Yes (SUBSTACK_COOKIE) | PDF/EPUB/JSON/HTML/TXT/MD | Widest formats, unmaintained. |
| **substack-dl** | github.com/nosajio/substack-dl | Rust 39★ | Yes | No | url + save-dir | Minimal. |
| **carlvellotti/substack-downloader** | github.com/carlvellotti/substack-downloader | — | No (Playwright) | Yes (per-domain session) | "your own content" framing | Handles custom domains via per-domain session files. |
| **python-substack** (ma2za) | github.com/ma2za/python-substack | Python 153★ | No | N/A | publishing/drafts | **Publish tool, not a reader** (disambiguation). |

## MCP servers (agent-native)

| Name | URL | Keyless? | Reads content? | Tools |
|---|---|---|---|---|
| **substack_mcp** (dkyazzentwatwa) | github.com/dkyazzentwatwa/substack_mcp | **Yes (public only)** | **Yes, broad** | 13: get_posts, get_all_posts, get_post_content, search, get_author_profile, get_notes, analyze_trends, crawl_publication… |
| **mcp-writer-substack** (jonathan-politzki) | github.com/jonathan-politzki/mcp-writer-substack | Yes (RSS) | Yes (Substack+Medium) | search_writing, refresh_content |
| substack-mcp (conorbronsdon) | github.com/conorbronsdon/substack-mcp | No (own pub) | own pub | list_published_posts, get_post… |
| substack-mcp (marcomoauro) | github.com/marcomoauro/substack-mcp | No | write-only | create_draft_post |

## Verdicts
- **Clone (endpoints):** NHagar/substack_api — cleanest object model, exercises the stable internal `/api/v1/`.
- **Beat (MCP):** dkyazzentwatwa/substack_mcp — the only keyless read-focused broad MCP; **weakness = no local corpus/SQL, fetches live per call.**
- **Match (archiver ergonomics):** sbstck-dl — multi-format, rate-limit, image download.
- **The opening no one fills:** a **local full-text-searchable corpus + SQL/analytics layer** (the Medium Reader pattern). Already working in this build.

## Reachability risk signals (design around)
1. Substack **removed sitemap.xml** → discover via `/api/v1/archive` (stable JSON), not sitemap. (sbstck-dl #14)
2. Cookie paid-download reported **broken** (`substack.sid` accepted, paid body not returned) — the custom-domain redirect crux. (sbstck-dl #23)
3. **Handle renames break lookups** → follow redirects / resolve via public_profile. (NHagar #7)
4. Paid `body_html` **null** unauthenticated by design. (NHagar #6)
5. **No Cloudflare/403** issues on the JSON endpoints — only 429 throttle. Positive: keyless plain-HTTP works.

Sources: the GitHub URLs above; sbstck-dl issues #14/#23; NHagar issues #6/#7; glama.ai/mcp?query=Substack.
