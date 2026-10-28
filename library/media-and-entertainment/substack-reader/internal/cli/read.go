// Copyright 2026 Maxime Delavergne and contributors. Licensed under Apache-2.0. See LICENSE.
// Novel command: entitlement-aware read of one or more Substack posts.
// Hand-implemented (Tier-0 keyless for free posts; Tier-1 via the user's own
// substack.sid for paid posts they subscribe to). generate --force preserves
// implemented bodies.

package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mvanhorn/printing-press-library/library/media-and-entertainment/substack-reader/internal/cliutil"
	"github.com/mvanhorn/printing-press-library/library/media-and-entertainment/substack-reader/internal/store"
	"github.com/mvanhorn/printing-press-library/library/media-and-entertainment/substack-reader/internal/substack"
)

// readResult carries one post's fetch outcome: the machine envelope plus the
// pieces the human renderer needs. A per-post failure fills err (and the
// envelope carries {"post": ..., "error": ...}) so a batch read reports each
// post honestly instead of dying on the first bad slug.
type readResult struct {
	envelope  map[string]any
	meta      substack.PostMeta
	bodyText  string          // plain text: the word count and the full-vs-preview verdict are taken from it
	bodyMD    string          // real Markdown (links, headings, lists, images)
	rawPost   json.RawMessage // the post object as Substack returned it, for the corpus
	access    substack.Access
	canonHost string
	err       error
	// authFetchErr is why the authenticated fetch of a paid post failed, when a
	// session was configured and the post fell back to its public preview. It
	// stays nil for a post that was never meant to be fetched with the session.
	authFetchErr error
}

// readClients is the pair of HTTP clients one `read` run uses for every post.
// Each client owns a rate limiter, and a 429 lowers its pace; building a new
// pair per post threw that slow-down away, so a fast bulk read kept hitting the
// limit and saved previews. One pair per batch carries the pace across posts.
type readClients struct {
	anon   *substack.Client // anonymous: resolves the post and fetches free bodies/previews
	authed *substack.Client // carries the session cookie; nil when no session is configured
}

func newReadClients(sess substack.Session) *readClients {
	rc := &readClients{anon: substack.NewClient()}
	if !sess.IsZero() {
		rc.authed = substack.NewAuthedClient(sess)
	}
	return rc
}

// runReadBatch runs fetch once per post, minting a FRESH context for each so
// every post gets its own --timeout budget. A shared batch-wide deadline would
// make one slow post starve all later ones into "context deadline exceeded";
// per-post budgets keep batch semantics identical to N single-post runs.
func runReadBatch(args []string, newCtx func() (context.Context, context.CancelFunc), fetch func(ctx context.Context, arg string) readResult) []readResult {
	results := make([]readResult, 0, len(args))
	for _, arg := range args {
		ctx, cancel := newCtx()
		results = append(results, fetch(ctx, arg))
		cancel()
	}
	return results
}

// readOnePost fetches and classifies a single post. It is the loop body of the
// batch read: same anonymous-then-authenticated two-fetch dance the single-post
// command always did, factored out so `read a b c` stops requiring a bash
// for-loop (PATCH amend-2026-07-31: batch read).
func readOnePost(ctx context.Context, cmd *cobra.Command, arg string, sess substack.Session, clients *readClients) readResult {
	ref, err := substack.ParsePostRef(arg)
	if err != nil {
		return readResult{err: usageErr(err)}
	}

	anon := clients.anon

	// Resolve to a canonical (host, slug). For a numeric reader-app id
	// this hits the by-id endpoint; the real subdomain can't be guessed.
	host, slug, err := ref.ResolveHostSlug(ctx, anon)
	if err != nil {
		return readResult{err: err}
	}

	// Fetch #1, anonymous: learns audience + the post's own
	// subdomain/custom_domain (a subdomain URL 301-redirects to the
	// custom domain, which Go follows anonymously) and returns the full
	// body for free posts or the public preview for paid ones.
	raw, err := anon.FetchPost(ctx, "https://"+host, slug)
	if err != nil {
		return readResult{err: fmt.Errorf("fetching post: %w", err)}
	}
	if looksLikeHTML(raw) {
		// A vanity subdomain that isn't the real publication host
		// 302-redirects to a reader-app profile HTML page instead of
		// serving post JSON (url-shapes.md: the three-name gotcha).
		return readResult{err: fmt.Errorf("%s returned a web page, not post JSON — that subdomain likely redirects to a reader-app profile; paste the canonical post URL (<pub>.substack.com/p/<slug> or the publication's custom domain)", host)}
	}
	meta, err := substack.ParsePostMeta(raw)
	if err != nil {
		return readResult{err: fmt.Errorf("parsing post: %w", err)}
	}
	// Derive the canonical host from canonical_url first: the single-post
	// object does NOT reliably carry subdomain/custom_domain (verified
	// against peteryang -> creatoreconomy.so — neither field is present),
	// but canonical_url always points at the real publication host where
	// the full body lives (the custom domain when the pub has one). This
	// is what makes the authed fetch target the host that honors the
	// cookie directly, instead of the subdomain that 301s and strips it.
	canonHost := hostFromURL(meta.CanonicalURL)
	if canonHost == "" {
		canonHost = substack.CanonicalHost(meta.Subdomain, meta.CustomDomain)
	}
	if canonHost == "" {
		canonHost = host
	}

	authed := false
	var authFetchErr error
	isFree := strings.EqualFold(strings.TrimSpace(meta.Audience), "everyone")

	// Fetch #2, authenticated: only for a paid post when a session is
	// configured. The universal Tier-1 path is substack.com's by-id
	// endpoint, where substack.sid is first-party for BOTH subdomain and
	// custom-domain publications — verified: it returns the full body +
	// is_viewed for uxmentor (subdomain) AND creatoreconomy.so (custom
	// domain), whereas a direct custom-domain fetch does NOT honor the
	// cookie (it's a different registrable domain). This supersedes the
	// earlier "fetch the custom domain directly" approach.
	if !isFree && !sess.IsZero() {
		if meta.ID == "" {
			fmt.Fprintf(cmd.ErrOrStderr(), "warning: no post id available for the authenticated fetch; showing the public preview\n")
		} else {
			env, ferr := clients.authed.FetchPostByID(ctx, meta.ID)
			if ferr == nil {
				var post json.RawMessage
				post, ferr = substack.ExtractByIDPost(env)
				if ferr == nil {
					raw = post
					if m2, perr := substack.ParsePostMeta(post); perr == nil {
						// by-id post objects carry no subdomain/custom_domain;
						// keep the canonHost we already derived for display.
						if m2.CanonicalURL == "" {
							m2.CanonicalURL = meta.CanonicalURL
						}
						meta = m2
					}
					authed = true
				}
			}
			if ferr != nil {
				authFetchErr = ferr
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: authenticated fetch failed (%v); showing the public preview\n", ferr)
			}
		}
	}

	bodyText := substack.HTMLToText(meta.BodyHTML)
	bodyMD := substack.HTMLToMarkdown(meta.BodyHTML)
	rendered := substack.WordCount(bodyText)
	access := substack.DetectAccess(meta, rendered, authed)

	return readResult{
		envelope: map[string]any{
			"host":          canonHost,
			"slug":          meta.Slug,
			"title":         meta.Title,
			"subtitle":      meta.Subtitle,
			"post_date":     meta.PostDate,
			"audience":      meta.Audience,
			"wordcount":     meta.Wordcount,
			"canonical_url": meta.CanonicalURL,
			"access":        access.Tier,
			"full":          access.Full,
			"authenticated": authed,
			"reason":        access.Reason,
			"body_text":     bodyText,
			"body_markdown": bodyMD,
			"body_html":     meta.BodyHTML,
		},
		meta:         meta,
		bodyText:     bodyText,
		bodyMD:       bodyMD,
		rawPost:      raw,
		access:       access,
		canonHost:    canonHost,
		authFetchErr: authFetchErr,
	}
}

// pp:data-source live
func newNovelReadCmd(flags *rootFlags) *cobra.Command {
	var saveDir, dbPath string
	var saveImages, noStore bool

	cmd := &cobra.Command{
		Use:   "read <post>...",
		Short: "Read one or more posts' full text; free posts keyless, paid posts via your own session",
		Long: `Read one or more Substack posts' full text.

Free posts (audience "everyone") are keyless — zero setup. Paid posts you
already subscribe to unlock with your own substack.sid session cookie
(SUBSTACK_SESSION env, or a JSON cookie file in the config dir); this reads only
what you are entitled to and is never required for free content.

Accepts any Substack URL shape, or the bare "<pub>/<slug>" form:
  substack-reader-pp-cli read astralcodexten/open-thread-441
  substack-reader-pp-cli read https://uxmentor.substack.com/p/<slug>
  substack-reader-pp-cli read https://creatoreconomy.so/p/<slug>
  substack-reader-pp-cli read https://substack.com/home/post/p-<id>

Multiple posts read in one call — no shell loop needed:
  substack-reader-pp-cli read astralcodexten/slug-one astralcodexten/slug-two --agent --select slug,subtitle
With more than one post, JSON mode emits an ARRAY of post envelopes (a failed
post becomes a {"post", "error"} entry and the others still return); a single
post keeps the original single-object envelope.

Each post comes back as body_markdown (real Markdown: links, headings, lists,
images, YouTube, PDF attachments), body_text (the flat text the full-vs-preview
check counts) and body_html (exactly as Substack sent it).

--save <dir> writes every post as <slug>.md (title, source, date, access and word
count as properties) plus <slug>.html, the untouched master copy to convert again
later. Add --images to download the post's images into <slug>--images/ and link
them from the note, so it reads offline. When the folder already holds a note of
that slug from another publication, the files are named <publication>--<slug>
instead; saving the same post again overwrites its own files.

Every post read is also kept in the local corpus, body included, so search and
sql find it; a full copy is never replaced by a later preview. --no-store skips
that, and so does SUBSTACK_READER_NO_LOCAL_DATA=1.

When a paid post can't be unlocked, the output is honest about it: you get the
public preview plus a clear "preview only — not entitled" signal, never a
silent downgrade. With a session configured, a batch that still contains
previews says "N of M posts are previews" and exits non-zero (4; 7 when the
authenticated fetch was rate limited).`,
		Example: "  substack-reader-pp-cli read astralcodexten/open-thread-441\n" +
			"  substack-reader-pp-cli read uxmentor/some-post --save ~/notes/uxmentor --images",
		// No mcp:read-only annotation: read WRITES the local SQLite corpus (and
		// files, with --save), so per the MCP readOnlyHint contract it is not
		// read-only. It inherits the conservative default, like archive and sync.
		//
		// pp:happy-args gives the live-dogfood probe a real positional; without it
		// happy_path skips on the non-ID <post> and phase 5 reports this feature
		// as hollow coverage.
		Annotations: map[string]string{
			"pp:happy-args": "post=astralcodexten/open-thread-441",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			if dryRunOK(flags) {
				return writeDryRun(cmd.OutOrStdout(), flags, "read")
			}

			if saveImages && saveDir == "" {
				return usageErr(fmt.Errorf("--images needs --save <dir>: it saves the images next to the notes"))
			}

			// Load the optional Tier-1 session once for the whole batch.
			// LoadSession errors only when an EXPLICIT SUBSTACK_COOKIE_FILE was
			// selected but is unusable — an authoritative config error we
			// surface instead of silently serving previews. A broken *default*
			// cookie degrades to anonymous inside LoadSession (warning only),
			// so a plain free read is never blocked.
			sess, sessErr := substack.LoadSession()
			if sessErr != nil {
				return sessErr
			}

			// Each post gets its OWN --timeout budget: a shared pre-loop
			// context made the deadline cumulative across the batch, so a
			// slow early post starved every later one into
			// "context deadline exceeded" (Greptile round-1 finding).
			// One client pair for the whole batch: the pace a 429 forces on the
			// limiter must carry over from one post to the next.
			clients := newReadClients(sess)

			// The corpus is opened once for the batch. Failing to open it is a
			// warning, not an error: reading is the job, storing is the bonus.
			var db *store.Store
			noStore = noStore || localDataDisabled()
			if !noStore {
				if dbPath == "" {
					dbPath = defaultDBPath("substack-reader-pp-cli")
				}
				ctx, cancel := boundCtx(cmd.Context(), flags)
				var err error
				db, err = store.OpenWithContext(ctx, dbPath)
				cancel()
				if err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "warning: could not open the corpus at %s (%v); posts will not be stored\n", dbPath, err)
					db = nil
				} else {
					defer db.Close()
				}
			}

			results := runReadBatch(args,
				func() (context.Context, context.CancelFunc) { return boundCtx(cmd.Context(), flags) },
				func(ctx context.Context, arg string) readResult {
					res := readOnePost(ctx, cmd, arg, sess, clients)
					if res.err == nil {
						finishPost(ctx, cmd, &res, db, !noStore, saveDir, saveImages, clients)
					}
					return res
				})

			failed := 0
			for i := range results {
				res := &results[i]
				if res.err != nil {
					failed++
					if len(args) == 1 {
						// Single-post reads keep the original contract: the
						// error is the command result, not an array entry.
						return res.err
					}
					fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s: %v\n", args[i], res.err)
					res.envelope = map[string]any{"post": args[i], "error": res.err.Error()}
				}
			}
			if failed == len(args) {
				return fmt.Errorf("all %d posts failed to read", len(args))
			}

			// emit writes the posts in the output format the caller asked for.
			emit := func() error {
				// Machine output: a structured envelope (agents pipe/select on it).
				// One post = the original single-object envelope (back-compat);
				// several posts = an array of envelopes.
				// read is a live command (two fetches against the publication),
				// so the agent envelope must say so: the printOutputWithFlags
				// default stamped meta.source "local", misreporting provenance.
				liveMeta := map[string]any{"source": "live"}
				if !wantsHumanTable(cmd.OutOrStdout(), flags) {
					if len(results) == 1 {
						data, err := json.Marshal(results[0].envelope)
						if err != nil {
							return err
						}
						return printOutputWithFlagsMeta(cmd.OutOrStdout(), data, flags, liveMeta)
					}
					// Batch: apply --select per successful envelope ourselves so a
					// {"post","error"} entry survives field selection instead of
					// collapsing to {} — a silent hole in the array.
					outputFlags := *flags
					envelopes := make([]map[string]any, 0, len(results))
					for _, r := range results {
						env := r.envelope
						if r.err == nil && flags.selectFields != "" {
							raw, err := json.Marshal(env)
							if err != nil {
								return err
							}
							var picked map[string]any
							if json.Unmarshal(filterFields(raw, flags.selectFields), &picked) == nil {
								env = picked
							}
						}
						envelopes = append(envelopes, env)
					}
					if flags.selectFields != "" {
						outputFlags.selectFields = ""
						outputFlags.compact = false
					}
					data, err := json.Marshal(envelopes)
					if err != nil {
						return err
					}
					return printOutputWithFlagsMeta(cmd.OutOrStdout(), data, &outputFlags, liveMeta)
				}

				// Human output.
				w := cmd.OutOrStdout()
				first := true
				for _, res := range results {
					if res.err != nil {
						continue // already warned on stderr
					}
					if !first {
						fmt.Fprintln(w, "\n"+strings.Repeat("─", 40)+"\n")
					}
					first = false
					meta, bodyMD, access := res.meta, res.bodyMD, res.access
					if meta.Title != "" {
						fmt.Fprintf(w, "%s\n", meta.Title)
					}
					if meta.Subtitle != "" {
						fmt.Fprintf(w, "%s\n", meta.Subtitle)
					}
					metaLine := res.canonHost + "/p/" + meta.Slug
					if meta.PostDate != "" {
						metaLine += " · " + meta.PostDate
					}
					metaLine += " · audience: " + orDash(meta.Audience)
					fmt.Fprintln(w, metaLine)
					fmt.Fprintf(w, "Access: %s — %s\n", access.Tier, access.Reason)
					fmt.Fprintln(w)
					if bodyMD == "" {
						fmt.Fprintln(w, "(no body returned)")
					} else {
						fmt.Fprintln(w, bodyMD)
					}
					if !access.Full {
						fmt.Fprintf(cmd.ErrOrStderr(), "\nThis is a preview. %s\n", access.Reason)
					}
				}
				return nil
			}
			if err := emit(); err != nil {
				return err
			}
			// With a session configured, a preview means something went wrong
			// (rate limit, expired cookie, not entitled): say so and exit non-zero
			// instead of reporting a clean run over silently downgraded posts.
			return previewReport(results, !sess.IsZero())
		},
	}
	cmd.Flags().StringVar(&saveDir, "save", "", "save each post as <slug>.md (with properties) and <slug>.html in this folder (<publication>--<slug> when another publication's post already has that slug)")
	cmd.Flags().BoolVar(&saveImages, "images", false, "with --save, also download the post's images next to the note")
	cmd.Flags().BoolVar(&noStore, "no-store", false, "do not keep the posts read in the local corpus")
	cmd.Flags().StringVar(&dbPath, "db", "", "database path (default: standard location)")
	return cmd
}

// finishPost does what happens to a post once it has been read: keep it in the
// corpus and, with --save, write its files. Neither can fail the read; each
// problem is a stderr warning and shows in the post's envelope (stored, saved).
func finishPost(ctx context.Context, cmd *cobra.Command, res *readResult, db *store.Store, wantStore bool, saveDir string, images bool, clients *readClients) {
	errw := cmd.ErrOrStderr()
	slug := res.meta.Slug
	if wantStore {
		stored := false
		switch {
		case db == nil:
			// the open failure was already reported
		case res.meta.ID == "":
			fmt.Fprintf(errw, "warning: %s has no post id; not stored in the corpus\n", slug)
		default:
			rec, err := corpusRecord(res.rawPost, res.canonHost, res.bodyText, res.access)
			if err == nil {
				stored, err = storeRead(db, res.meta.ID, rec, res.access)
			}
			switch {
			case err != nil:
				fmt.Fprintf(errw, "warning: %s not stored in the corpus: %v\n", slug, err)
			case !stored:
				fmt.Fprintf(errw, "note: %s is only a preview; the full copy already in the corpus was kept\n", slug)
			}
		}
		res.envelope["stored"] = stored
	}
	if saveDir != "" {
		files, err := savePost(ctx, saveDir, savedPost{
			Slug: slug, Title: res.meta.Title, Subtitle: res.meta.Subtitle,
			Source: res.meta.CanonicalURL, PostDate: res.meta.PostDate, Access: res.access.Tier,
			Wordcount: res.meta.Wordcount, BodyHTML: res.meta.BodyHTML, Markdown: res.bodyMD,
		}, images, clients.anon.FetchImage)
		if err != nil {
			fmt.Fprintf(errw, "warning: %s not saved: %v\n", slug, err)
			return
		}
		if files.ImageFailures > 0 {
			fmt.Fprintf(errw, "warning: %s: %d image(s) could not be downloaded; their links stay remote\n", slug, files.ImageFailures)
		}
		fmt.Fprintf(errw, "saved %s\n", files.Markdown)
		res.envelope["saved"] = map[string]any{"markdown": files.Markdown, "html": files.HTML, "images": files.Images}
	}
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}

// looksLikeHTML reports whether a response body is an HTML document rather than
// the expected post JSON — the signal that a subdomain 302-redirected to a
// reader-app profile page instead of serving the API. Post JSON always begins
// with '{' (or '['), never '<'.
func looksLikeHTML(raw []byte) bool {
	t := strings.TrimSpace(string(raw))
	return t != "" && t[0] == '<'
}

// previewReport is the exit signal for a batch that came back partly as
// previews. Without a session every paid post is expected to be a preview, so
// nothing is reported; with a session, a post that is still not full means the
// run did not deliver what it was configured to, so the batch says how many and
// which, and fails. Posts that failed outright are not counted: they already
// have their own warning, and a batch where all fail already errors.
func previewReport(results []readResult, sessionConfigured bool) error {
	if !sessionConfigured {
		return nil
	}
	var previews, limited []string
	total := 0
	for _, r := range results {
		if r.err != nil {
			continue
		}
		total++
		if r.access.Full {
			continue
		}
		previews = append(previews, r.meta.Slug)
		var rle *cliutil.RateLimitError
		if errors.As(r.authFetchErr, &rle) {
			limited = append(limited, r.meta.Slug)
		}
	}
	if len(previews) == 0 {
		return nil
	}
	msg := fmt.Sprintf("%d of %d posts are previews although a session is configured: %s",
		len(previews), total, strings.Join(previews, ", "))
	if len(limited) > 0 {
		msg += fmt.Sprintf(" (%d rate limited: %s; run them again later or read fewer posts per call)",
			len(limited), strings.Join(limited, ", "))
		return rateLimitErr(errors.New(msg))
	}
	return previewErr(errors.New(msg))
}

// previewErr exits 4, the auth exit code: the session did not unlock the post.
// Defined here because the chassis emits authErr only for CLIs whose spec
// declares auth, and this spec says auth: none.
func previewErr(err error) error { return &cliError{code: 4, err: err} }
