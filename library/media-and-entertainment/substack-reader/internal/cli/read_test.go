// Copyright 2026 Maxime Delavergne and contributors. Licensed under Apache-2.0. See LICENSE.
// cli-printing-press: novel-scaffold-test
// Novel command scaffold tests. Keep the wiring smoke test and add behavior cases as needed.

package cli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/mvanhorn/printing-press-library/library/media-and-entertainment/substack-reader/internal/cliutil"
	"github.com/mvanhorn/printing-press-library/library/media-and-entertainment/substack-reader/internal/substack"
)

// TestNovelReadHelpWires smoke-tests that the read command
// resolves at runtime and renders useful --help output. Catches wiring
// regressions (missing AddCommand, panicking RunE on --help, etc.) before
// review. Keep this smoke test when adding behavior-specific cases.
func TestNovelReadHelpWires(t *testing.T) {
	cmd := RootCmd()
	cmd.SetArgs([]string{"read", "--help"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("read --help error = %v (novel command not wired correctly?)", err)
	}
	help := out.String()
	for _, want := range []string{"Usage:", "read"} {
		if !strings.Contains(help, want) {
			t.Fatalf("read --help missing %q in output:\n%s", want, help)
		}
	}
}

// TestReadBatchDryRun smoke-tests the batch surface without the network:
// read accepts several posts in one call.
func TestReadBatchDryRun(t *testing.T) {
	cmd := RootCmd()
	cmd.SetArgs([]string{"read", "pub/slug-one", "pub/slug-two", "--dry-run"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("read multi-arg --dry-run error = %v (variadic args not wired?)", err)
	}
	if !strings.Contains(out.String(), "read") {
		t.Fatalf("dry-run output missing: %s", out.String())
	}
}

// TestRunReadBatchMintsFreshContextPerPost: a batch whose first post consumes
// more than one whole per-post budget must NOT starve the later posts. Each
// fetch must see its own fresh, unexpired context.
func TestRunReadBatchMintsFreshContextPerPost(t *testing.T) {
	const budget = 30 * time.Millisecond
	newCtx := func() (context.Context, context.CancelFunc) {
		return context.WithTimeout(context.Background(), budget)
	}
	fetch := func(ctx context.Context, arg string) readResult {
		if arg == "slow" {
			select {
			case <-ctx.Done():
			case <-time.After(budget * 3):
			}
			return readResult{err: ctx.Err()}
		}
		if err := ctx.Err(); err != nil {
			return readResult{err: fmt.Errorf("inherited an expired deadline: %w", err)}
		}
		return readResult{envelope: map[string]any{"slug": arg}}
	}
	results := runReadBatch([]string{"slow", "second", "third"}, newCtx, fetch)
	if len(results) != 3 {
		t.Fatalf("results = %d, want 3", len(results))
	}
	for i, r := range results[1:] {
		if r.err != nil {
			t.Fatalf("post %d failed after a slow first post: %v — per-post budgets must be independent", i+2, r.err)
		}
	}
}

// fakeSubstack serves canned https responses to a substack.Client, so the read
// pipeline runs end to end without the network. A path maps to a queue of
// replies; the last one repeats.
type fakeSubstack struct {
	t       *testing.T
	replies map[string][]fakeReply
	seen    []string
	onReq   func(url string)
}

type fakeReply struct {
	status     int
	retryAfter string
	body       string
}

func (f *fakeSubstack) RoundTrip(req *http.Request) (*http.Response, error) {
	u := req.URL.String()
	f.seen = append(f.seen, u)
	if f.onReq != nil {
		f.onReq(u)
	}
	q := f.replies[u]
	if len(q) == 0 {
		f.t.Fatalf("unexpected request %s", u)
	}
	r := q[0]
	if len(q) > 1 {
		f.replies[u] = q[1:]
	}
	h := http.Header{}
	if r.retryAfter != "" {
		h.Set("Retry-After", r.retryAfter)
	}
	return &http.Response{
		StatusCode: r.status,
		Header:     h,
		Body:       io.NopCloser(strings.NewReader(r.body)),
		Request:    req,
	}, nil
}

func freePostJSON(slug string) string {
	return fmt.Sprintf(`{"id":%d,"slug":%q,"title":"Post %s","audience":"everyone","wordcount":4,`+
		`"canonical_url":"https://pub.substack.com/p/%s","post_date":"2026-01-02T00:00:00.000Z",`+
		`"body_html":"<p>one two three four</p>"}`, len(slug), slug, slug, slug)
}

// TestReadBatchKeepsRateLimitSlowdownAcrossPosts: the 429 that hits the first
// post must slow the requests for the second one too. That only holds when the
// whole batch shares ONE client pair; a client per post would start the second
// post at full speed again, which is how a fast bulk read ended in previews.
func TestReadBatchKeepsRateLimitSlowdownAcrossPosts(t *testing.T) {
	fake := &fakeSubstack{t: t, replies: map[string][]fakeReply{
		"https://pub.substack.com/api/v1/posts/one": {{status: 429, retryAfter: "1"}, {status: 200, body: freePostJSON("one")}},
		"https://pub.substack.com/api/v1/posts/two": {{status: 200, body: freePostJSON("two")}},
	}}
	clients := newReadClients(substack.Session{})
	clients.anon.SetTransport(fake)
	clients.anon.SetPause(func(context.Context, time.Duration) error { return nil })

	startRate := clients.anon.Rate()
	var rateWhenPostTwoStarted float64
	fake.onReq = func(u string) {
		if strings.HasSuffix(u, "/posts/two") {
			rateWhenPostTwoStarted = clients.anon.Rate()
		}
	}

	cmd := &cobra.Command{}
	cmd.SetErr(&bytes.Buffer{})
	results := runReadBatch([]string{"pub/one", "pub/two"},
		func() (context.Context, context.CancelFunc) { return context.WithCancel(context.Background()) },
		func(ctx context.Context, arg string) readResult {
			return readOnePost(ctx, cmd, arg, substack.Session{}, clients)
		})

	for i, r := range results {
		if r.err != nil {
			t.Fatalf("post %d failed: %v", i+1, r.err)
		}
	}
	if rateWhenPostTwoStarted == 0 || rateWhenPostTwoStarted >= startRate {
		t.Errorf("rate when post two started = %v, want below the starting %v (the slow-down must carry over)", rateWhenPostTwoStarted, startRate)
	}
	if len(fake.seen) != 3 {
		t.Errorf("requests = %v, want 3 (post one, its retry, post two)", fake.seen)
	}
}

func okResult(slug string, full bool) readResult {
	return readResult{
		meta:   substack.PostMeta{Slug: slug},
		access: substack.Access{Full: full, Tier: map[bool]string{true: "entitled", false: "preview"}[full]},
	}
}

// TestPreviewReportNeedsASession: with no session every paid post is expected
// to be a preview, so that is not an error; with a session, a preview means
// something went wrong and the run must say so and fail.
func TestPreviewReportNeedsASession(t *testing.T) {
	results := []readResult{okResult("a", true), okResult("b", false), okResult("c", false)}

	if err := previewReport(results, false); err != nil {
		t.Errorf("no session configured: err = %v, want nil", err)
	}
	if err := previewReport([]readResult{okResult("a", true)}, true); err != nil {
		t.Errorf("all posts full: err = %v, want nil", err)
	}

	err := previewReport(results, true)
	if err == nil {
		t.Fatal("session configured and 2 previews: err = nil, want an error")
	}
	for _, want := range []string{"2 of 3 posts are previews", "b", "c"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("err = %q, missing %q", err, want)
		}
	}
	if code := ExitCode(err); code == 0 {
		t.Errorf("exit code = %d, want non-zero", code)
	}
}

// TestPreviewReportSeparatesRateLimitedPosts: a preview that came from a rate
// limited authenticated fetch is retryable, and says so with the rate-limit
// exit code, so a script can tell it from "not entitled".
func TestPreviewReportSeparatesRateLimitedPosts(t *testing.T) {
	limited := okResult("b", false)
	limited.authFetchErr = &cliutil.RateLimitError{URL: "https://substack.com/api/v1/posts/by-id/9"}

	err := previewReport([]readResult{okResult("a", true), limited}, true)
	if err == nil {
		t.Fatal("err = nil, want an error")
	}
	if !strings.Contains(err.Error(), "rate limited") {
		t.Errorf("err = %q, want it to name the rate limit", err)
	}
	if code := ExitCode(err); code != 7 {
		t.Errorf("exit code = %d, want 7 (rate limited)", code)
	}
}

// TestReadEnvelopeKeepsPlainTextAndAddsRealMarkdown: body_text is the flattened
// text the word count is taken from; body_markdown is a real Markdown copy that
// keeps headings and links.
func TestReadEnvelopeKeepsPlainTextAndAddsRealMarkdown(t *testing.T) {
	post := `{"id":9,"slug":"md","title":"T","audience":"everyone","wordcount":3,` +
		`"canonical_url":"https://pub.substack.com/p/md",` +
		`"body_html":"<h2>Head</h2><p>See <a href=\"https://x.com/a\">this link</a> now</p>"}`
	fake := &fakeSubstack{t: t, replies: map[string][]fakeReply{
		"https://pub.substack.com/api/v1/posts/md": {{status: 200, body: post}},
	}}
	clients := newReadClients(substack.Session{})
	clients.anon.SetTransport(fake)

	cmd := &cobra.Command{}
	cmd.SetErr(&bytes.Buffer{})
	res := readOnePost(context.Background(), cmd, "pub/md", substack.Session{}, clients)
	if res.err != nil {
		t.Fatalf("readOnePost: %v", res.err)
	}

	md, _ := res.envelope["body_markdown"].(string)
	if !strings.Contains(md, "## Head") || !strings.Contains(md, "[this link](https://x.com/a)") {
		t.Errorf("body_markdown is not real Markdown: %q", md)
	}
	text, _ := res.envelope["body_text"].(string)
	if strings.Contains(text, "##") || strings.Contains(text, "](") || !strings.Contains(text, "this link") {
		t.Errorf("body_text should be flat plain text: %q", text)
	}
	if res.envelope["body_html"] != "<h2>Head</h2><p>See <a href=\"https://x.com/a\">this link</a> now</p>" {
		t.Errorf("body_html changed: %v", res.envelope["body_html"])
	}
	if res.rawPost == nil {
		t.Error("the raw post object must be kept for the corpus")
	}
}
