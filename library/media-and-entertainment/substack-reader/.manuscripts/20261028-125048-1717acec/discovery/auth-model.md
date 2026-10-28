# Substack subscription/auth model — the crack

**Date:** 2026-07-06 (overnight autonomous, Opus 4.8). Sources: Substack support/FAQ/ToS, community libraries (NHagar `substack_api`, postcli, carlvellotti, RSS-Bridge), reverse-engineering write-ups. All from public sources — no login, no cookie harvesting. Cross-checked against my own anonymous probes (`reachability-probe.md`).

## The model in one picture

Substack is **per-publication**: every author is their own site at `<pub>.substack.com` (and optionally a custom domain). The **entitlement decision is server-side, per request, keyed on the session you present.** The same post URL returns a different body depending on your cookie.

| Post `audience` | Anonymous | Logged-in free sub | Logged-in **paid** sub |
|---|---|---|---|
| `everyone` (free) | **full body** | full | full |
| `only_paid` / `founding` | paywall stub / preview, **body withheld** | same | **full body** |

The body is gated **server-side** — for a locked post the full HTML is simply *not in the response* for a non-entitled reader. So client-side DOM tricks can't work; you need the credential. (Confirmed by my probe: paid post `body_html` len = 0 anonymously.)

## Tier model for the tool (mirrors Medium Reader's posture)

- **Tier 0 — keyless (default).** Free posts (`audience: everyone`), full body, zero setup. Powers `archive`/`read`/`search`/`feed`/`digest`/`corpus` for all free content. Anonymous plain-HTTP works (no Cloudflare challenge on read endpoints).
- **Tier 1 — the user's OWN credential (optional, never required, never redistributed).** Unlocks paid posts the user *already pays for*. Two legitimate mechanisms:
  1. **Session cookie** `substack.sid` (+ `connect.sid`) — sent as `Cookie:` header.
  2. **Private RSS feed** `https://<pub>.substack.com/feed/private/<token>` — a per-subscriber URL that returns **full paid bodies** with no cookie. Cleaner (no cookie-DB, no httpOnly wall), but per-publication.

## The session cookie

- **Names:** `substack.sid` (primary) + `connect.sid` (Express session), both `s%3A…` (signed express-session). `substack.lli` is a JWT carrying `userId` (helper for auto-discovery, *not* the auth credential).
- **Likely httpOnly** (every tool reads it from Chrome's cookie DB / DevTools / Playwright, never `document.cookie`) → plan for cookie-store extraction or manual paste/env, **not** `document.cookie`. *(live-verify)*
- **Domain scope — the load-bearing fact:** cookies are set on **`.substack.com`**, so one session authenticates **every `<pub>.substack.com` subdomain** you're logged into. **Custom domains do NOT receive the `.substack.com` cookie.**
- **Lifespan ~3 months.**

## ⚠ The custom-domain × cookie redirect problem (the crux to solve at build time)

This is the exact class of bug we already fixed once in Medium Reader (`ForwardHeadersOnRedirect`), and it's *the* subtle Substack engineering problem:

- `<pub>.substack.com/api/v1/posts/{slug}` **301-redirects to the custom domain** (proven in my probe).
- The `.substack.com` cookie is sent to the subdomain request but **is not forwarded across the cross-host 301** to the custom domain → the authenticated request silently downgrades to anonymous → you get the *locked* view even though you're a paid subscriber. (This matches sbstck-dl issue #23: "valid cookie accepted but paid body not returned.")
- **postcli's documented workaround:** for custom-domain pubs, hit the **`.substack.com` mirror subdomain** (which the cookie reaches) rather than the custom domain. Open question: does the subdomain API serve the authed body *before* redirecting, or must redirect-following be disabled on the authed path? **This is the #1 live-verify item.**

Design implication: the fetch client needs **two redirect policies** — for Tier-0 (anonymous) follow cross-host 301s to the custom domain freely; for Tier-1 (cookie) prefer the `.substack.com` host and either (a) don't follow the cross-host redirect, or (b) forward the cookie onto the custom domain host. Decide empirically in the morning crack.

## The email `?token=` path (clarified — don't over-claim it)

- The magic-link `…/authenticate?token=…` is a **one-time login token that *sets* the session cookie** — an auth bootstrap, not a reusable per-post unlock.
- Per-post email links to paid subscribers open the full post but carry session context; **no public source documents a clean reusable per-post `?token=` that returns a full body via JSON.** Treat "email token = standalone full-body fetch" as **unverified**; its real value is bootstrapping the cookie (postcli's OTP fallback captures `substack.sid`/`connect.sid` from `Set-Cookie`).

## The private RSS token path (clean legitimate full-body path)

- **Public** `/<pub>/feed` → free posts full, paid posts **truncated** with **"Read more"** appended to `<content>` (a reliable truncation marker).
- **Private** `/<pub>/feed/private/<token>` → **full paid bodies** for pubs you subscribe to. Per-subscriber, self-contained, no cookie. Password-equivalent — never commit/share it.
- Feeds are RSS 2.0; full body in the `content:encoded` element.

## The runtime full-vs-preview detector (Substack's `isLockedPreviewOnly` analog)

No single official boolean. Use layered signals:
```
got_full_body =
    (audience == "everyone")
    OR (audience in {"only_paid","founding"}
        AND body_html is non-empty
        AND no "paywall"/"paywall-title" div
        AND no "Read more" truncation marker)
```
`audience` = *intent to gate*; `body_html` presence + no-paywall-div = *proof you received it*. This is the reliable Tier-1 success check and the honest "you're not entitled to this one" signal.

## Entitlement / ToS posture (bake into README + scope guardrail)

**Defensible:** 1 user → their **own** credential → their **own** entitled content → their **own** on-demand reading. Functionally identical to their browser/RSS reader making the same authed request. Free posts anonymously = unambiguously fine.

**Out of bounds (design against it):** redistributing paid bodies; reading paid content without entitlement; **bulk crawling/scraping** (Substack ToS prohibits "crawl/scrape/spider"); sharing the cookie/private-token.

**Net → the tool's guardrails:** read-only, on-demand (not a crawler), personal, self-throttled (429 is the only observed limit; no rate headers), Tier-1 strictly opt-in and never redistributed. Same stance as Medium Reader; put it in the README ethics section verbatim.

## MORNING HANDOFF — live-verify checklist (needs Maxime's logged-in + paid session)

Each is a ~5-min check that finalizes the auth spec:

1. **httpOnly/Secure/SameSite** of `substack.sid` / `connect.sid` (DevTools → Application → Cookies).
2. **Cross-subdomain reach:** one session → `GET /api/v1/posts/{slug}` on a *different* pub you sub to returns full body (confirms `.substack.com` scope).
3. **Custom-domain fallback + the redirect crux:** does `<pub>.substack.com/api/v1/posts/{slug}` with the cookie return the full body (and how does the 301 interact)?
4. **Exact authed body field name(s)** on a paid post: `body_html` vs `body` vs `bodyHtml`, and any `should_paywall`/`free_unlock` flags.
5. **Anonymous-vs-authed diff** of the same paid post JSON → the ground-truth field that flips.
6. **Private RSS token:** your `/feed/private/<token>` returns full `content:encoded`, no "Read more".
7. **`connect.sid` necessity:** does `substack.sid` alone suffice (RSS-Bridge only asks for it) or is `connect.sid` also required?
8. **Login-token reuse:** confirm `/authenticate?token=` is single-use and only sets a session.

Items 3, 4, 5 are the ones that unblock the Tier-1 build; everything for Tier 0 is already verified and buildable.

---

## ✅ CRACK RESULTS — attended, 2026-07-07 (Maxime's live paid session)

Ran the checklist against a real logged-in + paid session (subscriptions: UX Mentor Diaries [monthly], Creator Economy / "Behind the Craft" [annual]). All values are from live authenticated GETs; cookie masked, `0600` file, never logged.

**Credential — `substack.sid` ALONE is sufficient:**
- sid-only → full authed body; `connect.sid` alone → **404** (useless); both = same as sid-only. → the cookie file needs **only `substack.sid`**. (`substack.lli` is a discovery JWT, not needed.)
- Sent verbatim as `Cookie: substack.sid=<value>` in the stored URL-encoded `s%3A…` form. `httpOnly + Secure + SameSite=None`; lifespan ~3 mo (this one expires 2026-10-05).

**One cookie → all subscriptions (account-level, NOT per-publication).** The *same* cookie unlocked TWO different publications:

| Pub | host fetched | anon `body_html` | authed `body_html` | Δ |
|---|---|---|---|---|
| UX Mentor Diaries | `uxmentor.substack.com` (subdomain) | 13,856 | **42,419** | +28,563 |
| Creator Economy | `creatoreconomy.so` (custom domain) | 15,444 | **27,232** | +11,788 |

Authenticated responses carry per-user fields (`is_viewed:true`, `read_progress`, `max_read_progress`, `is_saved`, `reaction`) — the ground-truth "served to a logged-in you" signal.

**Custom-domain crux — SOLVED, and simpler than feared:**
- `<subdomain>.substack.com/api/v1/posts/{slug}` **301-redirects to the custom domain** for custom-domain pubs (`peteryang.substack.com` → `creatoreconomy.so`); the subdomain serves NO body (301 only).
- BUT the **custom domain honors `substack.sid` when the cookie is attached to that host's request** (proven: `creatoreconomy.so` + cookie → 27,232 + `is_viewed:true` + `read_progress:0`). No ".substack.com mirror" workaround is needed.
- **Only engineering requirement: the HTTP client must carry the `Cookie` header THROUGH the cross-host 301** onto the final custom-domain host. Go's default `http.Client` **strips** `Cookie`/`Authorization` on cross-host redirects (since Go 1.8) → this is exactly the Medium `ForwardHeadersOnRedirect` bug. Fix = a `CheckRedirect` that re-attaches the cookie on substack-origin cross-host redirects, OR resolve the 301 `Location` and fetch it directly with the cookie. No-custom-domain pubs (uxmentor) don't redirect; cookie on the subdomain → full body directly.

**Gate-signal correction (SUPERSEDES the earlier "empty `body_html` = gate" claim, which was wrong for these pubs):**
- Anonymous paid posts return a **substantial preview** `body_html` (13,856 chars for uxmentor), NOT empty. "non-empty body_html" is therefore NOT proof of full access — it gives false positives.
- Reliable full-vs-preview detector: (a) `audience==everyone` → full; else (b) authed fetch + confirm per-user field presence (`is_viewed`), and (c) compare the rendered body's word count to the post's **`wordcount`** field (1469 — reported *identically* in anon and authed, so it's a stable reference for the full article's true length). Preview rendered-words ≪ `wordcount`; full ≈ `wordcount`.
- The `"paywall"` string is UNRELIABLE as a lock signal — it appeared **once in the FULL authed body** and **zero times in the anon preview** (it's a member widget, not the truncation marker). Do not gate on it.

**Per-pub redirect topology is NOT uniform** (confirmed): `uxmentor` = 200 native (no custom domain); `peteryang`/ACX = subdomain 301→custom domain; a non-real vanity subdomain (`userresearchstrategist.substack.com`) 302→`substack.com/@handle`. The client must resolve the *real* subdomain (never guess from handle/domain) and handle 200 / 301→custom / 302→profile. See `url-shapes.md`.

**Auth-probe note:** `substack.com/api/v1/subscriptions` is NOT a reliable cookie-validation endpoint — it returned 400/401 even with a valid session. Validate a Substack cookie by fetching any post and checking for **per-user fields** (`is_viewed`, `read_progress`, `is_saved`); the post endpoint is the arbiter, and a paid-body length jump vs the anonymous preview confirms entitlement.

---

## ⚠️ CORRECTION — custom-domain Tier-1 mechanism (build session, 2026-07-07 PM)

The CRACK RESULTS claim above — *"the custom domain honors `substack.sid` when the cookie is attached to that host's request (creatoreconomy.so + cookie → 27,232)"* — **does NOT reproduce** and is superseded. Verified live in the build session with a **known-valid** cookie (the same cookie returns uxmentor's full body 42,419 + `is_viewed:true`):

- **Direct authed fetch of the custom domain FAILS.** `GET https://creatoreconomy.so/api/v1/posts/<slug>` with `Cookie: substack.sid=…` returns the **anonymous preview** (15,444 chars, `is_viewed` absent), whether hit cold or by carrying the cookie through the `peteryang.substack.com` 301. The custom domain is a **different registrable domain**, so `substack.sid` (a `.substack.com` cookie) is simply **not first-party** there — no header trick changes that. The custom domain sets only its own analytics cookies (`ab_testing_id`, `__cf_bm`), no session cookie.
- **The universal Tier-1 path is `substack.com/api/v1/posts/by-id/<id>` AUTHED.** `substack.sid` **is** first-party on `substack.com`, so this returns the full `post.body_html` + `is_viewed:true` for **both** shapes: uxmentor (subdomain) → 42,419, creatoreconomy.so (custom domain) → 27,232. The envelope is `{post, publication, publicationSettings, subscription}`; read `post.body_html`.
- **Design consequence:** the Substack Reader routes **every** Tier-1 fetch through the `substack.com` by-id endpoint (needs the numeric post id, obtained from the anonymous resolve fetch). The **ForwardHeadersOnRedirect / carry-cookie-through-the-301 fix is therefore NOT load-bearing** for the primary path (by-id does not redirect). The client keeps a narrow `CheckRedirect` that re-attaches the cookie only on `*.substack.com` hops as a safe backstop, but the custom-domain crux the earlier notes obsessed over is moot: don't fetch the custom domain authed at all — go through substack.com by-id. The `.lli` JWT and cookie-through-redirect machinery are not needed.
- **Cookie transcription is a real trap (reconfirmed).** The build's first cookie value 401'd everywhere; the cause was a single **capital-`I` ↔ lowercase-`l`** transcription error (`…9cDIzxf-DIDplz…` vs the correct `…9cDlzxf-DlDplz…`), NOT a stale session. The live authed fetch is the only arbiter — never eyeball-compare a `substack.sid`.
