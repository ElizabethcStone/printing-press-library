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

func TestCrocoRelationDefinitionsEdgesAndTraversal(t *testing.T) {
	f, c := crocoEngineTestHTTP(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Error(r.Method)
		}
		switch r.URL.Path {
		case "/jet-engine/v2/get-relations":
			fmt.Fprint(w, `{"success":true,"data":{"2":{"id":"2","args":{"parent_object":"posts::post","child_object":"posts::page"}}}}`)
		case "/jet-rel/2":
			fmt.Fprint(w, `{"13":[{"child_object_id":"14","meta":{"priority":"high"}}]}`)
		case "/jet-rel/2/children/13":
			fmt.Fprint(w, `[{"child_object_id":"14"}]`)
		case "/jet-rel/2/parents/14":
			fmt.Fprint(w, `[{"parent_object_id":"13"}]`)
		default:
			t.Error(r.URL.Path)
			http.NotFound(w, r)
		}
	})
	definitions, err := crocoRelationDefinitions(context.Background(), c)
	if err != nil || len(definitions) != 1 || crocoString(definitions[0].(map[string]any)["id"]) != "2" {
		t.Fatal(definitions, err)
	}
	edges, err := crocoRelationEdges(context.Background(), c, "2")
	if err != nil || len(edges) != 1 || !reflect.DeepEqual(edges[0], map[string]any{"parent_object_id": "13", "child_object_id": "14", "meta": map[string]any{"priority": "high"}}) {
		t.Fatal(edges, err)
	}
	for _, tc := range []struct {
		action     string
		args       []string
		key, value string
	}{{"children", []string{"2", "13"}, "child_object_id", "14"}, {"parents", []string{"2", "14"}, "parent_object_id", "13"}} {
		out, err := crocoEngineTestRun(newCrocoRelationsCommand(f, tc.action), tc.args...)
		var rows []map[string]any
		json.Unmarshal([]byte(out), &rows)
		if err != nil || len(rows) != 1 || rows[0][tc.key] != tc.value {
			t.Fatal(out, err)
		}
	}
	out, err := crocoEngineTestRun(newCrocoRelationsCommand(f, "list"), "2", "--offset", "1")
	if err != nil || strings.TrimSpace(out) != "[]" {
		t.Fatal(out, err)
	}
	t.Log("PASS typed definitions, flattened edges, exact parents/children routes and bounded empty slice")
}
func TestCrocoRelationUpdatesExplicitModeContextAndReadback(t *testing.T) {
	for _, mode := range []string{"append", "replace", "disconnect"} {
		for _, direction := range []string{"child", "parent"} {
			t.Run(mode+"_"+direction, func(t *testing.T) {
				writes := 0
				f, _ := crocoEngineTestHTTP(t, func(w http.ResponseWriter, r *http.Request) {
					switch {
					case r.URL.Path == "/jet-engine/v2/get-relations":
						fmt.Fprint(w, `{"success":true,"data":[{"id":2,"meta_fields":[]}]}`)
					case r.Method == "PUT":
						writes++
						if r.URL.Path != "/jet-rel/2" {
							t.Error(r.URL.Path)
						}
						var body map[string]any
						json.NewDecoder(r.Body).Decode(&body)
						want := map[string]any{"parent_id": float64(13), "child_id": []any{float64(14)}, "context": direction, "store_items_type": mode}
						if !reflect.DeepEqual(body, want) {
							t.Error("payload differs", body)
						}
						fmt.Fprint(w, `{"success":true}`)
					default:
						path := "/jet-rel/2/children/13"
						key := "child_object_id"
						id := "14"
						if direction == "parent" {
							path = "/jet-rel/2/parents/14"
							key = "parent_object_id"
							id = "13"
						}
						if r.Method != "GET" || r.URL.Path != path {
							t.Error(r.Method, r.URL.Path)
						}
						rows := []any{}
						if mode != "disconnect" {
							rows = append(rows, map[string]any{key: id})
						}
						json.NewEncoder(w).Encode(rows)
					}
				})
				out, err := crocoEngineTestRun(newCrocoRelationsCommand(f, "update"), "2", "--context", direction, "--mode", mode, "--data", `{"parent_id":13,"child_id":[14]}`)
				if err != nil || writes != 1 || !strings.Contains(out, `"verified": true`) {
					t.Fatal(out, err, writes)
				}
				t.Log("PASS exact PUT body with explicit context/mode and verified membership")
			})
		}
	}
}
func TestCrocoRelationInputValidationDryRunAndUnknownMeta(t *testing.T) {
	calls := 0
	writes := 0
	f, _ := crocoEngineTestHTTP(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method == "PUT" {
			writes++
		}
		fmt.Fprint(w, `{"success":true,"data":[{"id":2,"meta_fields":[{"name":"priority"}]}]}`)
	})
	for _, args := range [][]string{{"-1", "--data", `{"parent_id":13,"child_id":14}`}, {"2", "--data", `{"parent_id":13,"child_id":14}`}, {"2", "--context", "child", "--mode", "append", "--data", `{"parent_id":-13,"child_id":14}`}, {"2", "--context", "child", "--mode", "guess", "--data", `{"parent_id":13,"child_id":14}`}, {"2", "--context", "child", "--mode", "disconnect", "--data", `{"parent_id":13,"child_id":[]}`}, {"2", "--context", "child", "--mode", "append", "--data", `{"parent_id":13,"child_id":14,"hidden":true}`}} {
		if _, err := crocoEngineTestRun(newCrocoRelationsCommand(f, "update"), args...); err == nil {
			t.Fatal(args)
		}
	}
	if calls != 0 {
		t.Fatal("invalid IDs/mode reached server", calls)
	}
	if _, err := crocoEngineTestRun(newCrocoRelationsCommand(f, "update"), "2", "--context", "child", "--mode", "append", "--data", `{"parent_id":13,"child_id":14,"meta":{"undeclared":"value"}}`); err == nil || writes != 0 {
		t.Fatal("unknown metadata accepted", err, writes)
	}
	f.dryRun = true
	out, err := crocoEngineTestRun(newCrocoRelationsCommand(f, "update"), "--input", "/absent/file.json")
	if err != nil || !strings.Contains(out, "dry_run") || calls != 1 {
		t.Fatal(out, err, calls)
	}
	t.Log("PASS invalid inputs pre-HTTP, metadata checked against definitions and dry-run has no mutation")
}
func TestCrocoRelationMalformedMissingAndEmptyCoverage(t *testing.T) {
	for _, tc := range []struct {
		body  string
		empty bool
	}{{`[]`, true}, {`{}`, true}, {`null`, false}, {`{"13":null}`, false}, {`{"13":[{"child_object_id":"wrong"}]}`, false}} {
		t.Run(tc.body, func(t *testing.T) {
			_, c := crocoEngineTestHTTP(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, tc.body) })
			rows, err := crocoRelationEdges(context.Background(), c, "2")
			if tc.empty {
				if err != nil || rows == nil || len(rows) != 0 {
					t.Fatal(rows, err)
				}
			} else if err == nil {
				t.Fatal("false empty success", rows)
			}
			t.Log("PASS only real empty edge collections yield []; malformed data fails")
		})
	}
	for _, body := range []string{`{"success":true}`, `{"success":true,"data":null}`, `{"success":false,"data":[]}`} {
		_, c := crocoEngineTestHTTP(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) })
		if _, err := crocoRelationDefinitions(context.Background(), c); err == nil {
			t.Fatal("fabricated definitions coverage", body)
		}
	}
}
