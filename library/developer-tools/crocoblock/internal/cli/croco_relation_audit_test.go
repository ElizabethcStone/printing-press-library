package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func crocoAuditTestInputs(parentType, childType string) (crocoAuditSnapshot, map[string]crocoAuditSnapshot) {
	defs := crocoAuditSnapshot{Capture: crocoP2TestCapture("engine:relations", []string{"schema"}, map[string]any{}), Rows: []map[string]any{{"id": "2", "args": map[string]any{"parent_object": parentType, "child_object": childType}}}}
	loaded := map[string]crocoAuditSnapshot{"relations:2": {Capture: crocoP2TestCapture("relations:2", []string{"parent_object_id", "child_object_id"}, map[string]any{"relation_id": "2"}), Rows: []map[string]any{{"id": "1", "parent_object_id": "1", "child_object_id": "2"}}}}
	for _, typ := range []string{parentType, childType} {
		resource, idKey, _ := crocoAuditEndpoint(typ)
		if resource == "" {
			continue
		}
		_, slug, _ := strings.Cut(typ, "::")
		scope := map[string]any{"type": slug}
		fields := []string{idKey}
		rows := []map[string]any{{idKey: "1"}, {idKey: "2"}}
		if strings.HasPrefix(typ, "posts::") {
			scope["status"] = "any"
			fields = append(fields, "type", "status")
			for _, r := range rows {
				r["type"] = slug
				r["status"] = "publish"
			}
		}
		loaded[resource] = crocoAuditSnapshot{Capture: crocoP2TestCapture(resource, fields, scope), Rows: rows}
	}
	return defs, loaded
}
func TestCrocoRelationAuditFindingTable(t *testing.T) {
	tests := []struct {
		name, parent, child string
		alter               func(map[string]crocoAuditSnapshot)
		kind                string
		complete            bool
		valid               int
	}{
		{"valid posts", "posts::post", "posts::post", nil, "", true, 1},
		{"valid cct", "cct::rooms", "cct::rooms", nil, "", true, 1},
		{"duplicate edge", "cct::rooms", "cct::rooms", func(m map[string]crocoAuditSnapshot) {
			s := m["relations:2"]
			s.Rows = append(s.Rows, map[string]any{"id": "2", "parent_object_id": "1", "child_object_id": "2"})
			m["relations:2"] = s
		}, "duplicate_edge", true, 1},
		{"missing CCT endpoint complete", "cct::rooms", "cct::rooms", func(m map[string]crocoAuditSnapshot) { s := m["cct:rooms"]; s.Rows = s.Rows[:1]; m["cct:rooms"] = s }, "dangling_endpoint", true, 0},
		{"missing post endpoint restricted", "posts::post", "posts::post", func(m map[string]crocoAuditSnapshot) {
			s := m["objects:post"]
			s.Rows = s.Rows[:1]
			m["objects:post"] = s
		}, "unknown_endpoint", false, 0},
		{"missing inventory", "cct::rooms", "cct::rooms", func(m map[string]crocoAuditSnapshot) { delete(m, "cct:rooms") }, "unknown_inventory", false, 0},
		{"incomplete inventory", "cct::rooms", "cct::rooms", func(m map[string]crocoAuditSnapshot) {
			s := m["cct:rooms"]
			s.Capture.Complete = false
			s.Rows = s.Rows[:1]
			m["cct:rooms"] = s
		}, "unknown_endpoint", false, 0},
		{"filtered CCT inventory", "cct::rooms", "cct::rooms", func(m map[string]crocoAuditSnapshot) {
			s := m["cct:rooms"]
			s.Capture.Scope["status"] = "publish"
			s.Rows = s.Rows[:1]
			m["cct:rooms"] = s
		}, "unknown_endpoint", false, 0},
		{"incomplete edges", "cct::rooms", "cct::rooms", func(m map[string]crocoAuditSnapshot) {
			s := m["relations:2"]
			s.Capture.Complete = false
			m["relations:2"] = s
		}, "unknown_edges", false, 1},
		{"missing edges", "cct::rooms", "cct::rooms", func(m map[string]crocoAuditSnapshot) { delete(m, "relations:2") }, "unknown_edges", false, 0},
		{"wrong inventory site", "cct::rooms", "cct::rooms", func(m map[string]crocoAuditSnapshot) {
			s := m["cct:rooms"]
			s.Capture.Site = "https://different.example/wp-json"
			m["cct:rooms"] = s
		}, "unknown_endpoint", false, 0},
		{"incompatible inventory version", "cct::rooms", "cct::rooms", func(m map[string]crocoAuditSnapshot) {
			s := m["cct:rooms"]
			s.Capture.Versions = map[string]string{"jet-engine": "4.0.0"}
			m["cct:rooms"] = s
		}, "unknown_endpoint", false, 0},
		{"unsupported endpoint", "mix::users", "posts::post", nil, "unknown_inventory", false, 0},
		{"duplicate inventory ID", "cct::rooms", "cct::rooms", func(m map[string]crocoAuditSnapshot) {
			s := m["cct:rooms"]
			s.Rows = append(s.Rows, map[string]any{"_ID": "2"})
			m["cct:rooms"] = s
		}, "duplicate_inventory_id", true, 1},
		{"malformed edge", "cct::rooms", "cct::rooms", func(m map[string]crocoAuditSnapshot) {
			s := m["relations:2"]
			s.Rows[0]["child_object_id"] = "-1"
			m["relations:2"] = s
		}, "invalid_edge", false, 0},
		{"missing inventory ID field", "cct::rooms", "cct::rooms", func(m map[string]crocoAuditSnapshot) { s := m["cct:rooms"]; s.Capture.Fields = nil; m["cct:rooms"] = s }, "unknown_inventory", false, 0},
		{"wrong post type", "posts::post", "posts::post", func(m map[string]crocoAuditSnapshot) {
			s := m["objects:post"]
			s.Rows[1]["type"] = "page"
			m["objects:post"] = s
		}, "invalid_inventory", false, 0},
		{"empty complete edges", "cct::rooms", "cct::rooms", func(m map[string]crocoAuditSnapshot) { s := m["relations:2"]; s.Rows = nil; m["relations:2"] = s }, "", true, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			defs, loaded := crocoAuditTestInputs(tc.parent, tc.child)
			if tc.alter != nil {
				tc.alter(loaded)
			}
			got, e := crocoComputeRelationAudit(defs, loaded, "", 100)
			if e != nil {
				t.Fatal(e)
			}
			if got["analysis_complete"] != tc.complete || got["scanned"].(map[string]int)["valid_edges"] != tc.valid {
				t.Fatal(got)
			}
			findings := got["findings"].([]any)
			found := false
			for _, f := range findings {
				m := f.(map[string]any)
				if m["kind"] == tc.kind {
					found = true
				}
				if tc.kind == "unknown_endpoint" && m["kind"] == "dangling_endpoint" {
					t.Fatal("fabricated orphan from unknown coverage", got)
				}
			}
			if tc.kind == "" && len(findings) != 0 || tc.kind != "" && !found {
				t.Fatal("finding mismatch", tc.kind, got)
			}
			if tc.kind == "dangling_endpoint" {
				f := findings[0].(map[string]any)
				if f["object_id"] != "2" || f["side"] != "child" {
					t.Fatal(f)
				}
			}
			t.Log("PASS typed endpoints, duplicate detection and absent versus unknown coverage")
		})
	}
}
func TestCrocoRelationAuditDefinitionAndBoundsTable(t *testing.T) {
	for _, tc := range []struct {
		name    string
		alter   func(*crocoAuditSnapshot)
		request string
		limit   int
		wantErr bool
	}{{"empty definitions", func(s *crocoAuditSnapshot) { s.Rows = nil }, "", 100, false}, {"incomplete definitions", func(s *crocoAuditSnapshot) { s.Capture.Complete = false }, "", 100, true}, {"missing version", func(s *crocoAuditSnapshot) { s.Capture.Versions = nil }, "", 100, true}, {"wrong resource", func(s *crocoAuditSnapshot) { s.Capture.Resource = "wrong" }, "", 100, true}, {"duplicate definitions", func(s *crocoAuditSnapshot) { s.Rows = append(s.Rows, s.Rows[0]) }, "", 100, true}, {"missing requested relation", nil, "99", 100, true}, {"negative request", nil, "-1", 100, true}, {"zero limit", nil, "", 0, true}, {"over limit", nil, "", 501, true}} {
		t.Run(tc.name, func(t *testing.T) {
			defs, loaded := crocoAuditTestInputs("cct::rooms", "cct::rooms")
			if tc.alter != nil {
				tc.alter(&defs)
			}
			got, e := crocoComputeRelationAudit(defs, loaded, tc.request, tc.limit)
			if (e != nil) != tc.wantErr {
				t.Fatal(got, e)
			}
			if !tc.wantErr && (got["analysis_complete"] != true || len(got["findings"].([]any)) != 0 || got["total_relations"] != 0) {
				t.Fatal(got)
			}
			t.Log("PASS definition provenance, explicit empty state and bounded input")
		})
	}
	for _, tc := range []struct{ endpoint, resource, idKey string }{{"posts::post", "objects:post", "id"}, {"cct::rooms", "cct:rooms", "_ID"}, {"terms::category", "", ""}, {"posts::../private", "", ""}, {"mix::users", "", ""}, {"", "", ""}} {
		r, k, _ := crocoAuditEndpoint(tc.endpoint)
		if r != tc.resource || k != tc.idKey {
			t.Fatal(tc, r, k)
		}
	}
}
func TestCrocoRelationAuditBoundedCountsAndPrivacy(t *testing.T) {
	defs, loaded := crocoAuditTestInputs("cct::rooms", "cct::rooms")
	s := loaded["cct:rooms"]
	s.Rows = nil
	loaded["cct:rooms"] = s
	edge := loaded["relations:2"]
	edge.Rows[0]["private_email"] = "private@person.test"
	loaded["relations:2"] = edge
	got, e := crocoComputeRelationAudit(defs, loaded, "2", 1)
	if e != nil {
		t.Fatal(e)
	}
	if len(got["findings"].([]any)) != 1 || got["total_findings"] != 2 || got["truncated"] != true || got["scanned"].(map[string]int)["edges"] != 1 {
		t.Fatal(got)
	}
	raw, _ := json.Marshal(got)
	if strings.Contains(string(raw), "private@person.test") {
		t.Fatal("edge value leaked")
	}
	t.Log("PASS displayed limit preserves full scanned totals and excludes unrelated record values")
}
func TestCrocoRelationAuditCLIReadOnlyAndControls(t *testing.T) {
	defs, loaded := crocoAuditTestInputs("posts::post", "posts::post")
	path := filepath.Join(t.TempDir(), "audit.db")
	crocoP2TestSave(t, path, defs.Capture, defs.Rows)
	for _, s := range loaded {
		crocoP2TestSave(t, path, s.Capture, s.Rows)
	}
	before, _ := os.ReadFile(path)
	f := &rootFlags{asJSON: true, timeout: 3 * time.Second, selectFields: "findings"}
	out, e := crocoEngineTestRun(newCrocoRelationAuditCommand(f), "2", "--db", path)
	if e != nil {
		t.Fatal(e)
	}
	var decoded map[string]any
	if json.Unmarshal([]byte(out), &decoded) != nil {
		t.Fatal(out)
	}
	results, ok := decoded["results"].(map[string]any)
	if !ok {
		results = decoded
	}
	if len(results) != 1 {
		t.Fatal(out)
	}
	if arr, ok := results["findings"].([]any); !ok || len(arr) != 0 {
		t.Fatal(out)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("local read mutated SQLite")
	}
	for _, tc := range []struct {
		name    string
		flags   rootFlags
		wantErr bool
	}{{"live forbidden", rootFlags{asJSON: true, dataSource: "live"}, true}, {"dryrun before missing file", rootFlags{asJSON: true, dryRun: true}, false}} {
		t.Run(tc.name, func(t *testing.T) {
			out, e := crocoEngineTestRun(newCrocoRelationAuditCommand(&tc.flags), "2", "--db", "/missing-audit.db")
			if (e != nil) != tc.wantErr {
				t.Fatal(out, e)
			}
			if !tc.wantErr && !strings.Contains(out, "dry_run") {
				t.Fatal(out)
			}
		})
	}
	t.Log("PASS SQLite remains unchanged, computed result selection works and local-only guards hold")
}
