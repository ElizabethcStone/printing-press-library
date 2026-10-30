// Copyright 2026 Elizabeth Stone and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/mvanhorn/printing-press-library/library/marketing/pingenerator/internal/cliutil"
	"github.com/mvanhorn/printing-press-library/library/marketing/pingenerator/internal/cliutil/testenv"
	"github.com/mvanhorn/printing-press-library/library/marketing/pingenerator/internal/pgmcp"
)

// scriptedMCP answers each tools/call through fn and counts calls per tool.
func scriptedMCP(t *testing.T, fn func(req pgmcp.Request) (int, string)) *sync.Map {
	t.Helper()
	counts := &sync.Map{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req pgmcp.Request
		_ = json.Unmarshal(body, &req)
		n, _ := counts.LoadOrStore(req.Params.Name, new(int32))
		atomic.AddInt32(n.(*int32), 1)
		status, out := fn(req)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(out))
	}))
	t.Cleanup(srv.Close)
	testenv.Isolate(t)
	t.Setenv("PINGENERATOR_BASE_URL", srv.URL)
	t.Setenv("PINGENERATOR_ACCESS_KEY", "test-key-not-real")
	t.Setenv("PINGENERATOR_LEDGER", filepath.Join(t.TempDir(), "ledger.jsonl"))
	t.Setenv(cliutil.DogfoodEnvVar, "")
	t.Setenv(cliutil.VerifyEnvVar, "")
	return counts
}

func callCount(m *sync.Map, tool string) int32 {
	if n, ok := m.Load(tool); ok {
		return atomic.LoadInt32(n.(*int32))
	}
	return 0
}

func rpcText(text string, isError bool) string {
	b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "result": map[string]any{"isError": isError, "content": []map[string]any{{"type": "text", "text": text}}}})
	return string(b)
}

// Finding 1: an unreadable reply after a paid call is ambiguous and must be
// ledgered unconfirmed; an explicit rejection is not a spend.
func TestUnreadablePaidReplyIsLedgeredUnconfirmed(t *testing.T) {
	cases := []struct {
		name       string
		body       string
		wantLedger bool
	}{
		{"malformed JSON", `{"jsonrpc":"2.0","result":`, true},
		{"missing result", `{"jsonrpc":"2.0","id":1}`, true},
		{"explicit tool rejection", rpcText("Template not found", true), false},
		{"explicit JSON-RPC error", `{"jsonrpc":"2.0","id":1,"error":{"code":-32602,"message":"Invalid params"}}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			counts := scriptedMCP(t, func(req pgmcp.Request) (int, string) {
				if req.Params.Name == "get_post_content" {
					return 200, rpcText(`{"title":"Fixture Post","images":["https://example.com/cdn/f.png"]}`, false)
				}
				return 200, tc.body
			})
			_, _, code := runPG(t, "pin-post", "https://example.com/post-a", "--pinterest-profile", "me", "--board-id", "b1", "--execute")
			if code == 0 {
				t.Fatal("failed paid call must exit non-zero")
			}
			entries, _, _ := pgmcp.ReadLedger(os.Getenv("PINGENERATOR_LEDGER"))
			if got := len(entries) == 1 && entries[0].Status == pgmcp.StatusUnconfirmed; got != tc.wantLedger {
				t.Fatalf("unconfirmed ledger entry = %v, want %v (%+v)", got, tc.wantLedger, entries)
			}
			if tc.wantLedger {
				out, _, code := runPG(t, "pin-post", "https://example.com/post-a", "--pinterest-profile", "me", "--board-id", "b1", "--execute", "--json")
				if code != 0 || !strings.Contains(out, `"skipped": true`) || callCount(counts, "generate_and_schedule_pin") != 1 {
					t.Fatalf("rerun must not charge again: exit %d calls %d %s", code, callCount(counts, "generate_and_schedule_pin"), out)
				}
			}
		})
	}
}

// Finding 2: overlapping spenders for the same URL make exactly one paid call.
func TestConcurrentSpendsForSameURLPayOnce(t *testing.T) {
	counts := scriptedMCP(t, func(req pgmcp.Request) (int, string) {
		time.Sleep(150 * time.Millisecond) // widen the race window
		return 200, rpcText(`{"pinId":"p1"}`, false)
	})
	tool := mustTool("generate_and_schedule_pin")
	const workers = 6
	var paid, skipped int32
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cmd := &cobra.Command{Use: "pin-post"}
			cmd.SetContext(context.Background())
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			args := map[string]any{"template": "S", "title": "T", "url": "https://example.com/post-a", "pinterestProfile": "me", "boardId": "b1"}
			_, _, skip, err := pgSpendLocked(cmd, &rootFlags{timeout: 10 * time.Second}, tool, args, "https://example.com/post-a/", false)
			if err != nil {
				t.Error(err)
				return
			}
			if skip {
				atomic.AddInt32(&skipped, 1)
			} else {
				atomic.AddInt32(&paid, 1)
			}
		}()
	}
	wg.Wait()
	if paid != 1 || skipped != workers-1 || callCount(counts, "generate_and_schedule_pin") != 1 {
		t.Fatalf("paid=%d skipped=%d server paid calls=%d; want exactly one paid call", paid, skipped, callCount(counts, "generate_and_schedule_pin"))
	}
}

// The cross-process half: separate OS processes contend on the ledger lock.
func TestConcurrentProcessesForSameURLPayOnce(t *testing.T) {
	counts := scriptedMCP(t, func(req pgmcp.Request) (int, string) {
		time.Sleep(200 * time.Millisecond)
		return 200, rpcText(`{"pinId":"p1"}`, false)
	})
	const procs = 4
	var wg sync.WaitGroup
	outs := make([]string, procs)
	for i := 0; i < procs; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			c := exec.Command(os.Args[0], "-test.run=^TestPGSpendHelperProcess$", "-test.count=1")
			c.Env = append(os.Environ(), "PG_SPEND_HELPER=1")
			out, err := c.CombinedOutput()
			if err != nil {
				t.Errorf("helper %d: %v\n%s", i, err, out)
			}
			outs[i] = string(out)
		}(i)
	}
	wg.Wait()
	joined := strings.Join(outs, "\n")
	if n := callCount(counts, "generate_and_schedule_pin"); n != 1 || strings.Count(joined, "PG_HELPER_PAID") != 1 || strings.Count(joined, "PG_HELPER_SKIPPED") != procs-1 {
		t.Fatalf("server paid calls=%d; helper output:\n%s", n, joined)
	}
}

// TestPGSpendHelperProcess is a child process for the cross-process test.
func TestPGSpendHelperProcess(t *testing.T) {
	if os.Getenv("PG_SPEND_HELPER") != "1" {
		t.Skip("helper process only")
	}
	cmd := &cobra.Command{Use: "pin-post"}
	cmd.SetContext(context.Background())
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	args := map[string]any{"template": "S", "title": "T", "url": "https://example.com/post-a", "pinterestProfile": "me", "boardId": "b1"}
	_, _, skip, err := pgSpendLocked(cmd, &rootFlags{timeout: 10 * time.Second}, mustTool("generate_and_schedule_pin"), args, "https://example.com/post-a", false)
	if err != nil {
		t.Fatal(err)
	}
	if skip {
		fmt.Println("PG_HELPER_SKIPPED")
	} else {
		fmt.Println("PG_HELPER_PAID")
	}
}

// Finding 3: a pin with a different known URL must not mark a same-titled
// post as pinned; title only matches pins that have no URL.
func TestUnpinnedTitleFallbackOnlyForURLlessPins(t *testing.T) {
	scriptedMCP(t, func(req pgmcp.Request) (int, string) {
		switch req.Params.Name {
		case "find_website_posts":
			return 200, rpcText(`{"posts":[{"url":"https://example.com/a","title":"Same Title"},{"url":"https://example.com/b","title":"No Link Pin"}]}`, false)
		default:
			return 200, rpcText(`{"scheduledPins":[{"title":"Same Title","url":"https://example.com/elsewhere"},{"title":"No Link Pin"}],"queuedPinsAwaitingTimeslot":[]}`, false)
		}
	})
	out, stderr, code := runPG(t, "unpinned-posts", "--website-url", "https://example.com", "--json")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	var v unpinnedPostsView
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatal(err)
	}
	if len(v.Unpinned) != 1 || v.Unpinned[0].URL != "https://example.com/a" || v.MatchedByTitle != 1 {
		t.Fatalf("same title with a different URL was treated as pinned: %+v", v)
	}
}

// Finding 4: a page-2 read failure keeps page-1 results but exits non-zero
// with the read error's code and shows it in the human report.
func TestPinProductsLaterPageFailureExitsNonZero(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		code   int
	}{
		{"auth", 200, rpcText("Unauthorized: invalid access key", true), 4},
		{"rate limit", 200, rpcText("Rate limit exceeded", true), 7},
		{"outage", 503, `{"error":"unavailable"}`, 5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			page := func(req pgmcp.Request) (int, string) {
				if p, _ := req.Params.Arguments["page"].(float64); p >= 2 {
					return tc.status, tc.body
				}
				return 200, rpcText(`{"products":[{"id":"1","title":"Fixture Product","url":"https://example.com/shop/p/1"}],"hasMore":true}`, false)
			}
			scriptedMCP(t, page)
			out, _, code := runPG(t, "pin-products", "--store-id", "s1", "--pinterest-profile", "me", "--board-id", "b1", "--max-scan-pages", "3", "--json")
			var v pinProductsView
			if err := json.Unmarshal([]byte(out), &v); err != nil {
				t.Fatalf("report not printed: %v %s", err, out)
			}
			if code != tc.code || v.Planned != 1 || len(v.FetchFailures) != 1 {
				t.Fatalf("exit %d want %d; planned %d failures %d", code, tc.code, v.Planned, len(v.FetchFailures))
			}
			t.Cleanup(func() { humanFriendly = false })
			human, _, hcode := runPG(t, "pin-products", "--store-id", "s1", "--pinterest-profile", "me", "--board-id", "b1", "--max-scan-pages", "3", "--human-friendly")
			if hcode != tc.code || !strings.Contains(human, "INCOMPLETE") {
				t.Fatalf("human report hid the failure: exit %d\n%s", hcode, human)
			}
		})
	}
}

func TestSpendLockWaitRespectsDeadline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.jsonl")
	t.Setenv("PINGENERATOR_LEDGER", path)
	held, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() { done <- cliutil.WithFileLock(path, func() error { close(held); <-release; return nil }) }()
	<-held
	defer func() {
		close(release)
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	start := time.Now()
	_, _, _, err := pgSpendLocked(cmd, &rootFlags{timeout: 30 * time.Millisecond}, mustTool("generate_and_schedule_pin"), nil, "https://example.com/post", false)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v, want deadline", err)
	}
	if time.Since(start) > time.Second {
		t.Fatal("lock wait ignored deadline")
	}
}
