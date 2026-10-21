package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mvanhorn/printing-press-library/library/developer-tools/crocoblock/internal/store"
	"github.com/spf13/cobra"
)

func crocoUsageFixture() (store.CrocoCapture, []map[string]any, store.CrocoCapture, []map[string]any) {
	at := time.Now().UTC().Format(time.RFC3339Nano)
	decl := store.CrocoCapture{Site: "https://fixture.invalid/wp-json", Resource: "forms:fields:23", CapturedAt: at, Complete: true, Fields: []string{"name", "type", "label"}, Versions: map[string]string{"jetformbuilder": "3.6.7"}, Scope: map[string]any{"form_id": "23", "source": "gutenberg_raw"}}
	obs := store.CrocoCapture{Site: decl.Site, Resource: "records:23", CapturedAt: at, Complete: true, Fields: []string{"record_id", "form_id", "fields.name", "fields.present"}, Versions: map[string]string{"jetformbuilder": "3.6.7"}, Scope: map[string]any{"form_id": 23, "values": false}}
	fields := []map[string]any{{"name": "pp_reference", "type": "text-field", "label": "Reference"}, {"name": "pp_unused", "type": "text-field", "label": "Unused"}}
	records := []map[string]any{{"record_id": json.Number("1"), "form_id": json.Number("23"), "fields": []any{map[string]any{"name": "pp_reference", "present": true, "value": "do-not-output@example.invalid"}, map[string]any{"name": "pp_retired", "present": false, "value": "secret-fixture-value"}}, "user_email": "private@example.invalid"}}
	return decl, fields, obs, records
}
func crocoUsageRows(t *testing.T, report map[string]any) map[string]map[string]any {
	t.Helper()
	rows := map[string]map[string]any{}
	for _, v := range report["fields"].([]any) {
		m := v.(map[string]any)
		rows[m["name"].(string)] = m
	}
	return rows
}
func TestCrocoFieldUsageObservedCoverage(t *testing.T) {
	d, fs, o, rs := crocoUsageFixture()
	report, e := crocoFieldUsage("23", d, fs, o, rs, 100)
	if e != nil {
		t.Fatal(e)
	}
	rows := crocoUsageRows(t, report)
	if report["records_total"] != 1 || report["declared_fields_total"] != 2 || report["observed_only_fields_total"] != 1 || report["fields_total"] != 3 {
		t.Fatalf("wrong totals %v", report)
	}
	if row := rows["pp_reference"]; row["records_seen"] != 1 || row["present"] != 1 || row["empty"] != 0 || row["missing"] != 0 || row["present_percent"] != float64(100) || row["percentage_denominator"] != 1 {
		t.Fatalf("present counts %v", row)
	}
	if row := rows["pp_unused"]; row["records_seen"] != 0 || row["missing"] != 1 || row["unused_in_sample"] != true {
		t.Fatalf("unused counts %v", row)
	}
	if row := rows["pp_retired"]; row["declared"] != false || row["classification"] != "observed_only" || row["empty"] != 1 || row["missing"] != 0 {
		t.Fatalf("retired counts %v", row)
	}
	b, _ := json.Marshal(report)
	for _, needle := range []string{"do-not-output@", "private@", "secret-fixture-value"} {
		if bytes.Contains(b, []byte(needle)) {
			t.Fatalf("submitted value leaked: %s", needle)
		}
	}
}
func TestCrocoFieldUsagePresenceFalseAndMissing(t *testing.T) {
	d, fs, o, _ := crocoUsageFixture()
	rs := []map[string]any{{"record_id": 1, "form_id": 23, "fields": []any{map[string]any{"name": "pp_reference", "present": false}}}, {"record_id": 2, "form_id": 23, "fields": []any{}}}
	report, e := crocoFieldUsage("23", d, fs, o, rs, 100)
	if e != nil {
		t.Fatal(e)
	}
	row := crocoUsageRows(t, report)["pp_reference"]
	if row["present"] != 0 || row["empty"] != 1 || row["missing"] != 1 || row["records_seen"] != 1 || row["percentage_denominator"] != 2 {
		t.Fatalf("false confused with missing: %v", row)
	}
}
func TestCrocoFieldUsageEmptySample(t *testing.T) {
	d, fs, o, _ := crocoUsageFixture()
	report, e := crocoFieldUsage("23", d, fs, o, []map[string]any{}, 100)
	if e != nil {
		t.Fatal(e)
	}
	if report["empty_sample"] != true || report["records_total"] != 0 || report["fields_total"] != 2 || report["unused_declared_fields_in_sample"] != 0 {
		t.Fatalf("empty report %v", report)
	}
	for _, row := range crocoUsageRows(t, report) {
		if row["present"] != 0 || row["empty"] != 0 || row["missing"] != 0 || row["present_percent"] != nil || row["unused_in_sample"] != false {
			t.Fatalf("empty sample invented usage %v", row)
		}
	}
	b, _ := json.Marshal(report["notes"])
	if !bytes.Contains(b, []byte("sample is empty")) {
		t.Fatal("empty sample note missing")
	}
}
func TestCrocoFieldUsageLimitRetainsTotals(t *testing.T) {
	d, fs, o, rs := crocoUsageFixture()
	report, e := crocoFieldUsage("23", d, fs, o, rs, 1)
	if e != nil {
		t.Fatal(e)
	}
	if report["truncated"] != true || report["fields_total"] != 3 || report["fields_returned"] != 1 || report["records_total"] != 1 {
		t.Fatalf("truncated totals misleading %v", report)
	}
}
func TestCrocoFieldUsageRejectsUnknownCoverage(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*store.CrocoCapture, *[]map[string]any, *store.CrocoCapture, *[]map[string]any)
	}{
		{"declarations_incomplete", func(d *store.CrocoCapture, _ *[]map[string]any, _ *store.CrocoCapture, _ *[]map[string]any) {
			d.Complete = false
		}},
		{"records_incomplete", func(_ *store.CrocoCapture, _ *[]map[string]any, o *store.CrocoCapture, _ *[]map[string]any) {
			o.Complete = false
		}},
		{"different_site", func(_ *store.CrocoCapture, _ *[]map[string]any, o *store.CrocoCapture, _ *[]map[string]any) {
			o.Site = "https://other.invalid/wp-json"
		}},
		{"different_version", func(_ *store.CrocoCapture, _ *[]map[string]any, o *store.CrocoCapture, _ *[]map[string]any) {
			o.Versions["jetformbuilder"] = "3.7.0"
		}},
		{"missing_version", func(d *store.CrocoCapture, _ *[]map[string]any, _ *store.CrocoCapture, _ *[]map[string]any) {
			d.Versions = map[string]string{}
		}},
		{"missing_coverage_field", func(_ *store.CrocoCapture, _ *[]map[string]any, o *store.CrocoCapture, _ *[]map[string]any) {
			o.Fields = []string{"record_id", "form_id"}
		}},
		{"wrong_form_scope", func(_ *store.CrocoCapture, _ *[]map[string]any, o *store.CrocoCapture, _ *[]map[string]any) {
			o.Scope["form_id"] = 24
		}},
		{"values_included", func(_ *store.CrocoCapture, _ *[]map[string]any, o *store.CrocoCapture, _ *[]map[string]any) {
			o.Scope["values"] = true
		}},
		{"wrong_resource", func(d *store.CrocoCapture, _ *[]map[string]any, _ *store.CrocoCapture, _ *[]map[string]any) {
			d.Resource = "forms:fields:24"
		}},
		{"invalid_timestamp", func(d *store.CrocoCapture, _ *[]map[string]any, _ *store.CrocoCapture, _ *[]map[string]any) {
			d.CapturedAt = "yesterday"
		}},
		{"record_other_form", func(_ *store.CrocoCapture, _ *[]map[string]any, _ *store.CrocoCapture, rs *[]map[string]any) {
			(*rs)[0]["form_id"] = 24
		}},
		{"record_without_fields", func(_ *store.CrocoCapture, _ *[]map[string]any, _ *store.CrocoCapture, rs *[]map[string]any) {
			delete((*rs)[0], "fields")
		}},
		{"presence_unspecified", func(_ *store.CrocoCapture, _ *[]map[string]any, _ *store.CrocoCapture, rs *[]map[string]any) {
			(*rs)[0]["fields"] = []any{map[string]any{"name": "pp_reference"}}
		}},
		{"presence_string_false", func(_ *store.CrocoCapture, _ *[]map[string]any, _ *store.CrocoCapture, rs *[]map[string]any) {
			(*rs)[0]["fields"] = []any{map[string]any{"name": "pp_reference", "present": "false"}}
		}},
		{"duplicate_record", func(_ *store.CrocoCapture, _ *[]map[string]any, _ *store.CrocoCapture, rs *[]map[string]any) {
			*rs = append(*rs, (*rs)[0])
		}},
		{"duplicate_observed_name", func(_ *store.CrocoCapture, _ *[]map[string]any, _ *store.CrocoCapture, rs *[]map[string]any) {
			(*rs)[0]["fields"] = []any{map[string]any{"name": "pp_reference", "present": true}, map[string]any{"name": "pp_reference", "present": false}}
		}},
		{"duplicate_declared_name", func(_ *store.CrocoCapture, fs *[]map[string]any, _ *store.CrocoCapture, _ *[]map[string]any) {
			*fs = append(*fs, (*fs)[0])
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d, fs, o, rs := crocoUsageFixture()
			tc.mutate(&d, &fs, &o, &rs)
			if _, e := crocoFieldUsage("23", d, fs, o, rs, 100); e == nil {
				t.Fatal("unknown coverage reported as a successful report")
			}
		})
	}
}
func crocoUsageCommand(t *testing.T, f *rootFlags) *cobra.Command {
	t.Helper()
	root := &cobra.Command{Use: "crocoblock-pp-cli"}
	for _, hook := range novelCommandHooks {
		hook(root, f)
	}
	cmd, rest, e := root.Find([]string{"forms", "field-usage"})
	if e != nil || len(rest) > 0 {
		t.Fatal("command not attached")
	}
	cmd.SetContext(context.Background())
	return cmd
}
func TestCrocoFieldUsageDryRunAndLiveBoundary(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.db")
	for _, tc := range []struct {
		name    string
		flags   rootFlags
		wantErr bool
	}{{"dry_run", rootFlags{dryRun: true, asJSON: true, dataSource: "live"}, false}, {"live_rejected", rootFlags{asJSON: true, dataSource: "live"}, true}, {"missing_capture", rootFlags{asJSON: true, dataSource: "local"}, true}} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := crocoUsageCommand(t, &tc.flags)
			if e := cmd.Flags().Set("db", missing); e != nil {
				t.Fatal(e)
			}
			var out bytes.Buffer
			cmd.SetOut(&out)
			args := []string{"23"}
			if tc.name == "dry_run" {
				args = nil
			}
			e := cmd.RunE(cmd, args)
			if (e != nil) != tc.wantErr {
				t.Fatalf("err=%v", e)
			}
			if tc.name == "dry_run" && !strings.Contains(out.String(), "dry_run") {
				t.Fatalf("dry run missing %s", out.String())
			}
			if tc.name == "live_rejected" && !strings.Contains(e.Error(), "local snapshots") {
				t.Fatalf("wrong boundary error %v", e)
			}
		})
	}
}
func TestCrocoFieldUsageSnapshotJSONAgentSelect(t *testing.T) {
	path := filepath.Join(t.TempDir(), "snapshot.db")
	s, e := store.Open(path)
	if e != nil {
		t.Fatal(e)
	}
	d, fs, o, rs := crocoUsageFixture()
	save := func(cap store.CrocoCapture, rows []map[string]any, key string) {
		t.Helper()
		out := []store.CrocoRow{}
		for _, row := range rows {
			b, _ := json.Marshal(row)
			out = append(out, store.CrocoRow{ID: fmt.Sprint(row[key]), Data: b})
		}
		if e = s.CrocoReplace(context.Background(), cap, out); e != nil {
			t.Fatal(e)
		}
	}
	save(d, fs, "name")
	save(o, rs, "record_id")
	s.Close()
	for _, tc := range []struct {
		name  string
		flags rootFlags
	}{{"json", rootFlags{asJSON: true, dataSource: "local"}}, {"agent", rootFlags{asJSON: true, agent: true, dataSource: "auto"}}, {"select", rootFlags{asJSON: true, agent: true, dataSource: "local", selectFields: "records_total"}}} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := crocoUsageCommand(t, &tc.flags)
			if e = cmd.Flags().Set("db", path); e != nil {
				t.Fatal(e)
			}
			var out bytes.Buffer
			cmd.SetOut(&out)
			if e = cmd.RunE(cmd, []string{"23"}); e != nil {
				t.Fatal(e)
			}
			var v any
			if e = json.Unmarshal(out.Bytes(), &v); e != nil {
				t.Fatalf("invalid JSON %s", out.String())
			}
			if strings.Contains(out.String(), "do-not-output@") || strings.Contains(out.String(), "private@") {
				t.Fatal("personal values leaked")
			}
			if !strings.Contains(out.String(), "records_total") {
				t.Fatalf("result missing %s", out.String())
			}
			if tc.name == "select" && strings.Contains(out.String(), "pp_reference") {
				t.Fatalf("--select ignored: %s", out.String())
			}
		})
	}
}

func TestCrocoFieldUsageBothCollectionsEmpty(t *testing.T) {
	d, _, o, _ := crocoUsageFixture()
	report, e := crocoFieldUsage("23", d, []map[string]any{}, o, []map[string]any{}, 100)
	if e != nil {
		t.Fatal(e)
	}
	if report["fields_total"] != 0 || report["records_total"] != 0 || report["empty_sample"] != true {
		t.Fatalf("verified empty snapshots not represented: %v", report)
	}
}
