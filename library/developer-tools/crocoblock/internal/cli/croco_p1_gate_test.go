package cli

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// Seed 20261020 selects bookings list, fields get, filters list from the read surface.
// These execute the complete Cobra tree over TLS. Live checks remain separate.
func TestCrocoPriorityOneGate(t *testing.T) {
	calls := 0
	_, _ = crocoEngineTestHTTP(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		var v any
		switch r.URL.Path {
		case "/jet-booking/v2/bookings-list":
			v = map[string]any{"success": true, "data": []any{}, "total": 0}
		case "/jet-smart-filters-api/v1/filters":
			v = map[string]any{"filters": []any{}, "count": "0"}
		case "/wp/v2/types/post":
			v = map[string]any{"rest_base": "posts", "rest_namespace": "wp/v2"}
		case "/jet-engine/v2/get-post-types":
			v = map[string]any{"success": true, "data": []any{}}
		case "/jet-engine/v2/get-meta-boxes":
			v = map[string]any{"success": true, "data": []any{map[string]any{"args": map[string]any{"object_type": "post", "allowed_post_type": []any{"post"}}, "meta_fields": []any{map[string]any{"name": "pp_keep", "type": "text"}}}}}
		case "/jet-engine/v2/get-built-in-post-type/post":
			v = map[string]any{"success": true, "data": map[string]any{"meta_fields": []any{}}}
		case "/wp/v2/posts":
			v = map[string]any{"schema": map[string]any{"properties": map[string]any{"meta": map[string]any{"properties": map[string]any{"pp_keep": map[string]any{"type": "string"}}}}}}
		case "/wp/v2/posts/13":
			v = map[string]any{"id": 13, "meta": map[string]any{"pp_keep": "unchanged"}}
		default:
			t.Errorf("unexpected route %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode(v)
	})
	for _, base := range [][]string{{"bookings", "list"}, {"fields", "get", "13", "--fields", "pp_keep"}, {"filters", "list"}} {
		for _, mode := range []string{"--help", "--dry-run", "--json"} {
			t.Run(strings.Join(base, " ")+mode, func(t *testing.T) {
				before := calls
				args := append(append([]string{}, base...), mode, "--no-learn")
				if mode == "--dry-run" {
					args = append(args, "--json")
				}
				out, e := crocoEngineTestRun(RootCmd(), args...)
				if e != nil {
					t.Fatal(e, out)
				}
				if mode == "--help" {
					if !strings.Contains(out, "Usage:") || !strings.Contains(out, "crocoblock-pp-cli") {
						t.Fatal(out)
					}
				} else {
					var v any
					if e = json.Unmarshal([]byte(out), &v); e != nil {
						t.Fatal(e, out)
					}
				}
				if mode != "--json" && calls != before {
					t.Fatal("help/dryrun performed HTTP")
				}
				if mode == "--json" && calls == before {
					t.Fatal("read failed to execute")
				}
				t.Logf("PASS %s %s: real leaf, output valid, requests=%d", strings.Join(base, " "), mode, calls-before)
			})
		}
	}
}
