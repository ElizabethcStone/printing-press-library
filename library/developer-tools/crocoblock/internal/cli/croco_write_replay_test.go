package cli

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"
)

func TestCrocoWritesNeverReplayConfiguredIdempotencyKeys(t *testing.T) {
	cases := []struct {
		name    string
		headers map[string]string
	}{
		{"none", nil},
		{"standard", map[string]string{"Idempotency-Key": "synthetic-standard"}},
		{"x-prefixed", map[string]string{"X-Idempotency-Key": "synthetic-x"}},
		{"both-mixed-case", map[string]string{"idempotency-key": "synthetic-standard", "x-IDEMPOTENCY-key": "synthetic-x"}},
	}
	for _, method := range []string{"POST", "PUT", "DELETE"} {
		for _, tc := range cases {
			t.Run(method+"/"+tc.name, func(t *testing.T) {
				var calls atomic.Int32
				_, c := crocoEngineTestHTTP(t, func(w http.ResponseWriter, r *http.Request) {
					calls.Add(1)
					if r.Method != method {
						t.Errorf("method = %s, want %s", r.Method, method)
					}
					if r.Header.Get("Idempotency-Key") != "" || r.Header.Get("X-Idempotency-Key") != "" {
						t.Error("unsupported configured idempotency key reached WordPress")
					}
					w.WriteHeader(http.StatusInternalServerError)
					_, _ = w.Write([]byte(`{"error":"synthetic failure after commit"}`))
				})
				c.Config.Headers = tc.headers
				_, err := crocoRequest(context.Background(), c, method, "/synthetic-write", nil, map[string]any{"synthetic": true}, false)
				if err == nil {
					t.Fatal("expected ambiguous server failure")
				}
				if calls.Load() != 1 {
					t.Fatalf("write sent %d times; must never replay", calls.Load())
				}
			})
		}
	}
}
