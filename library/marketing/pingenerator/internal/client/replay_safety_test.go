// Copyright 2026 Elizabeth Stone and contributors. Licensed under Apache-2.0. See LICENSE.

package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mvanhorn/printing-press-library/library/marketing/pingenerator/internal/cliutil"
	"github.com/mvanhorn/printing-press-library/library/marketing/pingenerator/internal/config"
)

// flakyServer answers 503 for the first `failures` requests, then 200 {}.
func flakyServer(t *testing.T, failures int32) (*httptest.Server, *int32) {
	t.Helper()
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&hits, 1) <= failures {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

func TestReplayOptOutAndStaticKeyNeverResendPaidCalls(t *testing.T) {
	t.Setenv(cliutil.VerifyEnvVar, "")
	t.Setenv(cliutil.DogfoodEnvVar, "")
	static := map[string]string{"Idempotency-Key": "static-config-key"}
	cases := []struct {
		name    string
		headers map[string]string
	}{
		{"explicit opt-out with static config key", map[string]string{ReplaySafeHeader: "false"}},
		{"undeclared POST with static config key", nil},
		{"explicit opt-out beats a per-request key", map[string]string{ReplaySafeHeader: "false", "Idempotency-Key": "per-request"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, hits := flakyServer(t, 1)
			c := New(&config.Config{BaseURL: srv.URL, Headers: static}, 5*time.Second, 0)
			if _, _, err := c.PostWithHeaders(context.Background(), "/mcp", map[string]any{"paid": true}, tc.headers); err == nil {
				t.Fatal("expected the 503 to surface, not a silent retry")
			}
			if got := atomic.LoadInt32(hits); got != 1 {
				t.Fatalf("paid POST reached the server %d times, want exactly 1", got)
			}
		})
	}
}

func TestGenuineRetriesStillWork(t *testing.T) {
	t.Setenv(cliutil.VerifyEnvVar, "")
	t.Setenv(cliutil.DogfoodEnvVar, "")
	t.Run("read GET retries after 503", func(t *testing.T) {
		srv, hits := flakyServer(t, 1)
		c := New(&config.Config{BaseURL: srv.URL}, 5*time.Second, 0)
		if _, err := c.Get(context.Background(), "/status", nil); err != nil {
			t.Fatal(err)
		}
		if got := atomic.LoadInt32(hits); got != 2 {
			t.Fatalf("GET attempts = %d, want 2", got)
		}
	})
	t.Run("POST with a per-request key retries", func(t *testing.T) {
		srv, hits := flakyServer(t, 1)
		c := New(&config.Config{BaseURL: srv.URL}, 5*time.Second, 0)
		if _, _, err := c.PostWithHeaders(context.Background(), "/items", map[string]any{"a": 1}, map[string]string{"Idempotency-Key": "per-request-unique"}); err != nil {
			t.Fatal(err)
		}
		if got := atomic.LoadInt32(hits); got != 2 {
			t.Fatalf("keyed POST attempts = %d, want 2", got)
		}
	})
}
