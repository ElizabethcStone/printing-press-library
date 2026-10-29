// Copyright 2026 Som Samantray and contributors. Licensed under Apache-2.0. See LICENSE.

package addrkit

import (
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestNormalizeKey(t *testing.T) {
	tests := []struct{ in, want string }{
		{"Flat 4B,  Lotus Apt\t Sec 45", "flat 4b, lotus apt sec 45"},
		{"  GURGAON ", "gurgaon"},
		{"", ""},
		{"a\n\nb", "a b"},
	}
	for _, tt := range tests {
		if got := NormalizeKey(tt.in); got != tt.want {
			t.Errorf("NormalizeKey(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
	if HashKey("Sector 45  Gurgaon") != HashKey("sector 45 gurgaon") {
		t.Error("HashKey must ignore case and whitespace runs")
	}
	if HashKey("sector 45 gurgaon") == HashKey("sector 46 gurgaon") {
		t.Error("HashKey collided for different addresses")
	}
	if got := NormalizeText("Sector-45, Gurgaon (122003)"); got != "sector 45 gurgaon 122003" {
		t.Errorf("NormalizeText = %q", got)
	}
}

func TestHaversine(t *testing.T) {
	tests := []struct {
		name                   string
		lat1, lng1, lat2, lng2 float64
		want, tol              float64
	}{
		{"same point", 28.4599, 77.0679, 28.4599, 77.0679, 0, 0.001},
		{"gurgaon to jaipur", 28.4595, 77.0266, 26.9124, 75.7873, 210900, 1500},
		{"one degree of latitude", 10, 77, 11, 77, 111195, 200},
		{"short hop about 111 m", 28.0, 77.0, 28.001, 77.0, 111.2, 1},
	}
	for _, tt := range tests {
		got := Haversine(tt.lat1, tt.lng1, tt.lat2, tt.lng2)
		if math.Abs(got-tt.want) > tt.tol {
			t.Errorf("%s: Haversine = %.1f, want %.1f +/- %.1f", tt.name, got, tt.want, tt.tol)
		}
	}
}

func TestParseLatLng(t *testing.T) {
	tests := []struct {
		in       string
		lat, lng float64
		ok       bool
	}{
		{"28.4599,77.0679", 28.4599, 77.0679, true},
		{" 28.4599 , 77.0679 ", 28.4599, 77.0679, true},
		{"-12.5,-45", -12.5, -45, true},
		{"Sector 45 Gurgaon", 0, 0, false},
		{"91,77", 0, 0, false},
		{"28,181", 0, 0, false},
		{"28.1", 0, 0, false},
		{"1,2,3", 0, 0, false},
		{"Gurgaon, Haryana", 0, 0, false},
	}
	for _, tt := range tests {
		lat, lng, ok := ParseLatLng(tt.in)
		if ok != tt.ok || lat != tt.lat || lng != tt.lng {
			t.Errorf("ParseLatLng(%q) = %v,%v,%v want %v,%v,%v", tt.in, lat, lng, ok, tt.lat, tt.lng, tt.ok)
		}
	}
}

func TestChunk(t *testing.T) {
	tests := []struct {
		n, size int
		want    []int
	}{
		{0, 50, []int{}},
		{50, 50, []int{50}},
		{51, 50, []int{50, 1}},
		{120, 50, []int{50, 50, 20}},
		{5, 0, []int{1, 1, 1, 1, 1}},
	}
	for _, tt := range tests {
		items := make([]int, tt.n)
		for i := range items {
			items[i] = i
		}
		sizes := []int{}
		total := 0
		for _, c := range Chunk(items, tt.size) {
			sizes = append(sizes, len(c))
			total += len(c)
		}
		if !reflect.DeepEqual(sizes, tt.want) || total != tt.n {
			t.Errorf("Chunk(%d,%d) sizes=%v total=%d, want %v", tt.n, tt.size, sizes, total, tt.want)
		}
	}
}

func TestReadTableCSVAndJSONL(t *testing.T) {
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "in.csv")
	csvBody := "\xef\xbb\xbfid, Address ,city\n1,\"Flat 4B, Lotus Apt\",Gurgaon\n\n2,Plot 9 Jaipur\n"
	if err := os.WriteFile(csvPath, []byte(csvBody), 0o600); err != nil {
		t.Fatal(err)
	}
	tb, err := ReadTable(csvPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := tb.Col("ADDRESS"); got != 1 {
		t.Fatalf("Col(ADDRESS) = %d, want 1 (header %v)", got, tb.Header)
	}
	if len(tb.Rows) != 2 || tb.Rows[0][1] != "Flat 4B, Lotus Apt" || tb.Rows[1][2] != "" {
		t.Fatalf("rows = %v", tb.Rows)
	}

	jPath := filepath.Join(dir, "in.jsonl")
	jBody := `{"address":"Sector 45 Gurgaon","lat":28.45}` + "\n\n" + `{"address":"Jaipur","pin":302001,"note":null}` + "\n"
	if err := os.WriteFile(jPath, []byte(jBody), 0o600); err != nil {
		t.Fatal(err)
	}
	jt, err := ReadTable(jPath)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(jt.Header, []string{"address", "lat", "pin", "note"}) {
		t.Fatalf("jsonl header = %v", jt.Header)
	}
	if jt.Rows[0][1] != "28.45" || jt.Rows[1][2] != "302001" || jt.Rows[1][3] != "" || jt.Rows[1][1] != "" {
		t.Fatalf("jsonl rows = %v", jt.Rows)
	}

	bad := filepath.Join(dir, "bad.jsonl")
	if err := os.WriteFile(bad, []byte("[1,2]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadTable(bad); err == nil || !strings.Contains(err.Error(), "JSON object") {
		t.Fatalf("expected JSON object error, got %v", err)
	}
	empty := filepath.Join(dir, "empty.csv")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadTable(empty); err == nil {
		t.Fatal("expected error for empty csv")
	}
}

func TestWriteCSVRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.csv")
	header := []string{"raw", "note"}
	rows := [][]string{{"Flat 4B, Lotus Apt", "has \"quotes\""}, {"x", ""}}
	if err := WriteCSV(path, header, rows); err != nil {
		t.Fatal(err)
	}
	tb, err := ReadTable(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(tb.Header, header) || !reflect.DeepEqual(tb.Rows, rows) {
		t.Fatalf("round trip mismatch: %v %v", tb.Header, tb.Rows)
	}
}
