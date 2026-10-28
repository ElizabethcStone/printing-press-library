# Substack Reader — anonymous reachability probe + subscriber-gate crack

**Date:** 2026-07-06 (overnight autonomous session, Opus 4.8)
**Method:** read-only anonymous `curl` GET with a plain Chrome UA. No login, no cookies. Test publications: Astral Codex Ten (`astralcodexten.substack.com` → custom domain `www.astralcodexten.com`), ByteByteGo (`blog.bytebytego.com`).

## Headline findings

1. **The internal API is anonymously reachable — keyless, no Cloudflare/JA3 gate.**
   Plain `curl` with a normal UA gets **HTTP 200 `application/json`** on `/api/v1/archive` and `/api/v1/posts/{slug}`. This is a *far* easier crack than Medium (which needed uTLS/Surf Chrome impersonation to clear Cloudflare on every surface). Substack Tier-0 (free/public) needs no impersonation client — Go stdlib `net/http` should suffice. **Re-verify at build time** (anti-bot posture can change).

2. **Per-publication addressing + custom-domain redirect.**
   `<pub>.substack.com` **301-redirects** to the publication's custom domain when one is set (`astralcodexten.substack.com` → `www.astralcodexten.com`). The API lives on the *final* host. Subdomain-only pubs (`blog.bytebytego.com`) serve the API directly (200, no redirect).
   → The client MUST follow redirects, and (critical) MUST **forward the session cookie across the redirect to the custom domain**. This is the exact bug class we already fixed in Medium Reader (`ForwardHeadersOnRedirect`). Carry that fix over from day one.

3. **The free/paid tier field is `audience`.** Values seen: `everyone` (free), `only_paid` (paid). (`founding` tier also exists per Substack docs.) In a recent 23-post ACX slice: 19 `everyone`, 4 `only_paid`.

## Endpoint shapes confirmed

| Endpoint | Method | Returns | Body? |
|---|---|---|---|
| `/api/v1/archive?sort=new&limit=N&offset=M` | GET | JSON **array** of post metadata (newest first) | NO full body; carries `truncated_body_text` (preview) + metadata |
| `/api/v1/posts/{slug}` | GET | JSON **object** (flat) for one post | **YES — full `body_html`** for free posts; empty for locked paid posts |
| `/api/v1/posts/by-id/{id}` | GET | 200 JSON but a **different/wrapped envelope** (flat-key probe returned all None) — investigate shape; prefer `{slug}` | TBD |

Archive pagination = **offset-based** (`limit` + `offset`). Confirm the max `limit` (Substack commonly caps ~50/page) and the total-count/`has more` signal at build time.

Post-object gating fields present: `audience`, `free_unlock_required`, `post_preview_limit`, `should_send_free_preview`, `truncated_body_text`, `is_geoblocked`, `postCountryBlocks`.

## The subscriber gate — the crack (proven anonymously)

Same request, two posts, no cookie:

| Post | `audience` | anonymous `body_html` len | meaning |
|---|---|---|---|
| `open-thread-441` | `everyone` | **2119** | free → full body delivered keyless |
| `hidden-open-thread-4405` | `only_paid` | **0** | paid → **body withheld** = the gate |

**Runtime full-vs-preview detector (Substack's `isLockedPreviewOnly` analog):**
> A post with `audience != "everyone"` returning an **empty/whitespace `body_html`** (or only `truncated_body_text`) = you are seeing the LOCKED view. A **non-empty `body_html` on a non-free post** = your credential unlocked it.

This single check is the reliable Tier-1 success signal. Far cleaner than guessing from length alone.

## Auth model (to be confirmed by the two live checks below)

- **Tier 0 (default, keyless):** free posts (`audience: everyone`) — full body anonymously. Powers `read`/`archive`/`search`/`feed` for all free content with zero setup.
- **Tier 1 (user's OWN `substack.sid` cookie):** unlocks paid posts the user is *already entitled to*. Optional, never required, never redistributed. Entitlement-bound posture identical to Medium Reader's `sid` design.
- **Alt unlock — email `?token=`:** paid posts emailed to subscribers carry a magic-link `?token=` that unlocks the full body without a session. Community-reported; verify programmatic viability. Potential *credential-free* Tier-1 path.

## MORNING HANDOFF — the two things I can't verify unsupervised

1. **Cookie unlock:** with Maxime's real logged-in Substack `substack.sid` cookie, does `GET /api/v1/posts/hidden-open-thread-4405` (a pub he pays for) return a non-empty `body_html`? Confirms Tier-1 + the exact cookie name/domain/precedence. (Needs a publication he actually subscribes to — ACX paid is one option, or any of his paid subs.)
2. **`?token=` unlock:** grab a real paid-post email link and confirm the `?token=` param returns the full body anonymously.

Both are ~5-minute checks and finalize the auth spec. Everything else (free tier) is buildable and verifiable tonight without them.
