package metamcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mvanhorn/printing-press-library/library/social-and-messaging/whatsapp-business/internal/cliutil"
	"github.com/mvanhorn/printing-press-library/library/social-and-messaging/whatsapp-business/internal/config"
)

func fixtureClient(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	s := httptest.NewServer(h)
	t.Cleanup(s.Close)
	c, err := New(context.Background(), &config.Config{BaseURL: s.URL, AuthHeaderVal: "Bearer fixture-only"}, false, time.Second, 1000)
	if err != nil {
		t.Fatal(err)
	}
	c.limiter = nil
	return c
}
func TestCatalogValidation(t *testing.T) {
	if len(Catalog()) != 24 {
		t.Fatal("catalog must include all 24 captured tools")
	}
	for _, tc := range []struct {
		name  string
		args  map[string]any
		valid bool
	}{
		{"whatsapp_biz_businesses", map[string]any{"action": "list"}, true},
		{"whatsapp_biz_businesses", map[string]any{}, false},
		{"ping", map[string]any{"unexpected": true}, false},
		{"ping", map[string]any{"message": false}, false},
		{"whatsapp_biz_add_phone_number", map[string]any{"phone_number": "+15550100123", "display_name": "Fixture", "vertical": "OTHER", "business_id": "fixture-business", "confirm": true}, true},
	} {
		t.Run(tc.name+fmt.Sprint(tc.valid), func(t *testing.T) {
			tool, ok := Lookup(tc.name)
			if !ok {
				t.Fatal("missing tool")
			}
			if Command(tc.name) == "" {
				t.Fatal("missing command")
			}
			if err := Validate(tool, tc.args); (err == nil) != tc.valid {
				t.Fatalf("valid=%v err=%v", tc.valid, err)
			}
		})
	}
	if _, ok := Lookup("unknown"); ok {
		t.Fatal("unknown tool resolved")
	}
}
func TestInitializeCallAndCatalog(t *testing.T) {
	var calls []string
	c := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fixture-only" {
			t.Error("auth missing")
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		m, _ := body["method"].(string)
		calls = append(calls, m)
		if m != "initialize" && r.Header.Get("Mcp-Session-Id") != "session-fixture" {
			t.Error("session not carried")
		}
		if m == "notifications/initialized" {
			w.WriteHeader(202)
			return
		}
		result := any(map[string]any{})
		switch m {
		case "initialize":
			w.Header().Set("Mcp-Session-Id", "session-fixture")
			result = map[string]any{"protocolVersion": "2025-03-26"}
		case "tools/list":
			result = map[string]any{"tools": []any{map[string]any{"name": "ping", "inputSchema": map[string]any{"type": "object"}}}}
		case "tools/call":
			result = map[string]any{"structuredContent": map[string]any{"result": "{\"response\":\"pong\"}"}}
		}
		json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": body["id"], "result": result})
	})
	if err := c.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	cat, err := c.Tools(context.Background())
	if err != nil || len(cat) != 1 || cat[0].Name != "ping" {
		t.Fatalf("catalog %v %v", cat, err)
	}
	tool, _ := Lookup("ping")
	v, err := c.Call(context.Background(), tool, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if v.(map[string]any)["response"] != "pong" {
		t.Fatalf("not decoded: %#v", v)
	}
	if strings.Join(calls, ",") != "initialize,notifications/initialized,tools/list,tools/call" {
		t.Fatal(calls)
	}
}
func TestEventAndErrorResponses(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
		wantErr    bool
	}{
		{"sse", "event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"structuredContent\":{\"response\":\"pong\"}}}\n\n", 200, false},
		{"wrong-id", `{"jsonrpc":"2.0","id":999,"result":{}}`, 200, true},
		{"tool-error", `{"jsonrpc":"2.0","id":1,"result":{"isError":true,"content":[{"type":"text","text":"fixture failure"}]}}`, 200, true},
		{"rpc-error", `{"jsonrpc":"2.0","id":1,"error":{"code":-32602,"message":"fixture failure"}}`, 200, true},
		{"plain-auth", "Unauthorized", 401, true},
		{"non-json", "not-json", 200, true},
		{"empty-success", `{"jsonrpc":"2.0","id":1,"result":{}}`, 200, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
				if tc.name == "sse" {
					w.Header().Set("Content-Type", "text/event-stream")
				}
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			})
			tool, _ := Lookup("ping")
			v, err := c.Call(context.Background(), tool, map[string]any{})
			if (err != nil) != tc.wantErr {
				t.Fatalf("value=%v error=%v", v, err)
			}
			if !tc.wantErr && v.(map[string]any)["response"] != "pong" {
				t.Fatal(v)
			}
		})
	}
}
func TestMutationNeverRetries(t *testing.T) {
	var n atomic.Int32
	c := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(429)
	})
	_, err := c.request(context.Background(), "tools/call", map[string]any{}, false, false)
	var rate *cliutil.RateLimitError
	if !errors.As(err, &rate) {
		t.Fatalf("expected rate error got %v", err)
	}
	if n.Load() != 1 {
		t.Fatalf("write retried %d times", n.Load())
	}
}
func TestReadRetryHonorsContext(t *testing.T) {
	c := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) { w.Header().Set("Retry-After", "2"); w.WriteHeader(429) })
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := c.request(ctx, "tools/list", nil, true, false)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("context not enforced: %v", err)
	}
}
func TestAuthAndEndpointBoundaries(t *testing.T) {
	for _, tc := range []struct {
		url, auth string
		native    bool
		ok        bool
	}{
		{"https://mcp.facebook.com", "Bearer fixture-only", false, true},
		{"http://127.0.0.1:1234", "Bearer fixture-only", false, true},
		{"https://example.com", "Bearer fixture-only", false, false},
		{"https://mcp.facebook.com", "", false, false},
		{"http://127.0.0.1:1234", "", true, false},
	} {
		_, err := New(context.Background(), &config.Config{BaseURL: tc.url, AuthHeaderVal: tc.auth}, tc.native, time.Second, 2)
		if (err == nil) != tc.ok {
			t.Fatalf("url=%s err=%v", tc.url, err)
		}
	}
	h, err := ResolveAuth(context.Background(), &config.Config{AuthHeaderVal: "Bearer fixture-only"}, false)
	if err != nil || h != "Bearer fixture-only" {
		t.Fatal(err)
	}
}
func TestNormalizePreservesUnknown(t *testing.T) {
	for _, tc := range []struct {
		in   any
		want string
	}{
		{map[string]any{"result": "{\"status\":\"unknown\"}"}, `{"status":"unknown"}`},
		{map[string]any{"result": "[]", "other": true}, `{"other":true,"result":[]}`},
		{map[string]any{"result": "not JSON"}, `{"result":"not JSON"}`},
		{[]any{}, `[]`},
	} {
		v := Normalize(tc.in)
		raw, _ := json.Marshal(v)
		if string(raw) != tc.want {
			t.Fatalf("%s != %s", raw, tc.want)
		}
	}
}

func TestLongRetryAfterIsTypedThrottle(t *testing.T) {
	c := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) { w.Header().Set("Retry-After", "30"); w.WriteHeader(429) })
	_, err := c.request(context.Background(), "tools/list", nil, true, false)
	var rate *cliutil.RateLimitError
	if !errors.As(err, &rate) {
		t.Fatalf("throttle lost: %v", err)
	}
}
func TestNormalizeKeepsLargeNumericIdentifiers(t *testing.T) {
	result := Normalize(map[string]any{"result": `{"identifier":9007199254740993}`})
	raw, _ := json.Marshal(result)
	if !strings.Contains(string(raw), "9007199254740993") {
		t.Fatalf("identifier rounded: %s", raw)
	}
}

func TestToolCatalogRetainsUnknownMetadata(t *testing.T) {
	raw := `{"name":"ping","title":"Ping","inputSchema":{"type":"object"},"annotations":{"readOnlyHint":true,"destructiveHint":false,"futureFlag":"fixture"},"_meta":{"beta":true}}`
	var tool Tool
	if err := json.Unmarshal([]byte(raw), &tool); err != nil {
		t.Fatal(err)
	}
	saved, err := json.Marshal(tool)
	if err != nil {
		t.Fatal(err)
	}
	var v map[string]any
	json.Unmarshal(saved, &v)
	if v["title"] != "Ping" || v["_meta"].(map[string]any)["beta"] != true || v["annotations"].(map[string]any)["futureFlag"] != "fixture" {
		t.Fatalf("catalog lost future metadata: %s", saved)
	}
}

func TestSensitiveInputsRedactedFromBothErrorForms(t *testing.T) {
	for _, rpc := range []bool{false, true} {
		t.Run(fmt.Sprint(rpc), func(t *testing.T) {
			message := "verification=fixture-code-918273 pin=fixture-pin-5286 token=fixture-webhook-abcdef auth=fixture-only"
			c := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				json.NewDecoder(r.Body).Decode(&body)
				reply := map[string]any{"jsonrpc": "2.0", "id": body["id"]}
				if rpc {
					reply["error"] = map[string]any{"code": -32000, "message": message}
				} else {
					reply["result"] = map[string]any{"isError": true, "content": []any{map[string]any{"type": "text", "text": message}}}
				}
				json.NewEncoder(w).Encode(reply)
			})
			tool := Tool{Name: "fixture-secrets", InputSchema: map[string]any{"type": "object", "properties": map[string]any{"verification_code": map[string]any{"type": "string"}, "pin": map[string]any{"type": "string"}, "verify_token": map[string]any{"type": "string"}}}}
			args := map[string]any{"verification_code": "fixture-code-918273", "pin": "fixture-pin-5286", "verify_token": "fixture-webhook-abcdef"}
			_, err := c.Call(context.Background(), tool, args)
			if err == nil {
				t.Fatal("expected server error")
			}
			for _, secret := range []string{"fixture-code-918273", "fixture-pin-5286", "fixture-webhook-abcdef", "fixture-only"} {
				if strings.Contains(err.Error(), secret) {
					t.Fatalf("sensitive input leaked: %s", err)
				}
			}
			if !strings.Contains(err.Error(), "[REDACTED]") {
				t.Fatal("missing redaction marker")
			}
		})
	}
	nested := map[string]any{"public": "kept", "nested": map[string]any{"verify_token": "fixture-nested"}, "list": []any{map[string]any{"pin": "fixture-nested-pin"}}}
	raw, _ := json.Marshal(PreviewArguments(nested))
	if strings.Contains(string(raw), "fixture-nested") {
		t.Fatal("nested preview leak")
	}
	if nested["nested"].(map[string]any)["verify_token"] != "fixture-nested" {
		t.Fatal("preview mutated original")
	}
}
