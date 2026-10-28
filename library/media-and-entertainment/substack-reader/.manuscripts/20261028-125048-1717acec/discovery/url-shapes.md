# Substack URL shapes → what the CLI must point to

Attended verification **2026-07-07** (live authed + anonymous GETs). Answers the question: a subscribed publication can expose the *same* article under a "preview" URL and a "full" URL — what does the CLI resolve to?

## The four URL shapes

| # | Shape | Examples | What it is | Anonymous content |
|---|---|---|---|---|
| **A** | Publication **subdomain** | `uxmentor.substack.com/p/<slug>` | Pub's native Substack host | full (free) / preview (paid) |
| **B** | Publication **custom domain** | `creatoreconomy.so/p/<slug>`, `www.userresearchstrategist.com/p/<slug>` | Author vanity domain, *same content* | same as A |
| **C** | Reader-app **profile** | `substack.com/@<handle>` | Cross-pub discovery feed ("the app") | **preview only** |
| **D** | Reader-app **post** | `substack.com/home/post/p-<id>`, `substack.com/inbox/post/<id>` | Reader-app viewer, numeric post id | **preview only** |

**The canonical fetch target is always A/B: `<host>/api/v1/posts/<slug>`.** Never read *content* from C/D — those surfaces are preview-only by design (that's why a paying subscriber still sees a teaser on `substack.com/@uxmentor`). C/D are for **resolution only**.

## The three-name gotcha — you cannot guess the subdomain

The `@handle`, the custom domain, and the internal `.substack.com` subdomain are frequently three different strings. Proven live:

| Publication | `@handle` | custom domain | **real subdomain** |
|---|---|---|---|
| UX Mentor Diaries | `uxmentor` | — | `uxmentor` |
| Creator Economy / "Behind the Craft" | — | `creatoreconomy.so` | **`peteryang`** |
| The User Research Strategist | `userresearchstrategist` | `www.userresearchstrategist.com` | **`userresearchacademy`** |
| The Beautiful Mess (Cutlefish) | `cutlefish` | — | `cutlefish` |

`userresearchstrategist.substack.com` isn't even the publication — it **302→`substack.com/@userresearchstrategist`**. So the subdomain MUST be resolved from the API, never derived from the URL text.

## The unifying resolver (all proven live)

Take any URL → produce `(subdomain, slug)` → fetch `https://<subdomain>.substack.com/api/v1/posts/<slug>` (with the cookie for Tier-1; the client must carry the cookie through any 301→custom-domain — see `auth-model.md` → "Custom-domain crux"):

- **A/B (URL has a `/p/<slug>`):** GET the post JSON on its own host; the payload carries inline `subdomain` + `custom_domain` fields and `slug`. For custom-domain pubs, the `subdomain` field is how you find the `.substack.com` host.
- **C (`@handle`):** resolve handle → publication → subdomain.
- **D (numeric id, `home/post/p-<id>` or `inbox/post/<id>`):** `GET https://substack.com/api/v1/posts/by-id/<id>` → returns `{post, publication}` with `publication.subdomain`, `post.slug`, `post.canonical_url`, `post.audience`. Verified: `p-198935601` → URS / `userresearchacademy`; `205636469` → Cutlefish / `cutlefish`.

**Practical rule:** whatever URL the user pastes, canonicalise to the publication host (A/B). The subdomain host is also the only host where the `substack.sid` cookie is first-party in a browser — so it's the safest fetch target for Tier-1 (matches Maxime's instinct: "subdomain or custom domain is safer than the feed").

## Per-post gate ≠ per-pub tier

A publication can have a paid tier while every post is free. Cutlefish ("support" tier) returns `audience: everyone` on posts → full body anonymously (36,908 chars, images included). The gate is the per-post **`audience`** field (`everyone` | `only_paid` | `founding`), never the publication's pricing.

## Images — how the reader "sees" them

Neither Medium Reader nor this tool sees pixels. The body is captured as **text with image URLs + caption/alt text**:
- **Substack:** `body_html` has `<img src="https://substackcdn.com/image/fetch/…">` inside `<figure><figcaption>`; the `substackcdn.com` CDN URLs are **public** (load with no cookie, even on paid posts).
- **Medium Reader** (for reference): renders `![alt](cdn-url)` markdown (`internal/source/page/page.go`).
- To actually *view* an image, open the public CDN URL (an agent can fetch + view it). Optional MCP enhancement: surface a per-post `images: [{url, caption}]` list so links are one hop away.
