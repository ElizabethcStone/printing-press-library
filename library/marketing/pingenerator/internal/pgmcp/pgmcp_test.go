// Copyright 2026 Elizabeth Stone and contributors. Licensed under Apache-2.0. See LICENSE.

package pgmcp

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// priorCommands is the exact command surface of the hand-built CLI this print
// replaces (skills/pp-pingenerator/pingenerator-pp-cli). Renaming any of these
// breaks the house skill and existing scripts.
var priorCommands = []string{
	"get-account-status", "list-pinterest-profiles", "list-boards", "list-scheduled-pins",
	"find-website-posts", "get-post-content", "list-connected-stores", "list-store-products",
	"list-templates", "generate-pin-image", "generate-and-schedule-pin", "schedule-pin",
	"list-design-profiles", "setup-design-profile", "update-design-profile",
	"generate-ai-pin-image", "generate-and-schedule-ai-pin",
	"list-automations", "create-automation", "run-automation-batch", "get-automation-batch-status",
}

func TestCatalogMatchesPriorSurface(t *testing.T) {
	if len(Tools) != 21 {
		t.Fatalf("expected 21 tools, got %d", len(Tools))
	}
	var got []string
	seenName := map[string]bool{}
	for _, tl := range Tools {
		got = append(got, tl.Cmd)
		if seenName[tl.Name] {
			t.Errorf("duplicate tool %s", tl.Name)
		}
		seenName[tl.Name] = true
		if tl.Cmd != strings.ReplaceAll(tl.Name, "_", "-") {
			t.Errorf("%s: command %q does not mirror tool name", tl.Name, tl.Cmd)
		}
		if tl.Cost.Spends() && !tl.Mutating {
			t.Errorf("%s: credit tool must be Mutating", tl.Cmd)
		}
		flags := map[string]bool{}
		for _, p := range tl.Params {
			if flags[p.Flag()] {
				t.Errorf("%s: duplicate flag --%s", tl.Cmd, p.Flag())
			}
			flags[p.Flag()] = true
		}
	}
	if !reflect.DeepEqual(got, priorCommands) {
		t.Fatalf("command surface drifted from the prior CLI:\n got %v\nwant %v", got, priorCommands)
	}
}

func TestCreditToolsMatchPriorPricing(t *testing.T) {
	cases := map[string]string{
		"generate_pin_image":           "1 credit",
		"generate_and_schedule_pin":    "1 credit",
		"schedule_pin":                 "1 credit",
		"generate_ai_pin_image":        "12-24 credits",
		"generate_and_schedule_ai_pin": "13-25 credits",
		"run_automation_batch":         "credits per pin",
		"create_automation":            "credits per pin",
		"setup_design_profile":         "free",
		"list_boards":                  "free",
	}
	for name, want := range cases {
		tl, ok := Lookup(name)
		if !ok {
			t.Fatalf("missing tool %s", name)
		}
		if got := tl.Cost.Note(); got != want {
			t.Errorf("%s cost = %q, want %q", name, got, want)
		}
	}
}

func TestFlagName(t *testing.T) {
	cases := map[string]string{
		"imageURL":                   "image-url",
		"mediaURL":                   "media-url",
		"urlText":                    "url-text",
		"pinterestProfile":           "pinterest-profile",
		"useAIBoardSelection":        "use-ai-board-selection",
		"aiTextMode":                 "ai-text-mode",
		"designProfileName":          "design-profile-name",
		"batchFrequencyDays":         "batch-frequency-days",
		"includeNewPostsFromSitemap": "include-new-posts-from-sitemap",
		"boardId":                    "board-id",
		"title":                      "title",
	}
	for in, want := range cases {
		if got := FlagName(in); got != want {
			t.Errorf("FlagName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBuildRequestShape(t *testing.T) {
	b, _ := json.Marshal(BuildRequest("list_boards", map[string]any{"pinterestProfile": "me"}))
	want := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_boards","arguments":{"pinterestProfile":"me"}}}`
	if string(b) != want {
		t.Fatalf("request = %s\nwant %s", b, want)
	}
	b, _ = json.Marshal(BuildRequest("get_account_status", nil))
	if !strings.Contains(string(b), `"arguments":{}`) {
		t.Fatalf("nil args must encode as {}: %s", b)
	}
}

func TestBuildArgs(t *testing.T) {
	gen, _ := Lookup("generate_pin_image")
	args, err := BuildArgs(gen, map[string]any{"template": "Standard", "image": []string{"a.jpg", "b.jpg"}})
	if err != nil {
		t.Fatal(err)
	}
	if args["image1"] != "a.jpg" || args["image2"] != "b.jpg" || args["template"] != "Standard" {
		t.Fatalf("image layers not expanded: %v", args)
	}
	if _, ok := args["image"]; ok {
		t.Fatalf("raw image key leaked: %v", args)
	}

	auto, _ := Lookup("create_automation")
	if _, err := BuildArgs(auto, map[string]any{"sourceType": "rss"}); err == nil || !strings.Contains(err.Error(), "--source-type") {
		t.Fatalf("enum violation not reported: %v", err)
	}
	if _, err := BuildArgs(auto, map[string]any{"batchFrequencyDays": 14}); err == nil {
		t.Fatal("int enum violation not reported")
	}
	args, err = BuildArgs(auto, map[string]any{"boards": `[{"id":"b1","name":"Beaches"}]`})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := args["boards"].([]any); !ok {
		t.Fatalf("boards JSON not decoded: %#v", args["boards"])
	}
	if _, err := BuildArgs(auto, map[string]any{"boards": `[{`}); err == nil {
		t.Fatal("invalid JSON accepted")
	}
	if missing := MissingRequired(auto, map[string]any{"name": "x"}); !reflect.DeepEqual(missing, []string{"source-type", "pinterest-profile"}) {
		t.Fatalf("missing = %v", missing)
	}
}

func TestUnwrap(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		want    string
		errKind Kind
		wantErr bool
	}{
		{"json text payload", `{"result":{"content":[{"type":"text","text":"{\"boards\":[]}"}]}}`, `{"boards":[]}`, 0, false},
		{"sse framed", "event: message\ndata: {\"result\":{\"content\":[{\"type\":\"text\",\"text\":\"[1,2]\"}]}}\n\n", `[1,2]`, 0, false},
		{"plain text payload", `{"result":{"content":[{"type":"text","text":"queued"}]}}`, `{"text":"queued"}`, 0, false},
		{"isError auth", `{"result":{"isError":true,"content":[{"type":"text","text":"User not found"}]}}`, "", KindAuth, true},
		{"isError credits", `{"result":{"isError":true,"content":[{"type":"text","text":"Not enough credits"}]}}`, "", KindCredits, true},
		{"rpc error rate", `{"error":{"code":-32000,"message":"Rate limit exceeded"}}`, "", KindRateLimit, true},
		{"rpc unknown method", `{"error":{"code":-32601,"message":"Method not found"}}`, "", KindNotFound, true},
		{"rpc error generic", `{"error":{"code":-32603,"message":"Internal error"}}`, "", KindGeneric, true},
		{"not json", `<html>oops</html>`, "", KindUnreadable, true},
		{"no result or error", `{"jsonrpc":"2.0","id":1}`, "", KindUnreadable, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Unwrap("tool", []byte(tc.body))
			if tc.wantErr {
				var me *Error
				if !errors.As(err, &me) || me.Kind != tc.errKind {
					t.Fatalf("err = %v, want kind %d", err, tc.errKind)
				}
				return
			}
			if err != nil || string(got) != tc.want {
				t.Fatalf("got %s, %v; want %s", got, err, tc.want)
			}
		})
	}
}

type fakePoster struct {
	path    string
	headers map[string]string
	body    any
	reply   string
}

func (f *fakePoster) PostWithHeaders(_ context.Context, path string, body any, headers map[string]string) (json.RawMessage, int, error) {
	f.path, f.body, f.headers = path, body, headers
	return json.RawMessage(f.reply), 200, nil
}

func TestCallUsesMCPPathAndAccept(t *testing.T) {
	f := &fakePoster{reply: `{"result":{"content":[{"type":"text","text":"{\"ok\":true}"}]}}`}
	got, err := Call(context.Background(), f, "list_templates", nil)
	if err != nil || string(got) != `{"ok":true}` {
		t.Fatalf("Call = %s, %v", got, err)
	}
	if f.path != "/mcp" || f.headers["Accept"] != AcceptHeader {
		t.Fatalf("path %q headers %v", f.path, f.headers)
	}
	if req, ok := f.body.(Request); !ok || req.Method != "tools/call" || req.Params.Name != "list_templates" {
		t.Fatalf("body = %#v", f.body)
	}
}

func TestShapeHelpers(t *testing.T) {
	m := Object(json.RawMessage(`{"a":{"b":"x"},"n":"42","t":1767225600,"ms":1767225600000,"d":"2026-01-01"}`))
	if Str(m, "missing", "a/b") != "x" {
		t.Error("dotted Str")
	}
	if v, ok := Num(m, "n"); !ok || v != 42 {
		t.Error("numeric string Num")
	}
	want := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, k := range []string{"t", "ms", "d"} {
		if got, ok := Time(m, k); !ok || !got.Equal(want) {
			t.Errorf("Time(%s) = %v %v", k, got, ok)
		}
	}
	if l := List(json.RawMessage(`{"meta":1,"pins":[{"id":1},{"id":2}]}`), "scheduledPins"); len(l) != 2 {
		t.Errorf("List fallback found %d", len(l))
	}
	if l := List(json.RawMessage(`[{"id":1}]`)); len(l) != 1 {
		t.Error("bare array List")
	}
	if NormalizeURL("https://WWW.Example.com/Best-Beaches/?utm_source=x#top") != "example.com/Best-Beaches" {
		t.Errorf("NormalizeURL = %q", NormalizeURL("https://WWW.Example.com/Best-Beaches/?utm_source=x#top"))
	}
	if NormalizeTitle("  10 Best Beaches!! (2026) ") != "10 best beaches 2026" {
		t.Errorf("NormalizeTitle = %q", NormalizeTitle("  10 Best Beaches!! (2026) "))
	}
}

func TestLedgerRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "ledger.jsonl")
	entries, bad, err := ReadLedger(path)
	if err != nil || len(entries) != 0 || bad != 0 {
		t.Fatalf("missing ledger should be empty: %v %d %v", entries, bad, err)
	}
	tool, _ := Lookup("generate_and_schedule_pin")
	e := NewLedgerEntry(tool, map[string]any{"url": "https://example.com/a", "title": "A", "pinterestProfile": "me", "image1": "skip"},
		json.RawMessage(`{"pinId":"p1","imageUrl":"https://img/x.png","secret":"nope"}`), "pingenerator-pp-cli pin-post", time.Unix(0, 0))
	if e.Keys["image1"] != "" || e.Result["secret"] != "" {
		t.Fatalf("ledger kept non-allowlisted fields: %+v", e)
	}
	if err := AppendLedger(path, e); err != nil {
		t.Fatal(err)
	}
	entries, _, err = ReadLedger(path)
	if err != nil || len(entries) != 1 || entries[0].Result["pinId"] != "p1" || entries[0].CreditsMax != 1 {
		t.Fatalf("round trip = %+v %v", entries, err)
	}
	if !LedgeredURLs(entries)["example.com/a"] {
		t.Fatal("ledgered URL not normalized")
	}
}

func TestNormalizeURLKeepsIdentifyingQuery(t *testing.T) {
	a, b := NormalizeURL("https://site.example.com/?p=1"), NormalizeURL("https://site.example.com/?p=2")
	if a == b || a == NormalizeURL("https://site.example.com/") {
		t.Fatalf("?p= posts collide: %q %q", a, b)
	}
	if NormalizeURL("https://www.example.com/a/?utm_source=pin&b=2&a=1&fbclid=x#top") != "example.com/a?a=1&b=2" {
		t.Fatalf("tracking strip/sort = %q", NormalizeURL("https://www.example.com/a/?utm_source=pin&b=2&a=1&fbclid=x#top"))
	}
}

func TestLedgerSurvivesLongLinesAndCountsOnlyScheduling(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.jsonl")
	gen, _ := Lookup("generate_pin_image")
	sched, _ := Lookup("generate_and_schedule_pin")
	big := strings.Repeat("x", 2<<20)
	if err := AppendLedger(path, NewLedgerEntry(gen, map[string]any{"url": "https://example.com/img", "title": big}, nil, "c", time.Now())); err != nil {
		t.Fatal(err)
	}
	if err := AppendLedger(path, NewLedgerEntry(sched, map[string]any{"url": "https://example.com/a"}, nil, "c", time.Now())); err != nil {
		t.Fatal(err)
	}
	entries, bad, err := ReadLedger(path)
	if err != nil || bad != 0 || len(entries) != 2 {
		t.Fatalf("long line hid entries: %d entries, %d bad, %v", len(entries), bad, err)
	}
	got := LedgeredURLs(entries)
	if !got["example.com/a"] || got["example.com/img"] {
		t.Fatalf("pinned set = %v", got)
	}
}

func TestClassifyNotFound(t *testing.T) {
	for msg, want := range map[string]Kind{
		"Batch not found":                 KindNotFound,
		"Could not read URL page (404)":   KindNotFound,
		"User not found for access key":   KindAuth,
		"Not enough credits remaining":    KindCredits,
		"Rate limit exceeded: not found?": KindRateLimit,
	} {
		if got := Classify("t", msg).Kind; got != want {
			t.Errorf("%q: kind %d want %d", msg, got, want)
		}
	}
}
