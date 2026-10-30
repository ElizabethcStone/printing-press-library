// Copyright 2026 Elizabeth Stone and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/mvanhorn/printing-press-library/library/marketing/pingenerator/internal/client"
	"github.com/mvanhorn/printing-press-library/library/marketing/pingenerator/internal/cliutil"
	"github.com/mvanhorn/printing-press-library/library/marketing/pingenerator/internal/cliutil/testenv"
	"github.com/mvanhorn/printing-press-library/library/marketing/pingenerator/internal/pgmcp"
	"github.com/mvanhorn/printing-press-library/library/marketing/pingenerator/internal/platform"
)

// mockMCP is an offline stand-in for the Pin Generator MCP endpoint. It
// records every tools/call and answers from a per-tool reply table.
type mockMCP struct {
	t       *testing.T
	mu      sync.Mutex
	calls   []mockCall
	replies map[string]string // tool -> content text (JSON) or "!error text"; "" key answers non-tools/call methods
}

type mockCall struct {
	Path, Auth, Accept string
	Idempotency        string
	Req                pgmcp.Request
}

func newMockMCP(t *testing.T, replies map[string]string) *mockMCP {
	t.Helper()
	m := &mockMCP{t: t, replies: replies}
	srv := httptest.NewServer(http.HandlerFunc(m.serve))
	t.Cleanup(srv.Close)
	testenv.Isolate(t)
	t.Setenv("PINGENERATOR_BASE_URL", srv.URL)
	t.Setenv("PINGENERATOR_ACCESS_KEY", "test-key-not-real")
	t.Setenv("PINGENERATOR_LEDGER", filepath.Join(t.TempDir(), "ledger.jsonl"))
	return m
}

func (m *mockMCP) serve(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	var req pgmcp.Request
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	m.mu.Lock()
	m.calls = append(m.calls, mockCall{Path: r.URL.Path, Auth: r.Header.Get("Authorization"), Accept: r.Header.Get("Accept"), Idempotency: r.Header.Get("Idempotency-Key"), Req: req})
	reply, ok := m.replies[req.Params.Name]
	if req.Method != "tools/call" {
		reply, ok = m.replies[""]
	}
	m.mu.Unlock()
	if strings.HasPrefix(reply, "HTTP ") {
		code := map[string]int{"HTTP 401": 401, "HTTP 402": 402, "HTTP 503": 503}[reply]
		w.WriteHeader(code)
		_, _ = w.Write([]byte(`{"error":"denied"}`))
		return
	}
	result := map[string]any{"content": []map[string]any{{"type": "text", "text": reply}}}
	if !ok {
		result = map[string]any{"isError": true, "content": []map[string]any{{"type": "text", "text": "unknown tool " + req.Params.Name}}}
	} else if strings.HasPrefix(reply, "!") {
		result = map[string]any{"isError": true, "content": []map[string]any{{"type": "text", "text": strings.TrimPrefix(reply, "!")}}}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
}

func (m *mockMCP) toolCalls(name string) []mockCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []mockCall
	for _, c := range m.calls {
		if name == "" || c.Req.Params.Name == name {
			out = append(out, c)
		}
	}
	return out
}

func runPG(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	cmd := RootCmd()
	var out, errb bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errb)
	cmd.SetArgs(args)
	err := cmd.Execute()
	code := 0
	if err != nil {
		code = ExitCode(err)
		errb.WriteString(err.Error())
	}
	return out.String(), errb.String(), code
}

func TestToolCallWireContract(t *testing.T) {
	m := newMockMCP(t, map[string]string{"list_boards": `{"boards":[{"id":"b1","name":"Beaches"}]}`})
	out, stderr, code := runPG(t, "list-boards", "--pinterest-profile", "me", "--json")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if !strings.Contains(out, `"b1"`) {
		t.Fatalf("payload not unwrapped: %s", out)
	}
	calls := m.toolCalls("list_boards")
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	c := calls[0]
	if c.Path != "/mcp" || c.Auth != "Bearer test-key-not-real" || !strings.Contains(c.Accept, "text/event-stream") {
		t.Fatalf("wire mismatch: %+v", c)
	}
	if c.Req.JSONRPC != "2.0" || c.Req.Method != "tools/call" || c.Req.Params.Arguments["pinterestProfile"] != "me" {
		t.Fatalf("envelope mismatch: %+v", c.Req)
	}
}

func TestCreditToolsNeverDialWithoutExecute(t *testing.T) {
	m := newMockMCP(t, map[string]string{})
	required := map[string][]string{
		"generate-pin-image":           {"--template", "Standard", "--title", "T"},
		"generate-and-schedule-pin":    {"--template", "Standard", "--title", "T", "--pinterest-profile", "me", "--board-id", "b1"},
		"schedule-pin":                 {"--media-url", "https://example.com/x.jpg", "--pinterest-profile", "me", "--board-id", "b1"},
		"generate-ai-pin-image":        {"--title", "T"},
		"generate-and-schedule-ai-pin": {"--title", "T", "--pinterest-profile", "me", "--board-id", "b1"},
		"run-automation-batch":         {"--automation-id", "a1"},
		"create-automation":            {"--name", "x", "--source-type", "manual", "--pinterest-profile", "me", "--first-batch", "now"},
	}
	for _, tl := range pgmcp.Tools {
		if !tl.Cost.Spends() {
			continue
		}
		args, ok := required[tl.Cmd]
		if !ok {
			t.Fatalf("credit tool %s has no test fixture", tl.Cmd)
		}
		out, stderr, code := runPG(t, append([]string{tl.Cmd, "--json"}, args...)...)
		if code != 0 {
			t.Fatalf("%s preview exit %d: %s", tl.Cmd, code, stderr)
		}
		var p pgPreview
		if err := json.Unmarshal([]byte(out), &p); err != nil || !p.DryRun || p.Tool != tl.Name {
			t.Fatalf("%s preview not parseable: %v %s", tl.Cmd, err, out)
		}
	}
	// Free-but-stateful tools preview under --dry-run.
	if _, _, code := runPG(t, "setup-design-profile", "--name", "x", "--dry-run", "--json"); code != 0 {
		t.Fatalf("setup-design-profile --dry-run exit %d", code)
	}
	if n := len(m.toolCalls("")); n != 0 {
		t.Fatalf("previews must not touch the network; saw %d calls", n)
	}
}

func TestErrorExitCodes(t *testing.T) {
	newMockMCP(t, map[string]string{
		"get_account_status":          "!User not found for this access key",
		"generate_pin_image":          "!Not enough credits remaining",
		"list_templates":              "!Rate limit exceeded, slow down",
		"list_automations":            "HTTP 401",
		"list_scheduled_pins":         "HTTP 402",
		"list_connected_stores":       "!internal failure",
		"get_automation_batch_status": "!Batch not found",
		"get_post_content":            "!Could not read URL page (404)",
	})
	cases := []struct {
		args []string
		code int
		want string
	}{
		{[]string{"get-account-status"}, 4, "access key"},
		{[]string{"generate-pin-image", "--template", "S", "--title", "T", "--execute"}, 5, "no credits"},
		{[]string{"list-templates"}, 7, "Rate limit"},
		{[]string{"list-automations"}, 4, "401"},
		{[]string{"list-scheduled-pins"}, 5, "no credits"},
		{[]string{"list-connected-stores"}, 5, "internal failure"},
		{[]string{"get-automation-batch-status", "--batch-id", "b1"}, 3, "Batch not found"},
		{[]string{"get-post-content", "--post-url", "https://example.com/gone"}, 3, "Could not read URL"},
		{[]string{"list-boards", "--json"}, 2, "pinterest-profile"},
		{[]string{"create-automation", "--name", "x", "--source-type", "rss", "--pinterest-profile", "me", "--execute"}, 2, "source-type"},
	}
	for _, tc := range cases {
		_, stderr, code := runPG(t, tc.args...)
		if code != tc.code || !strings.Contains(stderr, tc.want) {
			t.Errorf("%v: exit %d (want %d), stderr %q (want %q)", tc.args, code, tc.code, stderr, tc.want)
		}
	}
}

func TestPriorFlagsStillAccepted(t *testing.T) {
	testenv.Isolate(t)
	root := RootCmd()
	for _, tl := range pgmcp.Tools {
		cmd, _, err := root.Find([]string{tl.Cmd})
		if err != nil || cmd.Name() != tl.Cmd {
			t.Fatalf("command %s not registered", tl.Cmd)
		}
		for _, p := range tl.Params {
			if cmd.Flags().Lookup(p.Flag()) == nil {
				t.Errorf("%s lost flag --%s", tl.Cmd, p.Flag())
			}
		}
		if hasExec := cmd.Flags().Lookup("execute") != nil; hasExec != tl.Cost.Spends() {
			t.Errorf("%s: --execute present=%v, credit tool=%v", tl.Cmd, hasExec, tl.Cost.Spends())
		}
		if ro := cmd.Annotations["mcp:read-only"] == "true"; ro == tl.Mutating {
			t.Errorf("%s: read-only annotation %v but mutating=%v", tl.Cmd, ro, tl.Mutating)
		}
	}
}

func TestExecuteRecordsLedgerAndPinPostSkips(t *testing.T) {
	m := newMockMCP(t, map[string]string{
		"get_post_content":          `{"title":"Best Beaches","description":"Sun","images":["https://example.com/b.jpg"],"url":"https://example.com/best-beaches"}`,
		"generate_and_schedule_pin": `{"pinId":"p1","imageUrl":"https://example.com/cdn/p1.png"}`,
	})
	// Preview: reads the post, never schedules.
	out, stderr, code := runPG(t, "pin-post", "https://example.com/best-beaches", "--pinterest-profile", "me", "--board-id", "b1", "--json")
	if code != 0 {
		t.Fatalf("preview exit %d: %s", code, stderr)
	}
	var v pinPostView
	if err := json.Unmarshal([]byte(out), &v); err != nil || v.Executed || v.Request.Params.Arguments["imageURL"] != "https://example.com/b.jpg" || v.Request.Params.Arguments["title"] != "Best Beaches" {
		t.Fatalf("preview wrong: %v %s", err, out)
	}
	if n := len(m.toolCalls("generate_and_schedule_pin")); n != 0 {
		t.Fatalf("preview scheduled a pin (%d calls)", n)
	}
	// Execute: schedules once and records the spend.
	if _, stderr, code := runPG(t, "pin-post", "https://example.com/best-beaches", "--pinterest-profile", "me", "--board-id", "b1", "--execute", "--json"); code != 0 {
		t.Fatalf("execute exit %d: %s", code, stderr)
	}
	entries, _, err := pgmcp.ReadLedger(os.Getenv("PINGENERATOR_LEDGER"))
	if err != nil || len(entries) != 1 || entries[0].Tool != "generate_and_schedule_pin" || entries[0].Result["pinId"] != "p1" {
		t.Fatalf("ledger = %+v %v", entries, err)
	}
	// Second run skips: already pinned.
	out, _, code = runPG(t, "pin-post", "https://example.com/best-beaches/", "--pinterest-profile", "me", "--board-id", "b1", "--execute", "--json")
	if code != 0 || !strings.Contains(out, `"skipped": true`) {
		t.Fatalf("expected skip, exit %d: %s", code, out)
	}
	if n := len(m.toolCalls("generate_and_schedule_pin")); n != 1 {
		t.Fatalf("double spend: %d schedule calls", n)
	}
	// ledger reports the spend.
	out, _, code = runPG(t, "ledger", "--since", "1d", "--json")
	if code != 0 || !strings.Contains(out, `"credits_max_total": 1`) {
		t.Fatalf("ledger output exit %d: %s", code, out)
	}
}

func TestPinProductsPlansAndPaginates(t *testing.T) {
	m := newMockMCP(t, map[string]string{
		"list_store_products": `{"products":[{"title":"Linen Throw","url":"https://example.com/shop/p/1","images":[{"src":"https://example.com/shop/1.jpg"}]},{"title":"","url":"https://example.com/shop/p/2"}]}`,
	})
	out, stderr, code := runPG(t, "pin-products", "--store-id", "s1", "--pinterest-profile", "me", "--use-ai-board-selection", "--max-scan-pages", "2", "--json")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	var v pinProductsView
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatal(err, out)
	}
	// The mock repeats the same page: the scan stops at the repeat, the
	// titleless product is skipped, and nothing is planned twice.
	if v.Planned != 1 || v.ScannedPages != 2 || v.CreditsMax != 1 || v.Executed != 0 {
		t.Fatalf("plan = %+v", v)
	}
	if len(m.toolCalls("generate_and_schedule_pin")) != 0 {
		t.Fatal("pin-products spent without --execute")
	}
	if got := m.toolCalls("list_store_products")[1].Req.Params.Arguments["page"]; got != float64(2) {
		t.Fatalf("second page arg = %v", got)
	}
}

func TestUnpinnedPostsJoin(t *testing.T) {
	newMockMCP(t, map[string]string{
		"find_website_posts":  `{"posts":[{"url":"https://example.com/a","title":"Post A"},{"url":"https://example.com/b","title":"Post B"},{"url":"https://example.com/c","title":"Post C!"}]}`,
		"list_scheduled_pins": `{"scheduledPins":[{"url":"https://www.example.com/a/?utm_source=pin","title":"x"},{"title":"post c"}]}`,
	})
	out, stderr, code := runPG(t, "unpinned-posts", "--website-url", "https://example.com", "--json")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	var v unpinnedPostsView
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatal(err)
	}
	if len(v.Unpinned) != 1 || v.Unpinned[0].Title != "Post B" || v.MatchedByURL != 1 || v.MatchedByTitle != 1 {
		t.Fatalf("join = %+v", v)
	}
}

func TestQueueGapsBuckets(t *testing.T) {
	start := time.Date(2026, 3, 1, 15, 0, 0, 0, time.UTC)
	pins := []map[string]any{
		{"scheduledTime": "2026-03-01T09:00:00Z", "boardName": "Beaches"},
		{"scheduledTime": "2026-03-01T10:00:00Z", "boardName": "Beaches"},
		{"scheduledTime": "2026-03-01T11:00:00Z", "boardName": "Food"},
		{"scheduledTime": "2026-03-03T09:00:00Z", "boardName": "Food"},
		{"scheduledTime": "2026-04-01T09:00:00Z"},
		{"title": "queued, no time"},
	}
	v := queueGaps(pins, start, 3, 2, "")
	if v.PinsInWindow != 4 || v.PinsWithoutTime != 1 || len(v.EmptyDays) != 1 || v.EmptyDays[0] != "2026-03-02" {
		t.Fatalf("view = %+v", v)
	}
	if len(v.OverloadedDays) != 1 || v.OverloadedDays[0].Count != 3 || v.OverloadedDays[0].Boards["Beaches"] != 2 {
		t.Fatalf("overloaded = %+v", v.OverloadedDays)
	}
	if b := queueGaps(pins, start, 3, 0, "Food"); b.PinsInWindow != 2 {
		t.Fatalf("board filter = %+v", b)
	}
}

func TestCreditRunway(t *testing.T) {
	rem := 140.0
	v := runway([]map[string]any{
		{"name": "Weekly template", "status": "active", "pinsPerDay": 2, "template": "Standard"},
		{"name": "AI brand", "active": true, "pinsPerDay": "1", "designProfileName": "Brand"},
		{"name": "Paused", "status": "paused", "pinsPerDay": 10},
	}, &rem)
	if v.ActiveCount != 2 || v.WeeklyMin != 14+91 || v.WeeklyMax != 14+175 {
		t.Fatalf("burn = %+v", v)
	}
	if v.DaysLeftWorst == nil || *v.DaysLeftWorst != 5.2 || v.DaysLeftBest == nil || *v.DaysLeftBest != 9.3 {
		t.Fatalf("days = %v %v", v.DaysLeftWorst, v.DaysLeftBest)
	}
	if none := runway(nil, &rem); none.ActiveCount != 0 || none.DaysLeftWorst != nil || none.Note == "" {
		t.Fatalf("empty runway = %+v", none)
	}
}

func TestPreflightVerdicts(t *testing.T) {
	newMockMCP(t, map[string]string{
		"get_account_status":      `{"email":"x@example.com","creditLimit":100,"creditsUsed":80}`,
		"list_pinterest_profiles": `{"profiles":[{"username":"me"}]}`,
		"list_boards":             `{"boards":[{"id":"b1","name":"Beaches"}]}`,
		"list_design_profiles":    `{"designProfiles":[{"name":"Brand"}]}`,
	})
	out, stderr, code := runPG(t, "preflight", "--pinterest-profile", "me", "--board-id", "b1", "--tool", "generate-and-schedule-ai-pin", "--json")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	var v preflightView
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatal(err)
	}
	if v.Verdict != "check" || v.CreditsMin != 13 || v.CreditsMax != 25 || v.CreditsRemaining == nil || *v.CreditsRemaining != 20 {
		t.Fatalf("ai plan = %+v", v)
	}
	out, _, _ = runPG(t, "preflight", "--pinterest-profile", "me", "--board-id", "nope", "--design-profile-name", "brand", "--json")
	_ = json.Unmarshal([]byte(out), &v)
	if v.Verdict != "blocked" {
		t.Fatalf("missing board should block: %+v", v)
	}
	if _, _, code := runPG(t, "preflight", "--tool", "not-a-tool"); code != 2 {
		t.Fatalf("unknown tool exit %d", code)
	}
}

func TestAmbiguousSpendIsLedgeredUnconfirmed(t *testing.T) {
	m := newMockMCP(t, map[string]string{
		"get_post_content":          `{"title":"Best Beaches","images":["https://example.com/b.jpg"]}`,
		"generate_and_schedule_pin": "HTTP 503",
		"generate_pin_image":        "!Template not found",
	})
	_, stderr, code := runPG(t, "pin-post", "https://example.com/best-beaches", "--pinterest-profile", "me", "--board-id", "b1", "--execute")
	if code == 0 || !strings.Contains(stderr, "unconfirmed") {
		t.Fatalf("503 spend: exit %d, stderr %q", code, stderr)
	}
	entries, _, _ := pgmcp.ReadLedger(os.Getenv("PINGENERATOR_LEDGER"))
	if len(entries) != 1 || entries[0].Status != pgmcp.StatusUnconfirmed {
		t.Fatalf("ledger = %+v", entries)
	}
	// A retry must not double-spend while the first call is unconfirmed.
	out, _, code := runPG(t, "pin-post", "https://example.com/best-beaches", "--pinterest-profile", "me", "--board-id", "b1", "--execute", "--json")
	if code != 0 || !strings.Contains(out, `"skipped": true`) || len(m.toolCalls("generate_and_schedule_pin")) != 1 {
		t.Fatalf("retry was not skipped: exit %d %s", code, out)
	}
	// A server-side rejection is not a spend.
	if _, _, code := runPG(t, "generate-pin-image", "--template", "Nope", "--title", "T", "--execute"); code != 3 {
		t.Fatalf("rejection exit %d", code)
	}
	if entries, _, _ = pgmcp.ReadLedger(os.Getenv("PINGENERATOR_LEDGER")); len(entries) != 1 {
		t.Fatalf("rejected call was ledgered: %+v", entries)
	}
	out, _, _ = runPG(t, "ledger", "--json")
	if !strings.Contains(out, `"unconfirmed": 1`) {
		t.Fatalf("ledger view: %s", out)
	}
}

func TestSpendTimeoutFloor(t *testing.T) {
	newMockMCP(t, map[string]string{"generate_pin_image": `{"imageUrl":"https://example.com/cdn/x.png"}`})
	tool, _ := pgmcp.Lookup("generate_pin_image")
	args := map[string]any{"template": "Standard", "title": "T"}
	for _, tc := range []struct {
		explicit bool
		want     time.Duration
	}{{false, pgSpendTimeout}, {true, time.Minute}} {
		flags := &rootFlags{timeout: time.Minute, timeoutExplicit: tc.explicit}
		cmd := RootCmd()
		cmd.SetOut(io.Discard)
		cmd.SetContext(context.Background())
		if _, _, err := pgRunTool(cmd, flags, tool, args, true); err != nil {
			t.Fatal(err)
		}
		if flags.timeout != tc.want {
			t.Errorf("explicit=%v: timeout %s, want %s", tc.explicit, flags.timeout, tc.want)
		}
	}
}

func TestPassthroughCannotSpendCredits(t *testing.T) {
	m := newMockMCP(t, map[string]string{
		"":            `{"tools":[]}`,
		"list_boards": `{"boards":[]}`,
		"new_tool":    `{"ok":true}`,
	})
	call := func(params string, extra ...string) (string, int) {
		args := append([]string{"mcp", "--jsonrpc", "2.0", "--id", "1", "--method", "tools/call", "--params", params}, extra...)
		_, stderr, code := runPG(t, args...)
		return stderr, code
	}
	if stderr, code := call(`{"name":"generate_and_schedule_ai_pin","arguments":{"title":"T"}}`); code != 2 || !strings.Contains(stderr, "generate-and-schedule-ai-pin --execute") {
		t.Fatalf("spending passthrough: exit %d %q", code, stderr)
	}
	if stderr, code := call(`{"name":"new_tool","arguments":{}}`); code != 2 || !strings.Contains(stderr, "allow-unknown-tool") {
		t.Fatalf("unknown passthrough: exit %d %q", code, stderr)
	}
	if n := len(m.toolCalls("")); n != 0 {
		t.Fatalf("refused calls reached the server: %d", n)
	}
	if _, code := call(`{"name":"list_boards","arguments":{"pinterestProfile":"me"}}`); code != 0 {
		t.Fatalf("free passthrough exit %d", code)
	}
	if _, code := call(`{"name":"new_tool","arguments":{}}`, "--allow-unknown-tool"); code != 0 {
		t.Fatalf("allowed unknown exit %d", code)
	}
	if _, _, code := runPG(t, "mcp", "--jsonrpc", "2.0", "--id", "1", "--method", "tools/list"); code != 0 {
		t.Fatalf("tools/list exit %d", code)
	}
	if n := len(m.toolCalls("")); n != 3 {
		t.Fatalf("expected 3 forwarded calls, got %d", n)
	}
}

// liveAccountStatus mirrors the get_account_status field names confirmed
// live on 2026-10-29. Values are synthetic.
const liveAccountStatus = `{"accountEmail":"someone@example.com","usedPinCredits":12,"pinCreditLimit":100,"remainingPinCredits":88,"connectedPinterestProfiles":2}`

func TestCreditsRemainingLiveSchema(t *testing.T) {
	cases := []struct {
		name string
		body string
		want float64
		ok   bool
	}{
		{"live remaining field", liveAccountStatus, 88, true},
		{"zero balance is real", `{"usedPinCredits":100,"pinCreditLimit":100,"remainingPinCredits":0}`, 0, true},
		{"limit minus used fallback", `{"usedPinCredits":30,"pinCreditLimit":100}`, 70, true},
		{"overspent clamps to zero", `{"usedPinCredits":130,"pinCreditLimit":100}`, 0, true},
		{"numeric strings", `{"remainingPinCredits":"7"}`, 7, true},
		{"limit alone is not a balance", `{"pinCreditLimit":100,"connectedPinterestProfiles":2}`, 0, false},
	}
	for _, tc := range cases {
		got, ok := creditsRemaining(pgmcp.Object(json.RawMessage(tc.body)))
		if ok != tc.ok || got != tc.want {
			t.Errorf("%s: got %v,%v want %v,%v", tc.name, got, ok, tc.want, tc.ok)
		}
	}
}

func TestBalanceCommandsUseLiveSchema(t *testing.T) {
	newMockMCP(t, map[string]string{
		"get_account_status":      liveAccountStatus,
		"list_pinterest_profiles": `{"connectedPinterestProfiles":[{"username":"me"}],"other":[{"username":"not-me"}]}`,
		"list_boards":             `{"boards":[{"id":"b1"}]}`,
		"list_automations":        `{"automations":[{"name":"Daily","status":"active","pinsPerDay":2,"template":"Standard"}]}`,
	})
	out, stderr, code := runPG(t, "preflight", "--pinterest-profile", "me", "--board-id", "b1", "--count", "5", "--json")
	if code != 0 {
		t.Fatalf("preflight exit %d: %s", code, stderr)
	}
	var pv preflightView
	if err := json.Unmarshal([]byte(out), &pv); err != nil {
		t.Fatal(err)
	}
	if pv.CreditsRemaining == nil || *pv.CreditsRemaining != 88 || pv.Verdict != "ready" {
		t.Fatalf("preflight = %+v", pv)
	}
	out, stderr, code = runPG(t, "credit-runway", "--json")
	if code != 0 {
		t.Fatalf("credit-runway exit %d: %s", code, stderr)
	}
	var rv creditRunwayView
	if err := json.Unmarshal([]byte(out), &rv); err != nil {
		t.Fatal(err)
	}
	if rv.CreditsRemaining == nil || *rv.CreditsRemaining != 88 || rv.DaysLeftWorst == nil || *rv.DaysLeftWorst != 44 {
		t.Fatalf("credit-runway = %+v", rv)
	}
}

func TestPreflightBlocksOnZeroLiveBalance(t *testing.T) {
	newMockMCP(t, map[string]string{
		"get_account_status": `{"accountEmail":"someone@example.com","usedPinCredits":100,"pinCreditLimit":100,"remainingPinCredits":0,"connectedPinterestProfiles":1}`,
	})
	out, _, code := runPG(t, "preflight", "--json")
	var pv preflightView
	_ = json.Unmarshal([]byte(out), &pv)
	if code != 0 || pv.Verdict != "blocked" || pv.CreditsRemaining == nil || *pv.CreditsRemaining != 0 {
		t.Fatalf("zero balance: exit %d %+v", code, pv)
	}
}

// liveScheduledPins builds a list_scheduled_pins reply in the shape confirmed
// live on 2026-10-29. Values are synthetic.
func liveScheduledPins(t *testing.T, now time.Time) string {
	t.Helper()
	day := func(d int) string {
		return time.Date(now.Year(), now.Month(), now.Day(), 15, 0, 0, 0, time.UTC).AddDate(0, 0, d).Format(time.RFC3339)
	}
	body := map[string]any{
		"scheduledPins": []map[string]any{
			{"id": "p1", "title": "Post A", "pinterestProfile": "me", "board": "Beaches", "type": "template", "status": "scheduled", "scheduledTimeUTC": day(0), "url": "https://example.com/a", "imageURL": "https://example.com/cdn/a.png"},
			{"id": "p2", "title": "Post A again", "pinterestProfile": "me", "board": "Beaches", "type": "template", "status": "scheduled", "scheduledTimeUTC": day(0), "url": "https://example.com/a", "imageURL": "https://example.com/cdn/a2.png"},
			{"id": "p3", "title": "Post B", "pinterestProfile": "me", "board": "Food", "type": "ai", "status": "scheduled", "scheduledTimeUTC": day(2), "url": "https://example.com/b", "imageURL": "https://example.com/cdn/b.png"},
		},
		"queuedPinsAwaitingTimeslot": []map[string]any{
			{"id": "q1", "title": "Post C", "pinterestProfile": "me", "board": "Food", "type": "template", "status": "queued", "url": "https://example.com/c", "imageURL": "https://example.com/cdn/c.png"},
		},
	}
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestQueueGapsLiveSchema(t *testing.T) {
	newMockMCP(t, map[string]string{"list_scheduled_pins": liveScheduledPins(t, time.Now().UTC())})
	out, stderr, code := runPG(t, "queue-gaps", "--days", "3", "--per-day", "1", "--json")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	var v queueGapsView
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatal(err, out)
	}
	if v.PinsSeen != 3 || v.PinsInWindow != 3 || v.PinsWithoutTime != 0 || v.QueuedNoTime != 1 {
		t.Fatalf("live pins not bucketed: %+v", v)
	}
	if len(v.ByDay) != 3 || v.ByDay[0].Count != 2 || v.ByDay[0].Boards["Beaches"] != 2 || v.ByDay[1].Count != 0 || v.ByDay[2].Boards["Food"] != 1 {
		t.Fatalf("by day = %+v", v.ByDay)
	}
	if len(v.EmptyDays) != 1 || len(v.OverloadedDays) != 1 || v.OverloadedDays[0].Count != 2 {
		t.Fatalf("gaps = empty %v overloaded %+v", v.EmptyDays, v.OverloadedDays)
	}
	if b := queueGaps(pgmcp.ListAt(json.RawMessage(liveScheduledPins(t, time.Now().UTC())), "scheduledPins"), time.Now().UTC(), 3, 0, "Food"); b.PinsInWindow != 1 {
		t.Fatalf("board filter on live shape = %+v", b)
	}
}

func TestUnpinnedPostsCountsQueuedPins(t *testing.T) {
	newMockMCP(t, map[string]string{
		"find_website_posts":  `{"posts":[{"url":"https://example.com/a","title":"Post A"},{"url":"https://example.com/c","title":"Post C"},{"url":"https://example.com/d","title":"Post D"}]}`,
		"list_scheduled_pins": liveScheduledPins(t, time.Now().UTC()),
	})
	out, stderr, code := runPG(t, "unpinned-posts", "--website-url", "https://example.com", "--json")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	var v unpinnedPostsView
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatal(err)
	}
	if v.ScheduledPins != 3 || v.QueuedPins != 1 || len(v.Unpinned) != 1 || v.Unpinned[0].Title != "Post D" {
		t.Fatalf("queued pin not counted as pinned: %+v", v)
	}
}

func TestPreflightLiveProfileShape(t *testing.T) {
	newMockMCP(t, map[string]string{
		"get_account_status":      liveAccountStatus,
		"list_pinterest_profiles": `{"profiles":[{"username":"me","id":"111","isActive":true},{"username":"old","id":"222","isActive":false}]}`,
	})
	check := func(profile string) preflightView {
		out, stderr, code := runPG(t, "preflight", "--pinterest-profile", profile, "--json")
		if code != 0 {
			t.Fatalf("exit %d: %s", code, stderr)
		}
		var v preflightView
		if err := json.Unmarshal([]byte(out), &v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	if v := check("me"); v.Verdict != "ready" {
		t.Fatalf("active profile: %+v", v)
	}
	if v := check("old"); v.Verdict != "check" {
		t.Fatalf("inactive profile should warn: %+v", v)
	}
	if v := check("nobody"); v.Verdict != "blocked" {
		t.Fatalf("missing profile should block: %+v", v)
	}
}

func TestListScheduledPinsLegacyShapeStillWorks(t *testing.T) {
	s, q := scheduledAndQueued(json.RawMessage(`{"pins":[{"scheduledTime":"2026-03-01T09:00:00Z"}]}`))
	if len(s) != 1 || q != nil {
		t.Fatalf("legacy fallback: %v %v", s, q)
	}
	if got, ok := pgmcp.Time(map[string]any{"scheduledTimeUTC": "2026-03-01 09:30"}, "scheduledTimeUTC"); !ok || got.Hour() != 9 || got.Minute() != 30 {
		t.Fatalf("minute-precision UTC time = %v %v", got, ok)
	}
}

// TestLocalDataPurgeDryRunEnvelope guards the live-dogfood dry-run JSON
// contract (dry_run:true plus a non-empty action). The fix lives in the
// generated local_data.go; a regen that drops it fails here.
func TestLocalDataPurgeDryRunEnvelope(t *testing.T) {
	testenv.Isolate(t)
	out, stderr, code := runPG(t, "local-data", "purge", "--dry-run", "--json")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	var env map[string]any
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if env["dry_run"] != true || strings.TrimSpace(fmt.Sprint(env["action"])) == "" || env["action"] == nil {
		t.Fatalf("dry-run envelope missing dry_run/action: %s", out)
	}
}

func TestPassthroughRejectsCaseVariantName(t *testing.T) {
	m := newMockMCP(t, map[string]string{"list_boards": `{"boards":[]}`})
	_, stderr, code := runPG(t, "mcp", "--method", "tools/call", "--params", `{"name":"generate_and_schedule_ai_pin","Name":"list_boards","arguments":{}}`)
	if code != 2 || len(m.toolCalls("")) != 0 {
		t.Fatalf("case-variant name: exit %d, calls %d, %s", code, len(m.toolCalls("")), stderr)
	}
	_, stderr, code = runPG(t, "mcp", "--method", "tools/call", "--params", `{"Name":"generate_and_schedule_ai_pin","arguments":{}}`)
	if code != 2 || len(m.toolCalls("")) != 0 {
		t.Fatalf("Name-only: exit %d, %s", code, stderr)
	}
}

func TestAmbiguousSpendClassification(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"tool rejection", &pgmcp.Error{Kind: pgmcp.KindGeneric, Message: "x"}, false},
		{"rate limited", fmt.Errorf("wrap: %w", &platform.RateLimitedError{}), false},
		{"4xx", &client.APIError{StatusCode: 400}, false},
		{"5xx", &client.APIError{StatusCode: 502}, true},
		{"deadline", fmt.Errorf("POST /mcp: %w", context.DeadlineExceeded), true},
		{"dial refused", &url.Error{Op: "Post", URL: "u", Err: &net.OpError{Op: "dial", Err: syscall.ECONNREFUSED}}, false},
		{"dns", &url.Error{Op: "Post", URL: "u", Err: &net.DNSError{Err: "no such host", IsNotFound: true}}, false},
		{"reset after send", &url.Error{Op: "Post", URL: "u", Err: &net.OpError{Op: "read", Err: syscall.ECONNRESET}}, true},
		{"plaintext refusal", fmt.Errorf("x: %w", client.ErrPlaintextCredential), false},
		{"config error", fmt.Errorf("bad config"), false},
	}
	for _, tc := range cases {
		if got := pgAmbiguousSpend(tc.err); got != tc.want {
			t.Errorf("%s: got %v want %v", tc.name, got, tc.want)
		}
	}
}

func TestPinProductsExecuteReportsFailureExit(t *testing.T) {
	m := newMockMCP(t, map[string]string{
		"list_store_products":       `{"products":[{"id":"1","title":"Linen Throw","url":"https://example.com/shop/p/1"},{"id":"2","title":"Linen Throw","url":"https://example.com/shop/p/1?utm_source=x"},{"id":"3","title":"No Link"}]}`,
		"generate_and_schedule_pin": "!Not enough credits remaining",
	})
	out, _, code := runPG(t, "pin-products", "--store-id", "s1", "--pinterest-profile", "me", "--board-id", "b1", "--max-scan-pages", "1", "--execute", "--json")
	if code != 5 {
		t.Fatalf("out-of-credits run exited %d, want 5", code)
	}
	var v pinProductsView
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatalf("report not printed before the error: %v %s", err, out)
	}
	if v.Planned != 1 || len(m.toolCalls("generate_and_schedule_pin")) != 1 {
		t.Fatalf("duplicate URL or linkless product was planned: %+v calls %d", v, len(m.toolCalls("generate_and_schedule_pin")))
	}
}

func TestReviewLowFixes(t *testing.T) {
	// queue-gaps --board matches an ID even when a name is present.
	pins := []map[string]any{{"scheduledTimeUTC": "2026-03-01T09:00:00Z", "boardName": "Beaches", "boardId": "b1"}}
	if v := queueGaps(pins, time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC), 1, 0, "b1"); v.PinsInWindow != 1 {
		t.Errorf("board id filter: %+v", v)
	}
	// "daily" is not an AI automation.
	if r := runwayFor(map[string]any{"name": "x", "mode": "daily", "pinsPerDay": 1}); r.PinKind == "ai" {
		t.Errorf("daily read as ai: %+v", r)
	}
	if r := runwayFor(map[string]any{"name": "x", "pinType": "AI", "pinsPerDay": 1}); r.PinKind != "ai" {
		t.Errorf("AI token missed: %+v", r)
	}
	// Per-pin batches against an empty balance fail.
	if c := creditCheck(0, 0, 0, 1); c.Status != "fail" {
		t.Errorf("zero balance per-pin: %+v", c)
	}
}

func TestPreflightReturnsAuthError(t *testing.T) {
	newMockMCP(t, map[string]string{"get_account_status": "!Unauthorized: invalid access key"})
	if _, _, code := runPG(t, "preflight", "--json"); code != 4 {
		t.Fatalf("bad key exit %d, want 4", code)
	}
}

// TestPaidCallNotResentWithStaticIdempotencyKey: a static Idempotency-Key in
// config (which Pin Generator does not honour) must not make a paid
// tools/call retryable after a 5xx.
func TestPaidCallNotResentWithStaticIdempotencyKey(t *testing.T) {
	m := newMockMCP(t, map[string]string{"generate_pin_image": "HTTP 503", "list_templates": `{"templates":[]}`})
	t.Setenv(cliutil.DogfoodEnvVar, "")
	t.Setenv(cliutil.VerifyEnvVar, "")
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(cfgPath, []byte("[headers]\n\"Idempotency-Key\" = \"static-config-key\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PINGENERATOR_CONFIG", cfgPath)
	if _, _, code := runPG(t, "generate-pin-image", "--template", "S", "--title", "T", "--execute"); code == 0 {
		t.Fatal("503 should fail the paid call")
	}
	calls := m.toolCalls("generate_pin_image")
	if len(calls) != 1 {
		t.Fatalf("paid call reached the server %d times, want exactly 1", len(calls))
	}
	if calls[0].Idempotency != "static-config-key" {
		t.Fatalf("test is vacuous: static key not sent (got %q)", calls[0].Idempotency)
	}
}

func TestMCPCallsMarkPaidAndUnknownToolsReplayUnsafe(t *testing.T) {
	if pgmcp.ReplaySafeHeader != client.ReplaySafeHeader {
		t.Fatalf("pgmcp marker %q drifted from client %q", pgmcp.ReplaySafeHeader, client.ReplaySafeHeader)
	}
	for name, want := range map[string]string{"generate_and_schedule_ai_pin": "false", "create_automation": "false", "setup_design_profile": "false", "brand_new_tool": "false", "list_boards": ""} {
		p := &recordingPoster{}
		_, _ = pgmcp.Call(context.Background(), p, name, nil)
		if got := p.headers[pgmcp.ReplaySafeHeader]; got != want {
			t.Errorf("%s replay marker = %q, want %q", name, got, want)
		}
	}
}

type recordingPoster struct{ headers map[string]string }

func (p *recordingPoster) PostWithHeaders(_ context.Context, _ string, _ any, h map[string]string) (json.RawMessage, int, error) {
	p.headers = h
	return json.RawMessage(`{"result":{"content":[{"type":"text","text":"{}"}]}}`), 200, nil
}

func TestSavedProfileCannotArmSpend(t *testing.T) {
	m := newMockMCP(t, map[string]string{"generate_pin_image": `{"imageUrl":"https://example.com/cdn/x.png"}`})
	t.Setenv(cliutil.DogfoodEnvVar, "")
	t.Setenv(cliutil.VerifyEnvVar, "")
	// A hand-edited profile store that tries to arm --execute.
	if err := saveProfileStore(&profileStore{Profiles: map[string]Profile{"armed": {Name: "armed", Values: map[string]string{"execute": "true", "allow-unknown-tool": "true", "title": "From Profile"}}}}); err != nil {
		t.Fatal(err)
	}
	out, stderr, code := runPG(t, "generate-pin-image", "--template", "S", "--profile", "armed", "--json")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if n := len(m.toolCalls("generate_pin_image")); n != 0 || !strings.Contains(out, `"dry_run": true`) {
		t.Fatalf("profile armed a spend: calls=%d out=%s", n, out)
	}
	if !strings.Contains(out, "From Profile") {
		t.Fatalf("ordinary profile values should still apply: %s", out)
	}
	// An explicit --execute typed on the command line still spends.
	if _, stderr, code := runPG(t, "generate-pin-image", "--template", "S", "--profile", "armed", "--execute", "--json"); code != 0 || len(m.toolCalls("generate_pin_image")) != 1 {
		t.Fatalf("explicit --execute: exit %d calls %d %s", code, len(m.toolCalls("generate_pin_image")), stderr)
	}
}

func TestProfileApplyNeverSuppliesSpendConfirmations(t *testing.T) {
	newMockMCP(t, map[string]string{})
	root := RootCmd()
	gen, _, _ := root.Find([]string{"generate-pin-image"})
	if err := gen.Flags().Set("execute", "true"); err != nil {
		t.Fatal(err)
	}
	if err := ApplyProfileToFlags(gen, &Profile{Values: map[string]string{"execute": "false"}}); err != nil {
		t.Fatal(err)
	}
	if v, _ := gen.Flags().GetBool("execute"); !v {
		t.Fatal("explicit --execute must not be overridden")
	}
	gen2, _, _ := RootCmd().Find([]string{"generate-pin-image"})
	if err := ApplyProfileToFlags(gen2, &Profile{Values: map[string]string{"execute": "true"}}); err != nil {
		t.Fatal(err)
	}
	if gen2.Flags().Changed("execute") {
		t.Fatal("profile applied execute")
	}
}
