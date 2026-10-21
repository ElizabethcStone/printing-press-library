package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/mvanhorn/printing-press-library/library/developer-tools/crocoblock/internal/client"
	"github.com/mvanhorn/printing-press-library/library/developer-tools/crocoblock/internal/config"
	"github.com/mvanhorn/printing-press-library/library/developer-tools/crocoblock/internal/store"
	"github.com/spf13/cobra"
)

func crocoEngineTestHTTP(t *testing.T, h http.HandlerFunc) (*rootFlags, *client.Client) {
	t.Helper()
	server := httptest.NewTLSServer(h)
	t.Cleanup(server.Close)
	path := filepath.Join(t.TempDir(), "events.db")
	t.Setenv("CROCOBLOCK_DB", path)
	t.Setenv("CROCOBLOCK_BASE_URL", server.URL)
	t.Setenv("CROCOBLOCK_USERNAME", "fixture")
	t.Setenv("CROCOBLOCK_APPLICATION_PASSWORD", "fixture-password")
	cfg := &config.Config{BaseURL: server.URL, CrocoblockUsername: "fixture", CrocoblockApplicationPassword: "fixture-password"}
	c := client.New(cfg, 3*time.Second, 10000)
	c.HTTPClient = server.Client()
	c.NoCache = true
	old := clientHooks
	clientHooks = append(append([]func(*client.Client) error{}, old...), func(c *client.Client) error { c.HTTPClient = server.Client(); return nil })
	t.Cleanup(func() { clientHooks = old })
	return &rootFlags{asJSON: true, timeout: 3 * time.Second, rateLimit: 10000, configPath: filepath.Join(t.TempDir(), "absent.toml")}, c
}
func crocoEngineTestRun(cmd *cobra.Command, args ...string) (string, error) {
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}
func crocoEngineTestResult(w http.ResponseWriter, result any) {
	raw, _ := json.Marshal(result)
	json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": map[string]any{"content": []any{map[string]any{"type": "text", "text": string(raw)}}}})
}
func crocoEngineTestRPC(t *testing.T, r *http.Request) (string, map[string]any) {
	t.Helper()
	if r.Method != "POST" || r.URL.Path != "/jet-engine/v1/mcp" {
		t.Errorf("wrong RPC transport: %s %s", r.Method, r.URL.Path)
	}
	if r.Header.Get("Authorization") == "" {
		t.Error("missing Basic auth")
	}
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		t.Error(err)
		return "", nil
	}
	if body["method"] != "tools/call" || body["jsonrpc"] != "2.0" {
		t.Error(body)
	}
	params, _ := body["params"].(map[string]any)
	args, _ := params["arguments"].(map[string]any)
	return crocoString(params["name"]), args
}

func TestCrocoEngineReadsUseReadOnlyPOSTAndSelection(t *testing.T) {
	for _, tc := range []struct{ name, tool string }{{"configuration", "resource-get-configuration"}, {"website", "resource-get-website-config"}, {"macros", "resource-get-macros"}} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			f, _ := crocoEngineTestHTTP(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				tool, args := crocoEngineTestRPC(t, r)
				if tool != tc.tool {
					t.Error(tool)
				}
				if tc.name != "macros" && len(args["parts"].(map[string]any)) == 0 {
					t.Error("no requested parts")
				}
				crocoEngineTestResult(w, map[string]any{"success": true, "api_key": "must-not-print", "observed": "present"})
			})
			f.selectFields = "success"
			cmd := newCrocoEngineCommand(f, tc.name)
			if cmd.Annotations["mcp:read-only"] != "true" {
				t.Fatal("read annotation missing")
			}
			out, err := crocoEngineTestRun(cmd)
			if err != nil {
				t.Fatal(err)
			}
			var got map[string]any
			if json.Unmarshal([]byte(out), &got) != nil || !reflect.DeepEqual(got, map[string]any{"success": true}) || calls != 1 {
				t.Fatalf("select/read mismatch: %s calls=%d", out, calls)
			}
			t.Log("PASS POST native MCP read with exact tool and --select projection")
		})
	}
}
func TestCrocoEngineAllCreatesPayloadAndReadback(t *testing.T) {
	for _, tc := range []struct{ name, body, part string }{
		{"create-cct", `{"name":"Rooms","slug":"rooms","fields":[{"field_name":"label","field_type":"text"}]}`, "custom_content_types"},
		{"create-cpt", `{"general_settings":{"name":"Rooms","slug":"rooms"}}`, "post_types"},
		{"create-taxonomy", `{"general_settings":{"name":"Room class","slug":"room_class","object_type":["rooms"]}}`, "taxonomies"},
		{"create-meta-box", `{"general_settings":{"name":"Room details","object_type":"post"}}`, "meta_boxes"},
		{"create-query", `{"name":"Recent rooms","query_type":"posts","query_args":{"post_type":"rooms","posts_per_page":5}}`, "queries"},
		{"create-listing", `{"query_id":3,"view_type":"blocks"}`, ""},
		{"create-glossary", `{"name":"Class","source":"manual","fields":[{"value":"suite","label":"Suite"}]}`, "glossaries"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := []string{}
			var createdID any = 7
			if tc.name == "create-meta-box" {
				createdID = "meta-7"
			}
			var want map[string]any
			json.Unmarshal([]byte(tc.body), &want)
			f, _ := crocoEngineTestHTTP(t, func(w http.ResponseWriter, r *http.Request) {
				calls = append(calls, r.Method+" "+r.URL.Path)
				if r.Method == "GET" {
					if tc.name != "create-listing" || r.URL.Path != "/wp/v2/jet-engine/7" || r.URL.Query().Get("context") != "edit" {
						t.Error("unexpected readback", r.URL.String())
					}
					json.NewEncoder(w).Encode(map[string]any{"id": createdID})
					return
				}
				tool, args := crocoEngineTestRPC(t, r)
				if tool == "resource-get-configuration" {
					if !reflect.DeepEqual(args, map[string]any{"parts": map[string]any{tc.part: true}}) {
						t.Error(args)
					}
					crocoEngineTestResult(w, map[string]any{"success": true, tc.part: []any{map[string]any{"id": createdID}}})
					return
				}
				if tool != "tool-add-"+strings.TrimPrefix(tc.name, "create-") || !reflect.DeepEqual(args, want) {
					t.Fatalf("tool/payload mismatch %s %+v", tool, args)
				}
				crocoEngineTestResult(w, map[string]any{"success": true, "item_id": createdID})
			})
			out, err := crocoEngineTestRun(newCrocoEngineCommand(f, tc.name), "--data", tc.body)
			if err != nil {
				t.Fatal(err)
			}
			var result map[string]any
			json.Unmarshal([]byte(out), &result)
			if result["verified"] != true || crocoString(result["item_id"]) != crocoString(createdID) || len(calls) != 2 {
				t.Fatalf("readback missing: %s %+v", out, calls)
			}
			t.Log("PASS exact create payload, readback ID and verified=true")
		})
	}
}
func TestCrocoEngineModulesExplicitOperations(t *testing.T) {
	for _, operation := range []string{"list", "activate", "deactivate"} {
		t.Run(operation, func(t *testing.T) {
			active := operation == "deactivate"
			writes := 0
			calls := 0
			f, _ := crocoEngineTestHTTP(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				tool, args := crocoEngineTestRPC(t, r)
				if tool != "tool-manage-modules" {
					t.Error(tool)
				}
				if args["operation"] != "list" {
					writes++
					if args["operation"] != operation || !reflect.DeepEqual(args["modules"], []any{"custom-content-types"}) {
						t.Error(args)
					}
					active = operation == "activate"
				}
				all := []any{map[string]any{"slug": "custom-content-types"}}
				enabled := []any{}
				if active {
					enabled = all
				}
				crocoEngineTestResult(w, map[string]any{"success": true, "all_modules": all, "active_modules": enabled})
			})
			args := []string{"--operation", operation}
			if operation != "list" {
				args = append(args, "--modules", "custom-content-types")
			}
			out, err := crocoEngineTestRun(newCrocoEngineCommand(f, "modules"), args...)
			if err != nil {
				t.Fatal(err)
			}
			if operation == "list" {
				if writes != 0 || calls != 1 {
					t.Fatal(writes, calls)
				}
			} else {
				if writes != 1 || calls != 3 || !strings.Contains(out, `"verified": true`) {
					t.Fatal(out, writes, calls)
				}
			}
			t.Log("PASS module operation exact, list read-only and mutation readback")
		})
	}
}
func TestCrocoEngineRejectsInvalidInputBeforeHTTP(t *testing.T) {
	calls := 0
	f, _ := crocoEngineTestHTTP(t, func(w http.ResponseWriter, r *http.Request) { calls++; http.Error(w, "unexpected", 500) })
	for _, tc := range []struct{ name, body string }{{"create-cct", `{"name":"Rooms","slug":"../private"}`}, {"create-cct", `{"name":"Rooms","slug":"rooms","unknown":true}`}, {"create-listing", `{"query_id":-1}`}, {"create-cpt", `{"general_settings":{"name":"Rooms","slug":"rooms","unknown":true}}`}, {"create-query", `{"name":"Rooms","query_type":"posts"}`}, {"create-glossary", `{"name":"x","fields":[{"value":"x"}]}`}} {
		out, err := crocoEngineTestRun(newCrocoEngineCommand(f, tc.name), "--data", tc.body)
		if err == nil {
			t.Fatal("invalid input accepted", tc.name, out)
		}
	}
	if calls != 0 {
		t.Fatal("invalid request reached HTTP", calls)
	}
	t.Log("PASS invalid schema keys, slug, negative ID, missing query args and required glossary fields rejected before HTTP")
}
func TestCrocoEngineMalformedPermissionAndEmptyRPCFail(t *testing.T) {
	for _, mode := range []string{"malformed", "permission", "empty", "tool-error", "missing-success"} {
		t.Run(mode, func(t *testing.T) {
			f, _ := crocoEngineTestHTTP(t, func(w http.ResponseWriter, r *http.Request) {
				switch mode {
				case "malformed":
					fmt.Fprint(w, "<html>broken</html>")
				case "permission":
					w.WriteHeader(403)
					fmt.Fprint(w, `{"code":"rest_forbidden","message":"forbidden"}`)
				case "empty":
					fmt.Fprint(w, `{"result":{"content":[]}}`)
				case "tool-error":
					fmt.Fprint(w, `{"result":{"isError":true,"content":[]}}`)
				default:
					crocoEngineTestResult(w, map[string]any{})
				}
			})
			if out, err := crocoEngineTestRun(newCrocoEngineCommand(f, "configuration")); err == nil {
				t.Fatal("false success", out)
			}
			t.Log("PASS upstream failure rejected without empty-success fabrication")
		})
	}
}
func TestCrocoEngineDryRunNeverReadsInputOrSends(t *testing.T) {
	calls := 0
	f, _ := crocoEngineTestHTTP(t, func(w http.ResponseWriter, r *http.Request) { calls++ })
	f.dryRun = true
	for _, name := range []string{"create-cct", "create-cpt", "create-taxonomy", "create-meta-box", "create-query", "create-listing", "create-glossary", "modules"} {
		out, err := crocoEngineTestRun(newCrocoEngineCommand(f, name), "--input", "/missing/private-input.json")
		if err != nil || !strings.Contains(out, "dry_run") {
			t.Fatal(name, out, err)
		}
	}
	if calls != 0 {
		t.Fatal(calls)
	}
	t.Log("PASS all engine mutation dry runs skip input files and HTTP")
}
func TestCrocoEngineJournalPreflightPreventsMutation(t *testing.T) {
	writes := 0
	f, _ := crocoEngineTestHTTP(t, func(w http.ResponseWriter, r *http.Request) {
		writes++
		crocoEngineTestResult(w, map[string]any{"success": true, "item_id": 7})
	})
	t.Setenv("CROCOBLOCK_DB", t.TempDir())
	_, err := crocoEngineTestRun(newCrocoEngineCommand(f, "create-cct"), "--data", `{"name":"Rooms","slug":"rooms"}`)
	if err == nil || !strings.Contains(err.Error(), "prepare write journal") || writes != 0 {
		t.Fatal(err, writes)
	}
	t.Log("PASS unavailable journal fails preflight before any remote write")
}
func TestCrocoEngineMutationJournalFailureDoesNotRetry(t *testing.T) {
	writes := 0
	unavailableDB := t.TempDir()
	f, _ := crocoEngineTestHTTP(t, func(w http.ResponseWriter, r *http.Request) {
		writes++
		// Simulate the journal becoming unavailable after preflight and the write.
		if err := os.Setenv("CROCOBLOCK_DB", unavailableDB); err != nil {
			t.Error(err)
		}
		crocoEngineTestResult(w, map[string]any{"success": true, "item_id": 7})
	})
	_, err := crocoEngineTestRun(newCrocoEngineCommand(f, "create-cct"), "--data", `{"name":"Rooms","slug":"rooms"}`)
	if err == nil || !strings.Contains(err.Error(), "mutation_applied") || writes != 1 {
		t.Fatal(err, writes)
	}
	t.Log("PASS applied write plus journal failure returns explicit error and makes no retry")
}
func TestCrocoEngineJournalRecordsNoPayload(t *testing.T) {
	f, _ := crocoEngineTestHTTP(t, func(w http.ResponseWriter, r *http.Request) {
		tool, _ := crocoEngineTestRPC(t, r)
		if tool == "resource-get-configuration" {
			crocoEngineTestResult(w, map[string]any{"success": true, "custom_content_types": []any{map[string]any{"id": 7}}})
		} else {
			crocoEngineTestResult(w, map[string]any{"success": true, "item_id": 7})
		}
	})
	if _, err := crocoEngineTestRun(newCrocoEngineCommand(f, "create-cct"), "--data", `{"name":"Private display label","slug":"rooms"}`); err != nil {
		t.Fatal(err)
	}
	db, err := store.OpenReadOnly(crocoDBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	events, err := db.CrocoEvents(context.Background(), 0)
	if err != nil || len(events) != 1 {
		t.Fatal(events, err)
	}
	b, _ := json.Marshal(events)
	if strings.Contains(string(b), "Private display label") {
		t.Fatal("payload leaked into journal")
	}
	t.Log("PASS journal retains operation/ID/field names without input values")
}

func TestCrocoEngineCreatedIDContracts(t *testing.T) {
	for _, tc := range []struct {
		name, command string
		value         any
		want          string
	}{
		{"meta string", "create-meta-box", "meta-7", "meta-7"},
		{"numeric ID", "create-cct", float64(7), "7"},
		{"string numeric", "create-query", "7", "7"},
		{"meta requires prefix", "create-meta-box", 7, ""},
		{"meta zero", "create-meta-box", "meta-0", ""},
		{"meta negative", "create-meta-box", "meta--7", ""},
		{"meta path", "create-meta-box", "meta-7/other", ""},
		{"meta URL", "create-meta-box", "https://site/meta-7", ""},
		{"numeric rejects meta", "create-cpt", "meta-7", ""},
		{"numeric rejects negative", "create-glossary", -7, ""},
		{"nil", "create-listing", nil, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := crocoEngineCreatedID(tc.command, tc.value)
			if got != tc.want || (err != nil) != (tc.want == "") {
				t.Fatalf("got %q err %v; want %q", got, err, tc.want)
			}
		})
	}
}

func TestCrocoEngineInvalidCreatedIDDoesNotRetry(t *testing.T) {
	calls := 0
	f, _ := crocoEngineTestHTTP(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		crocoEngineTestResult(w, map[string]any{"success": true, "item_id": "meta-7/other"})
	})
	_, err := crocoEngineTestRun(newCrocoEngineCommand(f, "create-meta-box"), "--data", `{"general_settings":{"name":"Fixture","object_type":"post"}}`)
	if err == nil || !strings.Contains(err.Error(), "mutation_applied_unknown") || calls != 1 {
		t.Fatal(err, calls)
	}
	t.Log("PASS malformed created identifier fails after one mutation, without retry or unsafe readback")
}
