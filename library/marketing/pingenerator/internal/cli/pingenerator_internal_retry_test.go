// Copyright 2026 Elizabeth Stone and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mvanhorn/printing-press-library/library/marketing/pingenerator/internal/pgmcp"
)

func fastInternalRetry(t *testing.T, d time.Duration) {
	t.Helper()
	saved := pgInternalRetryDelay
	pgInternalRetryDelay = d
	t.Cleanup(func() { pgInternalRetryDelay = saved })
}

// A free catalog read that hits the vendor's transient internal error is
// retried once and succeeds: two requests, exit 0. Covers both the
// result.isError shape and the JSON-RPC error object.
func TestReadInternalErrorRetriedOnce(t *testing.T) {
	fastInternalRetry(t, time.Millisecond)
	shapes := map[string]string{
		"isError":        rpcText(pgVendorInternalError, true),
		"json-rpc error": `{"jsonrpc":"2.0","id":1,"error":{"code":-32603,"message":"` + pgVendorInternalError + `"}}`,
	}
	for name, failure := range shapes {
		t.Run(name, func(t *testing.T) {
			var n int32
			counts := scriptedMCP(t, func(req pgmcp.Request) (int, string) {
				if atomic.AddInt32(&n, 1) == 1 {
					return 200, failure
				}
				return 200, rpcText(`{"boards":[{"id":"b1","name":"Fixture"}]}`, false)
			})
			out, stderr, code := runPG(t, "list-boards", "--pinterest-profile", "me", "--json")
			if code != 0 || !strings.Contains(out, `"b1"`) {
				t.Fatalf("exit %d out %s err %s", code, out, stderr)
			}
			if got := callCount(counts, "list_boards"); got != 2 {
				t.Fatalf("requests = %d, want 2", got)
			}
		})
	}
}

// A second internal error is final, and other read failures are never retried.
func TestReadRetryIsBoundedAndNarrow(t *testing.T) {
	fastInternalRetry(t, time.Millisecond)
	cases := []struct {
		name  string
		body  string
		calls int32
		exit  int
	}{
		{"persistent internal error", rpcText(pgVendorInternalError, true), 2, 5},
		{"auth", rpcText("Unauthorized: invalid access key", true), 1, 4},
		{"not found", rpcText("Profile not found", true), 1, 3},
		{"rate limit", rpcText("Rate limit exceeded", true), 1, 7},
		{"other rejection", rpcText("Invalid params", true), 1, 5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			counts := scriptedMCP(t, func(pgmcp.Request) (int, string) { return 200, tc.body })
			_, _, code := runPG(t, "list-boards", "--pinterest-profile", "me", "--json")
			if code != tc.exit || callCount(counts, "list_boards") != tc.calls {
				t.Fatalf("exit %d calls %d, want exit %d calls %d", code, callCount(counts, "list_boards"), tc.exit, tc.calls)
			}
		})
	}
}

// A paid call that returns the same internal error is sent exactly once.
func TestPaidInternalErrorNotRetried(t *testing.T) {
	fastInternalRetry(t, time.Millisecond)
	counts := scriptedMCP(t, func(req pgmcp.Request) (int, string) {
		if req.Params.Name == "get_post_content" {
			return 200, rpcText(`{"title":"Fixture Post","images":["https://example.com/cdn/f.png"]}`, false)
		}
		return 200, rpcText(pgVendorInternalError, true)
	})
	_, _, code := runPG(t, "pin-post", "https://example.com/post-a", "--pinterest-profile", "me", "--board-id", "b1", "--execute")
	if code == 0 || callCount(counts, "generate_and_schedule_pin") != 1 {
		t.Fatalf("exit %d paid requests %d, want non-zero and 1", code, callCount(counts, "generate_and_schedule_pin"))
	}
}

// A free but mutating call is sent exactly once.
func TestMutatingInternalErrorNotRetried(t *testing.T) {
	fastInternalRetry(t, time.Millisecond)
	counts := scriptedMCP(t, func(pgmcp.Request) (int, string) { return 200, rpcText(pgVendorInternalError, true) })
	_, _, code := runPG(t, "setup-design-profile", "--website-url", "https://example.com", "--name", "Fixture")
	if code == 0 || callCount(counts, "setup_design_profile") != 1 {
		t.Fatalf("exit %d requests %d, want non-zero and 1", code, callCount(counts, "setup_design_profile"))
	}
}

// Only known, non-mutating, free catalog tools qualify.
func TestRetryableInternalOnlyForCatalogReads(t *testing.T) {
	internal := &pgmcp.Error{Kind: pgmcp.KindGeneric, Tool: "x", Message: pgVendorInternalError}
	if !pgRetryableInternal("list_boards", internal) {
		t.Fatal("list_boards internal error should qualify")
	}
	for _, name := range []string{"not_a_catalog_tool", "generate_pin_image", "run_automation_batch", "create_automation", "setup_design_profile"} {
		if pgRetryableInternal(name, internal) {
			t.Fatalf("%s must never be retried", name)
		}
	}
	for _, err := range []error{
		&pgmcp.Error{Kind: pgmcp.KindUnreadable, Message: pgVendorInternalError},
		&pgmcp.Error{Kind: pgmcp.KindAuth, Message: pgVendorInternalError},
		&pgmcp.Error{Kind: pgmcp.KindGeneric, Message: "Internal server error"},
		&pgmcp.Error{Kind: pgmcp.KindGeneric, Message: "unexpected: " + pgVendorInternalError},
		&pgmcp.Error{Kind: pgmcp.KindGeneric, Message: strings.ToLower(pgVendorInternalError)},
		errors.New(pgVendorInternalError),
	} {
		if pgRetryableInternal("list_boards", err) {
			t.Fatalf("%v must not qualify", err)
		}
	}
}

// The retry delay respects the command's existing deadline: a timeout during
// the wait ends the call without a second request.
func TestInternalRetryDelayStopsOnDeadline(t *testing.T) {
	fastInternalRetry(t, time.Hour)
	counts := scriptedMCP(t, func(pgmcp.Request) (int, string) { return 200, rpcText(pgVendorInternalError, true) })
	start := time.Now()
	_, _, code := runPG(t, "list-boards", "--pinterest-profile", "me", "--timeout", "300ms")
	if code == 0 || time.Since(start) > 10*time.Second || callCount(counts, "list_boards") != 1 {
		t.Fatalf("exit %d after %s with %d requests, want prompt failure and 1 request", code, time.Since(start), callCount(counts, "list_boards"))
	}
}
