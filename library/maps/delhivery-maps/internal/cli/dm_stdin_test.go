// Copyright 2026 Som Samantray and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/printing-press-library/library/maps/delhivery-maps/internal/addrkit"
)

// happyPath is what the live-dogfood runner does for a command: take the first
// runnable Example line, overlay the pp:happy-args flags (replace a matching
// flag or add it), feed pp:happy-stdin on standard input and append --stdin.
type happyPath struct {
	args  []string
	stdin string
}

func happyPathFor(t *testing.T, path ...string) happyPath {
	t.Helper()
	root := RootCmd()
	leaf, _, err := root.Find(path)
	if err != nil || leaf == nil {
		t.Fatalf("%v: %v", path, err)
	}
	var first string
	for _, line := range strings.Split(leaf.Example, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) > len(path) && fields[0] == "delhivery-maps-pp-cli" && strings.Join(fields[1:1+len(path)], " ") == strings.Join(path, " ") {
			first = strings.TrimSpace(line)
			break
		}
	}
	if first == "" {
		t.Fatalf("%v has no runnable Example line", path)
	}
	args := strings.Fields(first)[1:]
	if raw := leaf.Annotations["pp:happy-args"]; raw != "" {
		for _, tok := range strings.Split(raw, ";") {
			name, value, _ := strings.Cut(tok, "=")
			replaced := false
			for i, a := range args {
				if a == name && i+1 < len(args) {
					args[i+1], replaced = value, true
					break
				}
				if strings.HasPrefix(a, name+"=") {
					args[i], replaced = name+"="+value, true
					break
				}
			}
			if !replaced {
				args = append(args, name, value)
			}
		}
	}
	stdin := leaf.Annotations["pp:happy-stdin"]
	if stdin != "" {
		if !json.Valid([]byte(stdin)) {
			t.Fatalf("%v: pp:happy-stdin is not valid JSON, the runner would reject it: %q", path, stdin)
		}
		has := false
		for _, a := range args {
			if a == "--stdin" {
				has = true
			}
		}
		if !has {
			args = append(args, "--stdin")
		}
	}
	return happyPath{args: args, stdin: stdin}
}

func TestAnnotationsAreRunnableAndNeverPlaceholders(t *testing.T) {
	for _, path := range [][]string{{"address", "clean"}, {"coverage", "check"}, {"trip", "quote"}, {"fleet", "plan"}} {
		root := RootCmd()
		leaf, _, _ := root.Find(path)
		for k, v := range leaf.Annotations {
			if strings.Contains(v, "example-value") {
				t.Errorf("%v %s = %q: a placeholder marks the command blocked-fixture and leaves it hollow", path, k, v)
			}
		}
		hp := happyPathFor(t, path...)
		for _, a := range hp.args {
			// --file/--out values would be skipped by the runner (file-fixture-required) or write into its cwd.
			if a == "--file" || a == "--out" || strings.HasPrefix(a, "--file=") || strings.HasPrefix(a, "--out=") || a == "--orders" {
				t.Errorf("%v: the runner's happy args use %s; it must need neither an input file nor an output file: %v", path, a, hp.args)
			}
		}
		parent, _, _ := root.Find(path[:1])
		if parent.Annotations["pp:parent-group"] != "true" || parent.Annotations["pp:happy-args"] != "" {
			t.Errorf("parent %s must be a pp:parent-group with no happy-args (the runner would run it too): %v", path[0], parent.Annotations)
		}
	}
}

// TestHappyPathsRunEndToEndWithinTheQuota replays each runner happy path
// against a mock: exit 0, valid JSON with rows, the declared call counts on the
// first run and zero calls on the repeat (json_fidelity) run where the cache applies.
func TestHappyPathsRunEndToEndWithinTheQuota(t *testing.T) {
	cases := []struct {
		name         []string
		first, again int
		rowsKey      string
		keys         []string
	}{
		{[]string{"address", "clean"}, 1, 0, "cleaned", []string{"cleaned", "calls_made", "ok"}},
		{[]string{"coverage", "check"}, 1, 0, "flagged", []string{"flagged", "inside", "outside"}},
		{[]string{"trip", "quote"}, 2, 0, "", []string{"distance_km", "total_toll_inr", "plazas"}},
		{[]string{"fleet", "plan"}, 1, 1, "stops", []string{"stops", "assigned", "planned_calls"}},
	}
	for _, c := range cases {
		t.Run(strings.Join(c.name, " "), func(t *testing.T) {
			m := newDMMock(t)
			t.Setenv("DELHIVERY_MAPS_DATA_DIR", t.TempDir())
			squareIsochroneAround(m, 28.4599, 77.0679)
			mockRouteAndToll(m, [][]float64{{28.5954, 77.0319}, {28.4089, 77.3178}})
			m.on(epRoutePlan, echoRoutePlan)
			hp := happyPathFor(t, c.name...)
			run := func() map[string]any {
				t.Helper()
				stdout, stderr, err := runCLIStdin(t, hp.stdin, append(append([]string{}, c.name...), append(hp.args[len(c.name):], "--json")...)...)
				if err != nil {
					t.Fatalf("exit != 0: %v\nstderr: %s", err, stderr)
				}
				var v map[string]any
				if err := json.Unmarshal([]byte(stdout), &v); err != nil {
					t.Fatalf("invalid JSON: %v\n%s", err, stdout)
				}
				for _, k := range c.keys {
					if _, ok := v[k]; !ok {
						t.Fatalf("missing key %q in %s", k, stdout)
					}
				}
				if c.rowsKey != "" {
					if rows, _ := v[c.rowsKey].([]any); len(rows) == 0 {
						t.Fatalf("%q is empty: the command must return its rows when --out is omitted: %s", c.rowsKey, stdout)
					}
				}
				return v
			}
			run()
			if m.total() != c.first {
				t.Fatalf("first run made %d call(s), want %d", m.total(), c.first)
			}
			run()
			if got := m.total() - c.first; got != c.again {
				t.Fatalf("repeat run made %d call(s), want %d", got, c.again)
			}
		})
	}
}

// squareIsochroneAround answers /isochrone with a ~5 km box around a hub.
func squareIsochroneAround(m *dmMock, lat, lng float64) {
	m.on(epIsochrone, func(int, map[string]any) (int, any) {
		d := 0.05
		return http.StatusOK, map[string]any{
			"type": "FeatureCollection", "request_id": "x", "status": true,
			"features": []any{map[string]any{"type": "Feature", "properties": map[string]any{"cost_type": "time"},
				"geometry": map[string]any{"type": "Polygon", "coordinates": [][][]float64{{{lng - d, lat - d}, {lng + d, lat - d}, {lng + d, lat + d}, {lng - d, lat + d}, {lng - d, lat - d}}}}}},
		}
	})
}

func TestStdinInputFormatsAndLimits(t *testing.T) {
	newDMMock(t)
	t.Setenv("DELHIVERY_MAPS_DATA_DIR", t.TempDir())
	dir := t.TempDir()
	base := func(db string) []string {
		return []string{"address", "clean", "--stdin", "--steps", "geocode", "--db", filepath.Join(dir, db), "--json"}
	}
	rows := func(stdout string) int {
		var v struct {
			Rows int `json:"rows"`
			OK   int `json:"ok"`
		}
		if err := json.Unmarshal([]byte(stdout), &v); err != nil {
			t.Fatal(err)
		}
		return v.Rows
	}
	inputs := map[string]string{
		"csv":          "address,city\nFlat 1 Gurgaon,Gurgaon\nFlat 2 Gurgaon,Gurgaon\n",
		"csv with BOM": "\ufeffaddress\nFlat 1 Gurgaon\nFlat 2 Gurgaon\n",
		"jsonl":        "{\"address\":\"Flat 1 Gurgaon\"}\n{\"address\":\"Flat 2 Gurgaon\"}\n",
		"json array":   "  [{\"address\":\"Flat 1 Gurgaon\"},{\"address\":\"Flat 2 Gurgaon\"}]\n",
	}
	for name, in := range inputs {
		stdout, stderr, err := runCLIStdin(t, in, base(name+".db")...)
		if err != nil || rows(stdout) != 2 {
			t.Errorf("%s: err=%v rows=%d stderr=%s", name, err, rows(stdout), stderr)
		}
	}
	// --file - is the same as --stdin.
	stdout, _, err := runCLIStdin(t, inputs["csv"], "address", "clean", "--file", "-", "--steps", "geocode", "--db", filepath.Join(dir, "dash.db"), "--json")
	if err != nil || rows(stdout) != 2 {
		t.Fatalf("--file -: %v %s", err, stdout)
	}
	// Empty and whitespace-only stdin, and conflicting sources, are usage errors.
	for name, in := range map[string]string{"empty": "", "blank": "  \n\t\n"} {
		_, _, err := runCLIStdin(t, in, base(name+".db")...)
		if err == nil || ExitCode(err) != 2 || !strings.Contains(err.Error(), "standard input") {
			t.Errorf("%s stdin: want exit 2 naming standard input, got %v", name, err)
		}
	}
	if _, _, err := runCLIStdin(t, inputs["csv"], "address", "clean", "--stdin", "--file", "x.csv", "--db", filepath.Join(dir, "c.db")); err == nil || ExitCode(err) != 2 {
		t.Errorf("--stdin with --file: %v", err)
	}
	// The row cap applies to stdin.
	_, _, err = runCLIStdin(t, inputs["csv"], append(base("cap.db"), "--max-rows", "1")...)
	if err == nil || ExitCode(err) != 2 || !strings.Contains(err.Error(), "limit is 1") {
		t.Errorf("--max-rows on stdin: %v", err)
	}
	// Stdin is not a file, so the SameFile/--out safety is not tripped by "-".
	out := filepath.Join(dir, "o.csv")
	if _, _, err := runCLIStdin(t, inputs["csv"], append(base("out.db"), "--out", out)...); err != nil {
		t.Errorf("--stdin with --out: %v", err)
	}
}

func TestOutIsOptionalAndRowsComeBackInTheJSON(t *testing.T) {
	m := newDMMock(t)
	t.Setenv("DELHIVERY_MAPS_DATA_DIR", t.TempDir())
	squareIsochroneAround(m, 28.4599, 77.0679)
	m.on(epRoutePlan, echoRoutePlan)
	dir := t.TempDir()

	// address clean: typed rows, no file written.
	stdout, _, err := runCLIStdin(t, "address\nFlat 4B Lotus Apt Gurgaon\n", "address", "clean", "--stdin", "--db", filepath.Join(dir, "a.db"), "--json")
	if err != nil {
		t.Fatal(err)
	}
	var clean struct {
		Out     string `json:"out"`
		Cleaned []struct {
			Raw         string   `json:"raw"`
			Quality     string   `json:"quality"`
			Lat         *float64 `json:"lat"`
			ErrorRadius *int64   `json:"error_radius"`
			Status      string   `json:"status"`
		} `json:"cleaned"`
	}
	if err := json.Unmarshal([]byte(stdout), &clean); err != nil || len(clean.Cleaned) != 1 {
		t.Fatalf("cleaned rows: %v %s", err, stdout)
	}
	if r := clean.Cleaned[0]; r.Raw != "Flat 4B Lotus Apt Gurgaon" || r.Quality != "ok" || r.Lat == nil || r.ErrorRadius == nil || *r.ErrorRadius != 25 || r.Status != "ok" || clean.Out != "" {
		t.Fatalf("row = %+v", r)
	}
	entries, _ := filepath.Glob(filepath.Join(dir, "*.csv"))
	if len(entries) != 0 {
		t.Fatalf("a file was written without --out: %v", entries)
	}
	// With --out the rows go to the file and are not repeated in the JSON.
	stdout, _, _ = runCLIStdin(t, "address\nFlat 4B Lotus Apt Gurgaon\n", "address", "clean", "--stdin", "--out", filepath.Join(dir, "o.csv"), "--db", filepath.Join(dir, "a.db"), "--json")
	if strings.Contains(stdout, `"cleaned"`) {
		t.Fatalf("rows duplicated in the JSON when --out is given: %s", stdout)
	}
	// --csv without --out prints the rows as CSV (formulas neutralised).
	stdout, _, err = runCLIStdin(t, "address\n=1+1 Main St\n", "address", "clean", "--stdin", "--steps", "geocode", "--csv", "--db", filepath.Join(dir, "b.db"))
	if err != nil || !strings.HasPrefix(stdout, "raw,formatted,quality,") || !strings.Contains(stdout, "'=1+1 Main St") {
		t.Fatalf("csv output: %v\n%s", err, stdout)
	}
	// Human output prints a table.
	stdout, _, _ = runCLIStdin(t, "address\nFlat 4B Lotus Apt Gurgaon\n", "address", "clean", "--stdin", "--human-friendly", "--db", filepath.Join(dir, "a.db"))
	if !strings.Contains(stdout, "RAW") || !strings.Contains(stdout, "Flat 4B Lotus Apt Gurgaon") {
		t.Fatalf("human output: %s", stdout)
	}

	// coverage check: flagged rows with a boolean inside.
	stdout, _, err = runCLIStdin(t, "lat,lng\n28.4650,77.0700\n28.9,77.9\n", "coverage", "check", "--hub", "28.4599,77.0679", "--minutes", "20", "--stdin", "--db", filepath.Join(dir, "c.db"), "--json")
	if err != nil {
		t.Fatal(err)
	}
	var cov struct {
		Flagged []map[string]any `json:"flagged"`
	}
	if err := json.Unmarshal([]byte(stdout), &cov); err != nil || len(cov.Flagged) != 2 {
		t.Fatalf("flagged: %v %s", err, stdout)
	}
	if cov.Flagged[0]["inside"] != true || cov.Flagged[1]["inside"] != false || cov.Flagged[0]["geocode_status"] != "given" || cov.Flagged[0]["lat"] != "28.4650" {
		t.Fatalf("flagged rows = %v", cov.Flagged)
	}
	stdout, _, _ = runCLIStdin(t, "lat,lng\n28.4650,77.0700\n", "coverage", "check", "--hub", "28.4599,77.0679", "--minutes", "20", "--stdin", "--csv", "--db", filepath.Join(dir, "c.db"))
	if !strings.HasPrefix(stdout, "lat,lng,inside,geocode_status") || !strings.Contains(stdout, "28.4650,77.0700,true,") {
		t.Fatalf("coverage csv: %s", stdout)
	}

	// trip quote lanes from stdin; fleet plan orders from stdin (stops in the JSON).
	mockRouteAndToll(m, [][]float64{{28.4, 77.0}, {26.9, 75.7}})
	stdout, _, err = runCLIStdin(t, "from,to\n\"28.4,77.0\",\"26.9,75.7\"\n", "trip", "quote", "--stdin", "--db", filepath.Join(dir, "t.db"), "--json")
	if err != nil || len(decodeArray(t, stdout)) != 1 {
		t.Fatalf("trip lanes from stdin: %v %s", err, stdout)
	}
	stdout, _, err = runCLIStdin(t, `[{"order_id":"A1","lat":28.4,"lng":77.0},{"order_id":"A2","lat":28.5,"lng":77.1}]`, "fleet", "plan", "--orders", "-", "--vehicles", "1", "--depot", "28.4,77.0", "--db", filepath.Join(dir, "f.db"), "--json")
	if err != nil {
		t.Fatal(err)
	}
	if sum := decodeObject(t, stdout); len(sum["stops"].([]any)) != 2 {
		t.Fatalf("fleet stops: %s", stdout)
	}
}

func TestWriteCSVStreamRoundTrip(t *testing.T) {
	var b strings.Builder
	if err := addrkit.WriteCSVStream(&b, []string{"=h", "a"}, [][]string{{"+1 x", "-2.5"}, {"@x", "ok"}}); err != nil {
		t.Fatal(err)
	}
	if b.String() != "'=h,a\n'+1 x,-2.5\n'@x,ok\n" {
		t.Fatalf("stream = %q", b.String())
	}
}

// A 2xx route-plan reply with an unusable body is never re-sent: route-plan has
// a 5-call daily limit, and replaying a request that was accepted would burn it.
func TestUnusableRoutePlanReplyIsNotRetried(t *testing.T) {
	m := newDMMock(t)
	t.Setenv("DELHIVERY_MAPS_DATA_DIR", t.TempDir())
	m.on(epRoutePlan, func(int, map[string]any) (int, any) { return http.StatusOK, map[string]any{"plan_status": true} })
	_, _, err := runCLIStdin(t, `[{"order_id":"A","lat":28.4,"lng":77.0}]`, "fleet", "plan", "--stdin", "--vehicles", "1", "--depot", "28.4,77.0", "--json")
	if err == nil || ExitCode(err) != 5 {
		t.Fatalf("an unusable plan must fail the chunk (exit 5), got %v", err)
	}
	if m.count(epRoutePlan) != 1 {
		t.Fatalf("route-plan hits = %d, want exactly 1", m.count(epRoutePlan))
	}
}
