package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mvanhorn/printing-press-library/library/developer-tools/crocoblock/internal/client"
	"github.com/mvanhorn/printing-press-library/library/developer-tools/crocoblock/internal/config"
	"github.com/spf13/cobra"
)

func crocoFormsTestClient(t *testing.T, handler http.HandlerFunc) *client.Client {
	t.Helper()
	srv := httptest.NewTLSServer(handler)
	t.Cleanup(srv.Close)
	cfg := &config.Config{BaseURL: srv.URL, AuthHeaderVal: "Basic dGVzdDpmaXh0dXJl"}
	c := client.New(cfg, 2*time.Second, 1000)
	c.HTTPClient = srv.Client()
	c.NoCache = true
	return c
}
func TestCrocoFormBlockParser(t *testing.T) {
	for _, tc := range []struct {
		name, raw string
		want      int
		fail      bool
	}{
		{"empty", "", 0, false},
		{"nested", `<!-- wp:group --><div><!-- wp:jet-forms/text-field {"name":"reference","label":"Reference"} /--><!-- wp:jet-forms/repeater-field {"name":"guests"} --><!-- wp:jet-forms/text-field {"name":"guest_name"} /--><!-- /wp:jet-forms/repeater-field --></div><!-- /wp:group -->`, 3, false},
		{"unrelated", `<!-- wp:paragraph {"name":"not-a-field"} --><p>Text</p><!-- /wp:paragraph -->`, 0, false},
		{"invalid", `<!-- wp:jet-forms/text-field {broken} /-->`, 0, true},
		{"unterminated", `<!-- wp:jet-forms/text-field {"name":"lost"}`, 0, true},
		{"trailing", `<!-- wp:jet-forms/text-field {"name":"x"} garbage /-->`, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, e := crocoParseFormFields(tc.raw)
			if (e != nil) != tc.fail {
				t.Fatalf("err=%v", e)
			}
			if len(got) != tc.want {
				t.Fatalf("fields=%v", got)
			}
		})
	}
}
func TestCrocoFormDraftFieldsAndDenials(t *testing.T) {
	for _, tc := range []struct {
		name, status    string
		code            int
		native204, fail bool
	}{
		{name: "draft", status: "draft", code: 200}, {name: "empty_draft", status: "draft", code: 200}, {name: "published_204", status: "publish", code: 200, native204: true, fail: true}, {name: "denied", code: 403, fail: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			native := 0
			c := crocoFormsTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if strings.HasSuffix(r.URL.Path, "/fields") {
					native++
					w.WriteHeader(204)
					return
				}
				if tc.code == 403 {
					w.WriteHeader(403)
					fmt.Fprint(w, `{"code":"rest_forbidden_context","message":"denied"}`)
					return
				}
				raw := `<!-- wp:jet-forms/text-field {"name":"reference"} /-->`
				if tc.name == "empty_draft" {
					raw = ""
				}
				json.NewEncoder(w).Encode(map[string]any{"id": 42, "status": tc.status, "content": map[string]any{"raw": raw}})
			})
			got, source, e := crocoFormFields(context.Background(), c, "42")
			if (e != nil) != tc.fail {
				t.Fatalf("err=%v", e)
			}
			if tc.status == "draft" && native != 0 {
				t.Fatal("draft was sent to publish-only endpoint")
			}
			if tc.name == "draft" && (len(got) != 1 || source != "gutenberg_raw") {
				t.Fatalf("fields=%v source=%s", got, source)
			}
		})
	}
}
func TestCrocoFormWritePreservesRawBlocksAndMeta(t *testing.T) {
	raw := `<!-- wp:jet-forms/text-field {"name":"reference"} /-->`
	title := "Before"
	writes := 0
	c := crocoFormsTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == "POST" {
			writes++
			var body map[string]any
			if e := json.NewDecoder(r.Body).Decode(&body); e != nil {
				t.Fatal(e)
			}
			if len(body) != 1 || body["title"] != "After" {
				t.Errorf("unexpected patch: %v", body)
			}
			title = "After"
		}
		json.NewEncoder(w).Encode(map[string]any{"id": 42, "status": "draft", "title": map[string]any{"raw": title}, "content": map[string]any{"raw": raw}, "meta": map[string]any{"unrelated": "keep", "_jf_actions": "private-action-token"}})
	})
	got, id, e := crocoFormWrite(context.Background(), c, "update", "42", map[string]any{"title": "After"})
	if e != nil {
		t.Fatal(e)
	}
	if id != "42" || writes != 1 || crocoFormProperty(got, "content") != raw {
		t.Fatalf("write contract failed: %v", got)
	}
	safe, _ := json.Marshal(crocoSafe(got))
	if bytes.Contains(safe, []byte("private-action-token")) {
		t.Fatal("action credentials leaked")
	}
	if _, _, e = crocoFormWrite(context.Background(), c, "update", "42", map[string]any{"meta": []any{}}); e == nil {
		t.Fatal("invalid meta accepted")
	}
	if writes != 1 {
		t.Fatal("invalid meta caused a write")
	}
}
func TestCrocoFormPreservationFailure(t *testing.T) {
	before := map[string]any{"content": map[string]any{"raw": "original"}, "meta": map[string]any{"keep": "old"}}
	for _, after := range []map[string]any{{"content": map[string]any{"raw": "changed"}, "meta": map[string]any{"keep": "old"}}, {"content": map[string]any{"raw": "original"}, "meta": map[string]any{"keep": "changed"}}} {
		if e := crocoVerifyForm(before, after, map[string]any{}); e == nil {
			t.Fatal("silent unrelated mutation accepted")
		}
	}
}
func TestCrocoFormsRecordsFieldsDryRunNoIO(t *testing.T) {
	for _, path := range [][]string{{"forms", "create"}, {"forms", "update"}, {"forms", "trash"}, {"forms", "fields"}, {"records", "fields"}, {"fields", "update"}, {"fields", "restore"}} {
		t.Run(strings.Join(path, "_"), func(t *testing.T) {
			f := &rootFlags{dryRun: true, agent: true, asJSON: true}
			root := &cobra.Command{Use: "crocoblock-pp-cli"}
			for _, hook := range novelCommandHooks {
				hook(root, f)
			}
			cmd, rest, e := root.Find(path)
			if e != nil || len(rest) > 0 {
				t.Fatalf("find %v", e)
			}
			var out bytes.Buffer
			cmd.SetOut(&out)
			if e = cmd.RunE(cmd, nil); e != nil {
				t.Fatal(e)
			}
			if !strings.Contains(out.String(), "dry_run") {
				t.Fatalf("missing dry-run output %s", out.String())
			}
		})
	}
}
