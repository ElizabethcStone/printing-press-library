// Copyright 2026 Maxime Delavergne and contributors. Licensed under Apache-2.0. See LICENSE.

package substack

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mvanhorn/printing-press-library/library/media-and-entertainment/substack-reader/internal/cliutil"
)

// TestGetCapsOversizedBody proves the success-path read is size-capped: a body
// larger than the client's cap returns an error instead of being read whole
// into the heap. Regression for the Greptile P2 "success-path io.ReadAll has no
// size cap" finding — the error paths already used io.LimitReader, but the 2xx
// path read the whole body, so an oversized (or redirected) response could
// balloon the heap.
func TestGetCapsOversizedBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(make([]byte, 4096)) // well over the tiny cap set below
	}))
	defer srv.Close()

	c := NewClient()
	c.maxBytes = 1024 // shrink the 50 MB production cap so the test stays cheap

	if _, err := c.get(context.Background(), srv.URL); err == nil {
		t.Fatal("expected an error when the response body exceeds the cap, got nil")
	} else if !strings.Contains(err.Error(), "exceeds") {
		t.Errorf("expected a size-cap error mentioning 'exceeds', got: %v", err)
	}
}

// TestGetReadsBodyWithinCap confirms the cap does not truncate a normal body:
// a response comfortably under the cap is returned verbatim.
func TestGetReadsBodyWithinCap(t *testing.T) {
	const body = `[{"id":1,"slug":"hello"}]`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	c := NewClient() // default 50 MB cap
	got, err := c.get(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if string(got) != body {
		t.Errorf("body = %q, want %q", got, body)
	}
}

// rateLimitedServer answers the first `limited` requests with 429 (carrying
// retryAfter as a Retry-After header when non-empty) and every later request
// with body. calls counts every request it saw.
func rateLimitedServer(t *testing.T, limited int32, retryAfter, body string) (*httptest.Server, *int32) {
	t.Helper()
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n <= limited {
			if retryAfter != "" {
				w.Header().Set("Retry-After", retryAfter)
			}
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte("slow down"))
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

// fastClient is a Tier-0 client whose limiter starts fast, so the real pacing
// delay (up to 2 s after a 429) does not slow the tests down. The 429 slow-down
// still halves whatever the starting rate is.
func fastClient() *Client {
	c := NewClient()
	c.limiter = cliutil.NewAdaptiveLimiter(1000)
	return c
}

// recordPauses swaps the client's sleep for one that records the requested
// pause and returns at once, so retry tests do not wait in real time.
func recordPauses(c *Client) *[]time.Duration {
	var got []time.Duration
	c.SetPause(func(_ context.Context, d time.Duration) error {
		got = append(got, d)
		return nil
	})
	return &got
}

// TestGetRetries429AfterPause: a 429 is not the end of the request. The client
// pauses for the Retry-After the server sent and asks again, and the caller
// gets the post instead of a rate-limit error.
func TestGetRetries429AfterPause(t *testing.T) {
	const body = `{"slug":"hello"}`
	srv, calls := rateLimitedServer(t, 2, "7", body)
	c := fastClient()
	pauses := recordPauses(c)

	got, err := c.get(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("get after two 429s: %v", err)
	}
	if string(got) != body {
		t.Errorf("body = %q, want %q", got, body)
	}
	if n := atomic.LoadInt32(calls); n != 3 {
		t.Errorf("requests = %d, want 3 (two 429s, then the success)", n)
	}
	want := []time.Duration{7 * time.Second, 7 * time.Second}
	if len(*pauses) != len(want) || (*pauses)[0] != want[0] || (*pauses)[1] != want[1] {
		t.Errorf("pauses = %v, want %v (the server's Retry-After)", *pauses, want)
	}
}

// TestGetGivesUpAfterMaxRetries: a server that never stops answering 429 must
// still end in a rate-limit error (never empty data), after the bounded number
// of retries, with the pause growing when no Retry-After was sent.
func TestGetGivesUpAfterMaxRetries(t *testing.T) {
	srv, calls := rateLimitedServer(t, 1000, "", "")
	c := fastClient()
	pauses := recordPauses(c)

	_, err := c.get(context.Background(), srv.URL)
	var rle *cliutil.RateLimitError
	if !errors.As(err, &rle) {
		t.Fatalf("err = %v, want a *cliutil.RateLimitError", err)
	}
	if rle.RetryAfter <= 0 {
		t.Errorf("RetryAfter = %v, want the pause that was waited", rle.RetryAfter)
	}
	if n := atomic.LoadInt32(calls); n != 1+maxRateLimitRetries {
		t.Errorf("requests = %d, want %d (first try + %d retries)", n, 1+maxRateLimitRetries, maxRateLimitRetries)
	}
	if len(*pauses) != maxRateLimitRetries {
		t.Fatalf("pauses = %v, want %d", *pauses, maxRateLimitRetries)
	}
	if (*pauses)[1] <= (*pauses)[0] {
		t.Errorf("pauses = %v: without Retry-After the pause should grow", *pauses)
	}
}

// TestGetPauseHonoursContext: waiting out a 429 must not outlive the caller's
// deadline.
func TestGetPauseHonoursContext(t *testing.T) {
	srv, _ := rateLimitedServer(t, 1000, "30", "")
	c := fastClient() // real pause: a 30 s Retry-After would hang if ignored
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := c.get(ctx, srv.URL)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Errorf("get took %v: the pause ignored the context", time.Since(start))
	}
}

// TestRateLimitSlowsTheSameClient is the reason read keeps ONE client pair for
// a whole batch: the slow-down a 429 causes lives in the client's limiter, so a
// later request on the same client starts at the slower pace.
func TestRateLimitSlowsTheSameClient(t *testing.T) {
	srv, _ := rateLimitedServer(t, 1, "1", `{}`)
	c := fastClient()
	recordPauses(c)
	before := c.Rate()

	if _, err := c.get(context.Background(), srv.URL); err != nil {
		t.Fatalf("get: %v", err)
	}
	if after := c.Rate(); after >= before {
		t.Errorf("rate after a 429 = %v, want below the starting %v", after, before)
	}
}

// TestFetchImageAsksForAnImageAndKeepsTheBytes: an image download is a plain
// GET that must not claim to want JSON, returns the bytes untouched, and shares
// the client's 429 handling.
func TestFetchImageAsksForAnImageAndKeepsTheBytes(t *testing.T) {
	png := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a, 1, 2, 3}
	var accept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		accept = r.Header.Get("Accept")
		_, _ = w.Write(png)
	}))
	defer srv.Close()

	got, err := fastClient().FetchImage(context.Background(), srv.URL+"/pic.png")
	if err != nil {
		t.Fatalf("FetchImage: %v", err)
	}
	if string(got) != string(png) {
		t.Errorf("bytes = %v, want %v", got, png)
	}
	if strings.Contains(accept, "json") || !strings.Contains(accept, "image/") {
		t.Errorf("Accept = %q, want an image Accept header", accept)
	}
}

// TestGetKeepsTheRateLimitWhenTheWaitIsCutShort: a Retry-After longer than the
// time left ends the request while it waits. The caller must still get the
// RateLimitError (so read exits 7, "try later"), with the context error
// inside, not a bare timeout that reads as a session problem (exit 4).
// Greptile P2 on printing-press-library PR #2393.
func TestGetKeepsTheRateLimitWhenTheWaitIsCutShort(t *testing.T) {
	srv, _ := rateLimitedServer(t, 1000, "60", "")
	c := fastClient()
	c.SetPause(func(context.Context, time.Duration) error { return context.DeadlineExceeded })

	_, err := c.get(context.Background(), srv.URL)
	var rle *cliutil.RateLimitError
	if !errors.As(err, &rle) {
		t.Fatalf("err = %v, want a *cliutil.RateLimitError", err)
	}
	if rle.RetryAfter != 60*time.Second {
		t.Errorf("RetryAfter = %v, want the server's 60s", rle.RetryAfter)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want the deadline kept inside", err)
	}
}

// TestGetKeepsTheRateLimitWhenTheRetryIsCanceled: the wait itself finished,
// but the context ended before the retry could be sent. Still a rate limit.
func TestGetKeepsTheRateLimitWhenTheRetryIsCanceled(t *testing.T) {
	srv, _ := rateLimitedServer(t, 1000, "5", "")
	c := fastClient()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c.SetPause(func(context.Context, time.Duration) error { cancel(); return nil })

	_, err := c.get(ctx, srv.URL)
	var rle *cliutil.RateLimitError
	if !errors.As(err, &rle) {
		t.Fatalf("err = %v, want a *cliutil.RateLimitError", err)
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want the cancellation kept inside", err)
	}
}
