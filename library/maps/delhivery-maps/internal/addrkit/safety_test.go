// Copyright 2026 Som Samantray and contributors. Licensed under Apache-2.0. See LICENSE.

package addrkit

import (
	"errors"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestNeutralizeCell(t *testing.T) {
	tests := []struct{ in, want string }{
		{`=HYPERLINK("http://evil","click")`, `'=HYPERLINK("http://evil","click")`},
		{"+1 Main St", "'+1 Main St"},
		{"@SUM(1)", "'@SUM(1)"},
		{`=cmd|' /C calc'!A0`, `'=cmd|' /C calc'!A0`},
		{"-2+3", "'-2+3"},
		{"\t=1+1", "'\t=1+1"},
		{"\r=1+1", "'\r=1+1"},
		{"-", "'-"},
		{"+", "'+"},
		// Plain numbers are data, not formulas.
		{"-12.5", "-12.5"},
		{"+91", "+91"},
		{"-0.000123", "-0.000123"},
		{"1e-7", "1e-7"},
		{"-1E5", "-1E5"},
		// Things ParseFloat accepts but a spreadsheet would treat as formulas or text.
		{"-Inf", "'-Inf"},
		{"+NaN", "'+NaN"},
		{"-0x1p3", "'-0x1p3"},
		// Safe text is untouched.
		{"Flat 4B, Lotus Apt", "Flat 4B, Lotus Apt"},
		{"", ""},
		{"12 = 12", "12 = 12"},
		{"a=b", "a=b"},
	}
	for _, tt := range tests {
		if got := NeutralizeCell(tt.in); got != tt.want {
			t.Errorf("NeutralizeCell(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestWriteCSVNeutralizesHeadersAndCellsAndCleansUp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.csv")
	header := []string{"=evil", "lat", "note"}
	rows := [][]string{{`=HYPERLINK("http://x","y")`, "-12.5", "@SUM(1)"}, {"+1 Main St", "28.4", "plain"}}
	if err := WriteCSV(path, header, rows); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	text := string(raw)
	for _, bad := range []string{"\n=", ",=", "\n+", ",+", "\n@", ",@"} {
		if strings.Contains(text, bad) {
			t.Fatalf("an unneutralised formula cell (%q) reached the file:\n%s", bad, text)
		}
	}
	tb, err := ReadTable(path)
	if err != nil {
		t.Fatal(err)
	}
	if tb.Header[0] != "'=evil" || tb.Rows[0][0] != `'=HYPERLINK("http://x","y")` || tb.Rows[0][1] != "-12.5" || tb.Rows[0][2] != "'@SUM(1)" || tb.Rows[1][0] != "'+1 Main St" {
		t.Fatalf("round trip = %v %v", tb.Header, tb.Rows)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Fatalf("file mode = %v, want 0600", info.Mode().Perm())
	}

	// A failed write leaves neither a temp file nor a half-written target.
	missingDir := filepath.Join(dir, "nope", "out.csv")
	if err := WriteCSV(missingDir, header, rows); err == nil {
		t.Fatal("expected an error writing into a missing directory")
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".csv-") {
			t.Fatalf("temp file %s was left behind", e.Name())
		}
	}
	// A rename failure (target is a non-empty directory) also cleans up.
	target := filepath.Join(dir, "adir")
	if err := os.MkdirAll(filepath.Join(target, "child"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := WriteCSV(target, header, rows); err == nil {
		t.Fatal("expected an error renaming onto a directory")
	}
	entries, _ = os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".csv-") {
			t.Fatalf("temp file %s was left behind after a rename failure", e.Name())
		}
	}
}

func TestParseFiniteFloat(t *testing.T) {
	tests := []struct {
		in string
		ok bool
		v  float64
	}{
		{"1", true, 1}, {" 2.5 ", true, 2.5}, {"-3", true, -3}, {"+4", true, 4}, {"1e3", true, 1000}, {".5", true, 0.5},
		{"", false, 0}, {"   ", false, 0}, {"NaN", false, 0}, {"nan", false, 0}, {"Inf", false, 0}, {"-inf", false, 0}, {"+Infinity", false, 0},
		{"0x10", false, 0}, {"0x1p-2", false, 0}, {"1_000", false, 0}, {"1,5", false, 0}, {"abc", false, 0}, {"1e999", false, 0},
	}
	for _, tt := range tests {
		v, err := ParseFiniteFloat(tt.in)
		if (err == nil) != tt.ok || (tt.ok && v != tt.v) {
			t.Errorf("ParseFiniteFloat(%q) = %v, %v; want ok=%v v=%v", tt.in, v, err, tt.ok, tt.v)
		}
	}
	// Hex and non-finite values must not slip through as coordinates either.
	for _, p := range [][2]string{{"0x10", "77"}, {"28", "NaN"}, {"Inf", "77"}, {"1e999", "0"}} {
		if _, _, ok := ParseLatLngPair(p[0], p[1]); ok {
			t.Errorf("ParseLatLngPair(%q, %q) accepted a non-plain number", p[0], p[1])
		}
	}
}

func TestValidatePoints(t *testing.T) {
	tests := []struct {
		name string
		pts  [][2]float64
		ok   bool
	}{
		{"empty", nil, true},
		{"valid", [][2]float64{{28.4, 77.0}, {-33.9, 151.2}}, true},
		{"edges", [][2]float64{{90, 180}, {-90, -180}}, true},
		{"latitude too high", [][2]float64{{90.0001, 0}}, false},
		{"longitude too low", [][2]float64{{0, -180.5}}, false},
		{"NaN latitude", [][2]float64{{math.NaN(), 0}}, false},
		{"NaN longitude", [][2]float64{{0, math.NaN()}}, false},
		{"Inf", [][2]float64{{math.Inf(1), 0}}, false},
		{"second point bad", [][2]float64{{1, 1}, {200, 1}}, false},
	}
	for _, tt := range tests {
		if err := ValidatePoints(tt.pts); (err == nil) != tt.ok {
			t.Errorf("%s: err = %v, want ok=%v", tt.name, err, tt.ok)
		}
	}
}

func TestParseCSVEdgeCases(t *testing.T) {
	tests := []struct {
		name       string
		in         string
		wantHeader []string
		wantRows   [][]string
		wantErr    bool
	}{
		{"ragged rows are padded", "a,b,c\n1\n1,2,3\n", []string{"a", "b", "c"}, [][]string{{"1", "", ""}, {"1", "2", "3"}}, false},
		{"blank lines are skipped", "a,b\n\n1,2\n\n\n3,4\n", []string{"a", "b"}, [][]string{{"1", "2"}, {"3", "4"}}, false},
		{"header whitespace is trimmed", " a , b \n1,2\n", []string{"a", "b"}, [][]string{{"1", "2"}}, false},
		{"quoted newlines and commas", "a,b\n\"x, y\nz\",2\n", []string{"a", "b"}, [][]string{{"x, y\nz", "2"}}, false},
		{"header only", "a,b\n", []string{"a", "b"}, nil, false},
		{"CRLF line endings", "a,b\r\n1,2\r\n", []string{"a", "b"}, [][]string{{"1", "2"}}, false},
		{"empty input", "", nil, nil, true},
		{"unterminated quote", "a,b\n\"1,2\n", nil, nil, true},
	}
	for _, tt := range tests {
		got, err := ParseCSV(strings.NewReader(tt.in))
		if (err != nil) != tt.wantErr {
			t.Errorf("%s: err = %v", tt.name, err)
			continue
		}
		if tt.wantErr {
			continue
		}
		if !reflect.DeepEqual(got.Header, tt.wantHeader) || !reflect.DeepEqual(got.Rows, tt.wantRows) {
			t.Errorf("%s: got %v %v, want %v %v", tt.name, got.Header, got.Rows, tt.wantHeader, tt.wantRows)
		}
	}
}

func TestReadTableLimitsAndJSONArray(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}

	// A .json file holding an array of objects is accepted; so is JSON Lines under .json.
	arr, err := ReadTable(write("a.json", "\xef\xbb\xbf  [ {\"address\":\"A\",\"n\":1}, {\"address\":\"B\",\"extra\":true} ]"))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(arr.Header, []string{"address", "n", "extra"}) || arr.Rows[0][1] != "1" || arr.Rows[1][2] != "true" {
		t.Fatalf("array table = %v %v", arr.Header, arr.Rows)
	}
	lines, err := ReadTable(write("l.json", "{\"address\":\"A\"}\n{\"address\":\"B\"}\n"))
	if err != nil || len(lines.Rows) != 2 {
		t.Fatalf("json lines under .json: %v %v", lines, err)
	}
	for _, bad := range []string{`[1,2]`, `[{"a":1},"x"]`, `[{"a":`} {
		if _, err := ReadTable(write("bad.json", bad)); err == nil {
			t.Errorf("array %q should be rejected", bad)
		}
	}

	// Row limits.
	csv := write("rows.csv", "a\n1\n2\n3\n")
	if _, err := ReadTableMax(csv, 3); err != nil {
		t.Fatalf("3 rows within a limit of 3: %v", err)
	}
	_, err = ReadTableMax(csv, 2)
	if !errors.Is(err, ErrLimit) || !strings.Contains(err.Error(), "limit is 2") {
		t.Fatalf("row limit err = %v", err)
	}
	for _, n := range []int{0, -1, HardMaxRows + 1} {
		if _, err := ReadTableMax(csv, n); !errors.Is(err, ErrLimit) {
			t.Errorf("max rows %d should be refused, got %v", n, err)
		}
	}

	// Not a regular file.
	if _, err := ReadTable(dir); !errors.Is(err, ErrLimit) {
		t.Errorf("directory: err = %v", err)
	}
	if _, err := ReadTable(filepath.Join(dir, "missing.csv")); err == nil || errors.Is(err, ErrLimit) {
		t.Errorf("missing file should be a plain error, got %v", err)
	}
}

func TestReadTableRefusesOversizedFiles(t *testing.T) {
	// A sparse file reports a huge size without using disk.
	p := filepath.Join(t.TempDir(), "big.csv")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(MaxFileBytes + 1); err != nil {
		f.Close()
		t.Skipf("cannot create a sparse file here: %v", err)
	}
	f.Close()
	if _, err := ReadTable(p); !errors.Is(err, ErrLimit) || !strings.Contains(err.Error(), "bytes") {
		t.Fatalf("oversized file err = %v", err)
	}
}

func TestClusterRejectsBadInputs(t *testing.T) {
	base := LatLng{28.4, 77.0}
	near := LatLng{28.4, 77.00001}
	tests := []struct {
		name   string
		points []LatLng
		radius float64
		empty  bool
	}{
		{"NaN radius", []LatLng{base, near}, math.NaN(), true},
		{"Inf radius", []LatLng{base, near}, math.Inf(1), true},
		{"negative radius", []LatLng{base, near}, -5, true},
		{"absurd radius", []LatLng{base, near}, MaxClusterRadiusM + 1, true},
		{"NaN coordinate", []LatLng{base, {math.NaN(), 77}}, 30, true},
		{"largest allowed radius", []LatLng{base, near}, MaxClusterRadiusM, false},
	}
	for _, tt := range tests {
		got := ClusterByRadius(tt.points, tt.radius)
		if got == nil || (len(got) == 0) != tt.empty {
			t.Errorf("%s: got %v, want empty=%v (and never nil)", tt.name, got, tt.empty)
		}
	}
}

// TestClusterNeverMissesPairsJustInsideTheRadius places pairs at 0.9995 of the
// radius in several directions and at several latitudes, including pairs
// straddling grid-cell boundaries; every one must be found.
func TestClusterNeverMissesPairsJustInsideTheRadius(t *testing.T) {
	const radius = 500.0
	const frac = 0.9995
	for _, lat := range []float64{0, 8.5, 28.4599, 34.0, 60.0, 75.0} {
		for _, bearingDeg := range []float64{0, 30, 45, 90, 135, 180, 225, 270, 315} {
			for _, jitter := range []float64{0, 0.17, 0.5, 0.83, 0.999} {
				// Start at a point offset within a cell, then walk radius*frac along the bearing.
				a := LatLng{lat + jitter*radius/111195, 77.0 + jitter*radius/(111195*math.Cos(lat*math.Pi/180))}
				b := walk(a, radius*frac, bearingDeg)
				if d := Haversine(a.Lat, a.Lng, b.Lat, b.Lng); d > radius {
					t.Fatalf("test setup: pair is %.3f m apart, want <= %v", d, radius)
				}
				got := ClusterByRadius([]LatLng{a, b}, radius)
				if len(got) != 1 || len(got[0]) != 2 {
					t.Errorf("pair at 0.9995*radius missed: lat=%v bearing=%v jitter=%v -> %v", lat, bearingDeg, jitter, got)
				}
			}
		}
	}
	// And pairs just outside the radius are not merged.
	a := LatLng{28.4, 77.0}
	if got := ClusterByRadius([]LatLng{a, walk(a, radius*1.01, 90)}, radius); len(got) != 0 {
		t.Errorf("a pair 1%% beyond the radius was merged: %v", got)
	}
}

// walk moves from p by dist metres on a bearing (degrees from north), using
// the same sphere as Haversine.
func walk(p LatLng, dist, bearingDeg float64) LatLng {
	const r = 6371008.8
	rad := math.Pi / 180
	lat1, lng1, brg, d := p.Lat*rad, p.Lng*rad, bearingDeg*rad, dist/r
	lat2 := math.Asin(math.Sin(lat1)*math.Cos(d) + math.Cos(lat1)*math.Sin(d)*math.Cos(brg))
	lng2 := lng1 + math.Atan2(math.Sin(brg)*math.Sin(d)*math.Cos(lat1), math.Cos(d)-math.Sin(lat1)*math.Sin(lat2))
	return LatLng{lat2 / rad, lng2 / rad}
}

// endlessReader yields a header and then data rows forever, counting the bytes
// it handed out, so a test can prove a parser stops early.
type endlessReader struct {
	header, row string
	buf         []byte
	read        int
	started     bool
}

func (r *endlessReader) Read(p []byte) (int, error) {
	if len(r.buf) == 0 {
		if !r.started {
			r.buf, r.started = []byte(r.header), true
		} else {
			r.buf = []byte(r.row)
		}
	}
	n := copy(p, r.buf)
	r.buf = r.buf[n:]
	r.read += n
	return n, nil
}

func TestRowLimitStopsParsingEarly(t *testing.T) {
	tests := []struct {
		name  string
		r     *endlessReader
		parse func(*endlessReader, int) (*Table, error)
	}{
		{"csv", &endlessReader{header: "address,city\n", row: "Flat 4B Lotus Apt Sector 45,Gurgaon\n"}, func(r *endlessReader, n int) (*Table, error) { return ParseCSVMax(r, n) }},
		{"jsonl", &endlessReader{header: "", row: "{\"address\":\"Flat 4B Lotus Apt Sector 45\",\"city\":\"Gurgaon\"}\n"}, func(r *endlessReader, n int) (*Table, error) { return ParseJSONLMax(r, n) }},
		{"json array", &endlessReader{header: "[", row: "{\"address\":\"Flat 4B Lotus Apt Sector 45\"},"}, func(r *endlessReader, n int) (*Table, error) { return ParseJSONArrayMax(r, n) }},
	}
	for _, tt := range tests {
		_, err := tt.parse(tt.r, 10)
		if !errors.Is(err, ErrLimit) {
			t.Errorf("%s: err = %v, want ErrLimit", tt.name, err)
		}
		// Ten rows plus buffering: well under 1 MiB of an endless input.
		if tt.r.read > 1<<20 {
			t.Errorf("%s: the parser consumed %d bytes of an endless input; the row limit must stop it early", tt.name, tt.r.read)
		}
	}
	// Exactly maxRows rows is allowed; one more is not; blank lines do not count.
	tb, err := ParseCSVMax(strings.NewReader("a\n1\n\n2\n\n"), 2)
	if err != nil || len(tb.Rows) != 2 {
		t.Fatalf("2 rows within a limit of 2: %v %v", tb, err)
	}
	if _, err := ParseCSVMax(strings.NewReader("a\n1\n2\n3\n"), 2); !errors.Is(err, ErrLimit) {
		t.Fatalf("3 rows over a limit of 2: %v", err)
	}
	if _, err := ParseJSONLMax(strings.NewReader("{\"a\":1}\n{\"a\":2}\n{\"a\":3}\n"), 2); !errors.Is(err, ErrLimit) {
		t.Fatalf("jsonl over limit: %v", err)
	}
	if _, err := ParseJSONArrayMax(strings.NewReader(`[{"a":1},{"a":2},{"a":3}]`), 2); !errors.Is(err, ErrLimit) {
		t.Fatalf("json array over limit: %v", err)
	}
}

func TestWriteCSVModeNeverReplacesAnExistingFileWithoutOverwrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.csv")
	if err := os.WriteFile(path, []byte("MINE"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := WriteCSVMode(path, []string{"a"}, [][]string{{"1"}}, false)
	if !errors.Is(err, ErrExists) {
		t.Fatalf("err = %v, want ErrExists", err)
	}
	if b, _ := os.ReadFile(path); string(b) != "MINE" {
		t.Fatalf("the existing file was replaced: %q", b)
	}
	if err := WriteCSVMode(path, []string{"a"}, [][]string{{"1"}}, true); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); !strings.HasPrefix(string(b), "a\n1") {
		t.Fatalf("overwrite did not replace: %q", b)
	}
	// A fresh target is created without overwrite, 0600, with no temp file left.
	fresh := filepath.Join(dir, "fresh.csv")
	if err := WriteCSVMode(fresh, []string{"a"}, [][]string{{"=1+1"}}, false); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(fresh); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("fresh file: %v %v", info, err)
	}
	if b, _ := os.ReadFile(fresh); !strings.Contains(string(b), "'=1+1") {
		t.Fatalf("formula not neutralised on the no-overwrite path: %q", b)
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".csv-") {
			t.Fatalf("temp file %s left behind", e.Name())
		}
	}
}

func TestNormalizeTextKeepsCombiningMarks(t *testing.T) {
	tests := []struct{ in, want string }{
		{"सेक्टर 45, गुड़गांव", "सेक्टर 45 गुड़गांव"},
		{"Sector-45, Gurgaon", "sector 45 gurgaon"},
		{"Café Roma", "café roma"},
	}
	for _, tt := range tests {
		if got := NormalizeText(tt.in); got != tt.want {
			t.Errorf("NormalizeText(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
	// Distinct words stay distinct: stripping the vowel signs would make these collide.
	if NormalizeText("सेक्टर") == NormalizeText("सक्टर") {
		t.Error("different Devanagari words compare equal")
	}
	if got := NormalizeText("सेक्टर"); len(strings.Fields(got)) != 1 {
		t.Errorf("a single word was split into %q", got)
	}
}

// addendum G: small boundary rows for the pure helpers.
func TestHelperBoundaries(t *testing.T) {
	// JSON null cell becomes an empty string.
	tb, err := ParseJSONL(strings.NewReader(`{"a":null,"b":"x"}` + "\n"))
	if err != nil || tb.Rows[0][tb.Col("a")] != "" || tb.Rows[0][tb.Col("b")] != "x" {
		t.Fatalf("null cell: %v %v", tb, err)
	}
	// Fewer than three points is never a polygon.
	if PointInRing(0.5, 0.5, Ring{{0, 0}, {1, 1}}) || PointInRing(0, 0, nil) {
		t.Error("PointInRing with fewer than 3 points must be false")
	}
	// Precision above 9 is rejected, 9 is accepted.
	if _, err := DecodePolyline("_p~iF~ps|U", 10); err == nil {
		t.Error("precision 10 must be rejected")
	}
	if _, err := DecodePolyline("_p~iF~ps|U", 9); err != nil {
		t.Errorf("precision 9: %v", err)
	}
	if _, err := DecodePolyline("_p~iF~ps|U", -1); err == nil {
		t.Error("negative precision must be rejected")
	}
	// The radius is inclusive: a pair exactly at the radius clusters, a hair beyond does not.
	a := LatLng{28.4, 77.0}
	b := walk(a, 100, 0)
	d := Haversine(a.Lat, a.Lng, b.Lat, b.Lng)
	if got := ClusterByRadius([]LatLng{a, b}, d); len(got) != 1 {
		t.Errorf("a pair exactly at the radius (%v m) must cluster: %v", d, got)
	}
	if got := ClusterByRadius([]LatLng{a, b}, d-0.01); len(got) != 0 {
		t.Errorf("a pair just beyond the radius must not cluster: %v", got)
	}
}

func TestReadTableReaderSniffsFormatsAndEnforcesLimits(t *testing.T) {
	tests := []struct {
		name, in string
		header   []string
		rows     int
	}{
		{"csv", "address,city\nA,Gurgaon\nB,Jaipur\n", []string{"address", "city"}, 2},
		{"csv with BOM and leading blank", "\ufeffaddress\nA\n", []string{"address"}, 1},
		{"jsonl", "{\"address\":\"A\"}\n{\"address\":\"B\",\"x\":1}\n", []string{"address", "x"}, 2},
		{"single json object", `{"address":"A"}`, []string{"address"}, 1},
		{"json array", "\n  [{\"address\":\"A\"},{\"address\":\"B\"}]  \n", []string{"address"}, 2},
	}
	for _, tt := range tests {
		got, err := ReadTableReader(strings.NewReader(tt.in), 10)
		if err != nil {
			t.Errorf("%s: %v", tt.name, err)
			continue
		}
		if !reflect.DeepEqual(got.Header, tt.header) || len(got.Rows) != tt.rows {
			t.Errorf("%s: header=%v rows=%d, want %v %d", tt.name, got.Header, len(got.Rows), tt.header, tt.rows)
		}
	}
	for _, empty := range []string{"", "   ", "\n\t \n", "\ufeff"} {
		if _, err := ReadTableReader(strings.NewReader(empty), 10); !errors.Is(err, ErrNoInput) {
			t.Errorf("%q: err = %v, want ErrNoInput", empty, err)
		}
	}
	// Row cap for each format, with exactly the cap accepted.
	for name, in := range map[string]string{
		"csv": "a\n1\n2\n3\n", "jsonl": "{\"a\":1}\n{\"a\":2}\n{\"a\":3}\n", "array": `[{"a":1},{"a":2},{"a":3}]`,
	} {
		if _, err := ReadTableReader(strings.NewReader(in), 3); err != nil {
			t.Errorf("%s: 3 rows within a cap of 3: %v", name, err)
		}
		if _, err := ReadTableReader(strings.NewReader(in), 2); !errors.Is(err, ErrLimit) {
			t.Errorf("%s: 3 rows over a cap of 2: %v", name, err)
		}
	}
	if _, err := ReadTableReader(strings.NewReader("a\n1\n"), 0); !errors.Is(err, ErrLimit) {
		t.Errorf("max rows 0 must be refused: %v", err)
	}
	// An endless stream is cut off at the byte limit, not read forever.
	endless := &endlessReader{header: "a\n", row: strings.Repeat("x", 1000) + "\n"}
	if _, err := ReadTableReader(endless, 10); !errors.Is(err, ErrLimit) {
		t.Errorf("oversized stdin: %v", err)
	}
	if endless.read > MaxFileBytes+2*1024*1024 {
		t.Errorf("read %d bytes of an endless stream; the limit is %d", endless.read, MaxFileBytes)
	}
	if _, err := ReadTableReader(strings.NewReader("[1,2]"), 10); err == nil {
		t.Error("a JSON array of non-objects must be rejected")
	}
}
