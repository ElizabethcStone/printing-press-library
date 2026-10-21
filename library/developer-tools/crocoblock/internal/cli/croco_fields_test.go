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
)

func TestCrocoPostFieldsDiscoveryTLS(t *testing.T) {
	c := crocoFormsTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		var v any
		switch r.URL.Path {
		case "/wp/v2/types/post":
			v = map[string]any{"rest_base": "posts", "rest_namespace": "wp/v2"}
		case "/jet-engine/v2/get-post-types":
			v = map[string]any{"success": true, "data": []any{}}
		case "/jet-engine/v2/get-meta-boxes":
			v = map[string]any{"success": true, "data": []any{map[string]any{"args": map[string]any{"object_type": "post", "allowed_post_type": []any{"post"}}, "meta_fields": []any{map[string]any{"name": "exposed", "type": "text"}, map[string]any{"name": "private", "type": "text"}}}}}
		case "/jet-engine/v2/get-built-in-post-type/post":
			v = map[string]any{"success": true, "data": map[string]any{"meta_fields": []any{}}}
		case "/wp/v2/posts":
			if r.URL.Query().Get("_method") != "OPTIONS" {
				t.Fatal("not a schema request")
			}
			v = map[string]any{"schema": map[string]any{"properties": map[string]any{"meta": map[string]any{"properties": map[string]any{"exposed": map[string]any{"type": "string"}, "foreign": map[string]any{"type": "string"}}}}}}
		default:
			t.Fatalf("unexpected request %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(v)
	})
	schema, e := crocoDiscoverPostFields(context.Background(), c, "post")
	if e != nil {
		t.Fatal(e)
	}
	if len(schema.Declared) != 2 || len(schema.Exposed) != 1 {
		t.Fatalf("declared/exposed intersection wrong: %+v", schema)
	}
	if e = crocoValidatePostField(schema, "foreign", "x"); e == nil {
		t.Fatal("non-JetEngine field writable")
	}
	if e = crocoValidatePostField(schema, "private", "x"); e == nil {
		t.Fatal("unexposed field writable")
	}
	if e = crocoValidatePostField(schema, "exposed", "x"); e != nil {
		t.Fatal(e)
	}
}
func TestCrocoRESTValueValidation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		schema map[string]any
		value  any
		fail   bool
	}{{"string", map[string]any{"type": "string"}, "ok", false}, {"wrong_type", map[string]any{"type": "integer"}, "2", true}, {"integer", map[string]any{"type": "integer"}, json.Number("2"), false}, {"fraction", map[string]any{"type": "integer"}, json.Number("2.2"), true}, {"array", map[string]any{"type": "array", "items": map[string]any{"type": "string"}}, []any{"a", json.Number("2")}, true}, {"unknown_schema", map[string]any{}, "a", true}, {"enum", map[string]any{"type": "string", "enum": []any{"a", "b"}}, "c", true}, {"object", map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{"x": map[string]any{"type": "string"}}}, map[string]any{"other": "bad"}, true}} {
		t.Run(tc.name, func(t *testing.T) {
			if e := crocoValidateRESTValue(tc.schema, tc.value); (e != nil) != tc.fail {
				t.Fatalf("err=%v", e)
			}
		})
	}
}
func TestCrocoPrivateSnapshotAndRestoreConflict(t *testing.T) {
	p := filepath.Join(t.TempDir(), "change.json")
	snap := &crocoFieldSnapshot{Version: 1, Site: "https://fixture.invalid/wp-json", PostType: "post", ID: "13", Before: map[string]any{"x": "old"}, After: map[string]any{"x": "new"}}
	f, e := crocoCreateFieldSnapshot(p, snap)
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	info, _ := f.Stat()
	if info.Mode().Perm() != 0600 {
		t.Fatal("snapshot not private")
	}
	if _, e = crocoReadFieldSnapshot(p); e == nil {
		t.Fatal("unverified snapshot accepted")
	}
	snap.Verified = true
	if e = crocoWriteFieldSnapshot(f, snap); e != nil {
		t.Fatal(e)
	}
	loaded, e := crocoReadFieldSnapshot(p)
	if e != nil {
		t.Fatal(e)
	}
	if e = crocoRestoreUnchanged(map[string]any{"x": "new", "unrelated": "newer"}, loaded); e != nil {
		t.Fatal(e)
	}
	if e = crocoRestoreUnchanged(map[string]any{"x": "concurrent"}, loaded); e == nil {
		t.Fatal("concurrent edit overwritten")
	}
	if _, e = crocoCreateFieldSnapshot(p, snap); e == nil {
		t.Fatal("existing snapshot overwritten")
	}
	link := filepath.Join(filepath.Dir(p), "link.json")
	if e = os.Symlink(p, link); e == nil {
		if _, e = crocoReadFieldSnapshot(link); e == nil {
			t.Fatal("symlink accepted")
		}
	}
	if e = os.Chmod(p, 0644); e != nil {
		t.Fatal(e)
	}
	if _, e = crocoReadFieldSnapshot(p); e == nil {
		t.Fatal("world-readable snapshot accepted")
	}
}
func TestCrocoPostMetaPermissionDenied(t *testing.T) {
	count := 0
	c := crocoFormsTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		count++
		if r.Method != "GET" {
			t.Error("read attempted mutation")
		}
		w.WriteHeader(403)
		fmt.Fprint(w, `{"code":"rest_forbidden","message":"denied"}`)
	})
	_, e := crocoPostMeta(context.Background(), c, "/wp/v2/posts", "13")
	if e == nil || count != 1 {
		t.Fatalf("denied falsely empty or retried: %d %v", count, e)
	}
}
func TestCrocoMetaReadbackPreservation(t *testing.T) {
	e := crocoVerifyMeta(map[string]any{"x": "old", "keep": "original"}, map[string]any{"x": "new", "keep": "changed"}, map[string]any{"x": "new"})
	if e == nil || !strings.Contains(e.Error(), "unrelated") {
		t.Fatalf("unrelated update not caught: %v", e)
	}
}

func TestCrocoIndexedJetEngineDefinitions(t *testing.T) {
	rows, e := crocoDeclaredRows(map[string]any{"meta-1": map[string]any{"id": "meta-1", "meta_fields": []any{}}})
	if e != nil || len(rows) != 1 {
		t.Fatalf("rows=%v err=%v", rows, e)
	}
	if _, e = crocoDeclaredRows(nil); e == nil {
		t.Fatal("missing collection reported empty")
	}
}

func TestCrocoNestedSensitiveFields(t *testing.T) {
	if !crocoNestedSensitive(map[string]any{"settings": []any{map[string]any{"api_key": "never-store"}}}) {
		t.Fatal("nested credential missed")
	}
	s := crocoPostFields{Declared: map[string]map[string]any{"settings": {"type": "repeater"}}, Exposed: map[string]map[string]any{"settings": {"type": "object"}}}
	if _, e := crocoSelectPostMeta(s, map[string]any{"settings": map[string]any{"access_token": "never-store"}}, []string{"settings"}); e == nil {
		t.Fatal("snapshot permitted nested credential")
	}
	if e := crocoValidatePostField(s, "settings", map[string]any{"client_secret": "never-store"}); e == nil {
		t.Fatal("write permitted nested credential")
	}
}
