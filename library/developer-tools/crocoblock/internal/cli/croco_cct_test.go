package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func crocoCCTTestIndex() any {
	fields := map[string]any{"label": map[string]any{"type": "string"}, "keep": map[string]any{"type": "string"}}
	return map[string]any{"routes": map[string]any{
		"/jet-cct/rooms":                 map[string]any{"endpoints": []any{map[string]any{"methods": []any{"GET"}, "args": fields}, map[string]any{"methods": []any{"POST"}, "args": fields}}},
		"/jet-cct/rooms/(?P<_ID>[\\d]+)": map[string]any{"endpoints": []any{map[string]any{"methods": []any{"GET"}, "args": []any{}}, map[string]any{"methods": []any{"POST", "PUT", "PATCH"}, "args": fields}, map[string]any{"methods": []any{"DELETE"}, "args": []any{}}}},
	}}
}
func TestCrocoCCTLifecycleExactPayloadAndPartialUpdate(t *testing.T) {
	item := map[string]any{}
	calls := []string{}
	f, _ := crocoEngineTestHTTP(t, func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		if r.URL.Path == "/" {
			json.NewEncoder(w).Encode(crocoCCTTestIndex())
			return
		}
		switch r.Method {
		case "POST":
			if r.URL.Path != "/jet-cct/rooms" {
				t.Error(r.URL.Path)
			}
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			if !reflect.DeepEqual(body, map[string]any{"label": "Before", "keep": "Retained"}) {
				t.Error(body)
			}
			item = body
			item["_ID"] = "7"
			json.NewEncoder(w).Encode(map[string]any{"success": true, "item_id": 7})
		case "PUT":
			if r.URL.Path != "/jet-cct/rooms/7" {
				t.Error(r.URL.Path)
			}
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			if !reflect.DeepEqual(body, map[string]any{"label": "After"}) {
				t.Fatalf("partial write expanded: %+v", body)
			}
			for k, v := range body {
				item[k] = v
			}
			json.NewEncoder(w).Encode(map[string]any{"success": true, "item_id": 7})
		case "DELETE":
			if r.URL.Path != "/jet-cct/rooms/7" {
				t.Error(r.URL.Path)
			}
			item = nil
			json.NewEncoder(w).Encode(map[string]any{"success": true})
		case "GET":
			if r.URL.Path != "/jet-cct/rooms/7" {
				t.Error(r.URL.Path)
			}
			json.NewEncoder(w).Encode(item)
		default:
			t.Error(r.Method)
		}
	})
	for _, tc := range []struct {
		action string
		args   []string
	}{{"create", []string{"rooms", "--data", `{"label":"Before","keep":"Retained"}`}}, {"get", []string{"rooms", "7"}}, {"update", []string{"rooms", "7", "--data", `{"label":"After"}`}}, {"delete", []string{"rooms", "7"}}} {
		out, err := crocoEngineTestRun(newCrocoCCTCommand(f, tc.action), tc.args...)
		if err != nil {
			t.Fatal(tc.action, err)
		}
		if tc.action != "get" && !strings.Contains(out, `"verified": true`) {
			t.Fatal(out)
		}
		if tc.action == "update" && item["keep"] != "Retained" {
			t.Fatal("omitted field lost")
		}
	}
	writes := []string{}
	for _, call := range calls {
		if !strings.HasPrefix(call, "GET ") {
			writes = append(writes, call)
		}
	}
	if !reflect.DeepEqual(writes, []string{"POST /jet-cct/rooms", "PUT /jet-cct/rooms/7", "DELETE /jet-cct/rooms/7"}) {
		t.Fatal(writes)
	}
	t.Log("PASS create/get/partial PUT/delete paths, payloads and persisted readbacks; omitted fields preserved")
}
func TestCrocoCCTListExactPaginationAndFilters(t *testing.T) {
	f, _ := crocoEngineTestHTTP(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			json.NewEncoder(w).Encode(crocoCCTTestIndex())
			return
		}
		if r.Method != "GET" || r.URL.Path != "/jet-cct/rooms" {
			t.Error(r.Method, r.URL.Path)
		}
		want := map[string]string{"_limit": "2", "_offset": "3", "label": "Suite", "_cct_search": "quiet", "_cct_search_by": "label", "_orderby": "label", "_order": "desc"}
		if len(r.URL.Query()) != len(want) {
			t.Error(r.URL.Query())
		}
		for k, v := range want {
			if r.URL.Query().Get(k) != v {
				t.Error(k, r.URL.Query())
			}
		}
		json.NewEncoder(w).Encode([]any{map[string]any{"_ID": "8", "label": "Suite"}})
	})
	f.selectFields = "_ID"
	out, err := crocoEngineTestRun(newCrocoCCTCommand(f, "list"), "rooms", "--limit", "2", "--offset", "3", "--where", "label=Suite", "--search", "quiet", "--search-by", "label", "--order-by", "label", "--order", "desc")
	if err != nil {
		t.Fatal(err)
	}
	var rows []map[string]any
	if json.Unmarshal([]byte(out), &rows) != nil || !reflect.DeepEqual(rows, []map[string]any{{"_ID": "8"}}) {
		t.Fatal(out)
	}
	t.Log("PASS bounded query parameters exact and --select removes unselected fields")
}
func TestCrocoCCTPageRejectsMalformedAndPreservesRealEmpty(t *testing.T) {
	for _, body := range []string{`[]`, `null`, `{}`, `[{"label":"missing ID"}]`, `[{"_ID":"1"},{"_ID":"2"}]`} {
		t.Run(body, func(t *testing.T) {
			_, c := crocoEngineTestHTTP(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) })
			rows, err := crocoCCTPage(context.Background(), c, "rooms", 1, 0, nil)
			if body == `[]` {
				if err != nil || rows == nil || len(rows) != 0 {
					t.Fatal(rows, err)
				}
			} else if err == nil {
				t.Fatal("malformed/oversized page accepted", rows)
			}
			t.Log("PASS response shape/limit checked; only actual [] counts as empty")
		})
	}
}
func TestCrocoCCTInvalidIDsBoundsAndDryRunBeforeHTTP(t *testing.T) {
	calls := 0
	f, _ := crocoEngineTestHTTP(t, func(w http.ResponseWriter, r *http.Request) { calls++; json.NewEncoder(w).Encode(crocoCCTTestIndex()) })
	for _, tc := range []struct {
		action string
		args   []string
	}{{"list", []string{"../rooms"}}, {"get", []string{"rooms", "-1"}}, {"delete", []string{"rooms", "0"}}, {"list", []string{"rooms", "--limit", "0"}}, {"list", []string{"rooms", "--offset", "-1"}}, {"update", []string{"rooms", "7", "--data", `{"_ID":8}`}}} {
		if _, err := crocoEngineTestRun(newCrocoCCTCommand(f, tc.action), tc.args...); err == nil {
			t.Fatal(tc)
		}
	}
	if calls != 0 {
		t.Fatal("invalid ID reached HTTP", calls)
	}
	f.dryRun = true
	for _, action := range []string{"create", "update", "delete"} {
		args := []string{}
		if action != "delete" {
			args = []string{"--input", "/missing/file.json"}
		}
		out, err := crocoEngineTestRun(newCrocoCCTCommand(f, action), args...)
		if err != nil || !strings.Contains(out, "dry_run") {
			t.Fatal(out, err)
		}
	}
	if calls != 0 {
		t.Fatal(calls)
	}
	t.Log("PASS invalid IDs/slugs/bounds rejected pre-HTTP; all dry runs skip files/network")
}
func TestCrocoCCTUnknownKeysAndPermissionPreventMutation(t *testing.T) {
	for _, forbidden := range []bool{false, true} {
		t.Run(fmt.Sprint(forbidden), func(t *testing.T) {
			writes := 0
			f, _ := crocoEngineTestHTTP(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" {
					writes++
				}
				if forbidden {
					w.WriteHeader(403)
					fmt.Fprint(w, `{"code":"rest_forbidden","message":"denied"}`)
				} else {
					json.NewEncoder(w).Encode(crocoCCTTestIndex())
				}
			})
			if _, err := crocoEngineTestRun(newCrocoCCTCommand(f, "create"), "rooms", "--data", `{"unknown":"value"}`); err == nil {
				t.Fatal("unexpected success")
			}
			if writes != 0 {
				t.Fatal(writes)
			}
			t.Log("PASS unknown field or permission error prevents mutation")
		})
	}
}
