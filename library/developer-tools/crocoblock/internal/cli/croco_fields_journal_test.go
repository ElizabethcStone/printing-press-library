package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/printing-press-library/library/developer-tools/crocoblock/internal/store"
	"github.com/spf13/cobra"
)

func TestCrocoFieldsIntentSurvivesVerificationFailures(t *testing.T) {
	for _, mode := range []string{"success", "committed_500", "readback_failure", "mismatch", "snapshot_failure"} {
		t.Run(mode, func(t *testing.T) {
			writes, reads := 0, 0
			snapshot := filepath.Join(t.TempDir(), "change.json")
			f, c := crocoEngineTestHTTP(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				var v any
				switch r.URL.Path {
				case "/wp/v2/types/post":
					v = map[string]any{"rest_base": "posts"}
				case "/jet-engine/v2/get-post-types":
					v = map[string]any{"data": []any{}}
				case "/jet-engine/v2/get-meta-boxes":
					v = map[string]any{"data": []any{map[string]any{"args": map[string]any{"allowed_post_type": []any{"post"}}, "meta_fields": []any{map[string]any{"name": "price", "type": "text"}}}}}
				case "/jet-engine/v2/get-built-in-post-type/post":
					v = map[string]any{"data": map[string]any{"meta_fields": []any{}}}
				case "/wp/v2/posts":
					v = map[string]any{"schema": map[string]any{"properties": map[string]any{"meta": map[string]any{"properties": map[string]any{"price": map[string]any{"type": "string"}}}}}}
				case "/wp/v2/posts/13":
					if r.Method == "POST" {
						writes++
						db, e := store.OpenReadOnly(crocoDBPath())
						if e != nil {
							t.Error(e)
							return
						}
						events, e := db.CrocoEvents(context.Background(), 0)
						db.Close()
						if e != nil || len(events) != 1 || events[0]["kind"] != "post_fields_attempt" {
							t.Errorf("intent absent before transport: %v %v", events, e)
						}
						if mode == "committed_500" {
							w.WriteHeader(500)
							fmt.Fprint(w, `{"code":"failed_after_commit"}`)
							return
						}
						v = map[string]any{"id": 13}
					} else {
						reads++
						value := "old"
						if reads > 1 {
							if mode == "readback_failure" {
								w.WriteHeader(403)
								fmt.Fprint(w, `{"code":"rest_forbidden"}`)
								return
							}
							value = "new"
							if mode == "mismatch" {
								value = "other"
							}
							if mode == "snapshot_failure" {
								if e := os.Remove(snapshot); e != nil {
									t.Error(e)
								}
							}
						}
						v = map[string]any{"meta": map[string]any{"price": value}}
					}
				default:
					t.Errorf("unexpected %s", r.URL.Path)
				}
				json.NewEncoder(w).Encode(v)
			})
			root := &cobra.Command{Use: "crocoblock-pp-cli"}
			for _, hook := range novelCommandHooks {
				hook(root, f)
			}
			cmd, _, e := root.Find([]string{"fields", "update"})
			if e != nil {
				t.Fatal(e)
			}
			if e = cmd.ParseFlags([]string{"13", "--data", `{"price":"new"}`, "--snapshot", snapshot}); e != nil {
				t.Fatal(e)
			}
			cmd.SetContext(context.Background())
			e = cmd.RunE(cmd, cmd.Flags().Args())
			if (e == nil) != (mode == "success") || writes != 1 {
				t.Fatalf("writes=%d err=%v", writes, e)
			}
			db, e := store.OpenReadOnly(crocoDBPath())
			if e != nil {
				t.Fatal(e)
			}
			defer db.Close()
			events, e := db.CrocoEvents(context.Background(), 0)
			if e != nil {
				t.Fatal(e)
			}
			cap, filters := crocoImpactFixture()
			cap.Site = c.BaseURL
			// A receipt concurrent with an ambiguous write cannot clear its intent.
			if mode != "success" {
				events = append(events, map[string]any{"id": int64(2), "site": c.BaseURL, "kind": "reindex", "resource": "filters", "detail": map[string]any{"verified": true, "reindex_event_id": 1}})
			}
			rows, _, e := crocoComputeFilterImpact(cap, filters, events, 0)
			want := "unknown"
			if mode == "success" {
				want = "candidate"
			}
			if e != nil || rows[0].(map[string]any)["status"] != want {
				t.Fatalf("%v %v", rows, e)
			}
			encoded, _ := json.Marshal(events)
			if strings.Contains(string(encoded), `"new"`) || strings.Contains(string(encoded), `"old"`) {
				t.Fatal("journal leaked values")
			}
		})
	}
}

func TestCrocoFieldIntentUnavailablePreventsTransport(t *testing.T) {
	t.Setenv("CROCOBLOCK_DB", t.TempDir())
	c := crocoFormsTestClient(t, func(w http.ResponseWriter, r *http.Request) { t.Fatal("unexpected HTTP") })
	_, e := crocoFieldWriteIntent(context.Background(), c, "13", "update", []string{"price"})
	if e == nil || !strings.Contains(e.Error(), "write_not_sent") || strings.Contains(e.Error(), "mutation_applied") {
		t.Fatal(e)
	}
}
