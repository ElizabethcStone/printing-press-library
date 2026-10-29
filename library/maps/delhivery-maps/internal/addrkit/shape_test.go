// Copyright 2026 Som Samantray and contributors. Licensed under Apache-2.0. See LICENSE.

package addrkit

import (
	"math"
	"testing"
)

func TestDownsample(t *testing.T) {
	mk := func(n int) [][2]float64 {
		pts := make([][2]float64, n)
		for i := range pts {
			pts[i] = [2]float64{float64(i), float64(i) * 2}
		}
		return pts
	}
	tests := []struct {
		n, max, wantLen int
	}{
		{5, 2000, 5},
		{2000, 2000, 2000},
		{10000, 2000, 2000},
		{2001, 2000, 2000},
		{10, 3, 3},
		{10, 0, 2},
	}
	for _, tt := range tests {
		in := mk(tt.n)
		got := Downsample(in, tt.max)
		if len(got) != tt.wantLen {
			t.Errorf("Downsample(%d,%d) len = %d, want %d", tt.n, tt.max, len(got), tt.wantLen)
			continue
		}
		if got[0] != in[0] || got[len(got)-1] != in[len(in)-1] {
			t.Errorf("Downsample(%d,%d) lost an endpoint", tt.n, tt.max)
		}
		for i := 1; i < len(got); i++ {
			if got[i][0] <= got[i-1][0] {
				t.Errorf("Downsample(%d,%d) not strictly ordered at %d", tt.n, tt.max, i)
				break
			}
		}
	}
}

func TestPolygonContainsLngLatOrder(t *testing.T) {
	// GeoJSON order: [lng, lat]. A box spanning lng 77.0..77.2 and lat 28.4..28.6.
	box := Ring{{77.0, 28.4}, {77.2, 28.4}, {77.2, 28.6}, {77.0, 28.6}, {77.0, 28.4}}
	hole := Ring{{77.08, 28.48}, {77.12, 28.48}, {77.12, 28.52}, {77.08, 28.52}}
	poly := Polygon{box, hole}
	tests := []struct {
		name     string
		lat, lng float64
		want     bool
	}{
		{"inside", 28.45, 77.03, true},
		{"east of the box", 28.5, 77.5, false},
		{"north of the box", 28.9, 77.1, false},
		{"inside the hole", 28.5, 77.1, false},
		{"swapped axes would land far away", 77.1, 28.5, false},
	}
	for _, tt := range tests {
		// Callers hold (lat, lng); the ring wants (x=lng, y=lat).
		if got := poly.Contains(tt.lng, tt.lat); got != tt.want {
			t.Errorf("%s: Contains(lng=%v, lat=%v) = %v, want %v", tt.name, tt.lng, tt.lat, got, tt.want)
		}
	}
	if (Polygon{}).Contains(1, 1) {
		t.Error("empty polygon must contain nothing")
	}
	if PointInRing(0, 0, Ring{{0, 0}, {1, 1}}) {
		t.Error("degenerate ring must contain nothing")
	}
	multi := MultiPolygon{poly, {Ring{{80, 10}, {81, 10}, {81, 11}, {80, 11}}}}
	if !multi.Contains(80.5, 10.5) || multi.Contains(79, 10.5) {
		t.Error("MultiPolygon membership wrong")
	}
}

func TestPointInRingConcave(t *testing.T) {
	// An L shape: the notch at the top right must be outside.
	l := Ring{{0, 0}, {4, 0}, {4, 2}, {2, 2}, {2, 4}, {0, 4}}
	tests := []struct {
		x, y float64
		want bool
	}{
		{1, 1, true}, {3, 1, true}, {1, 3, true}, {3, 3, false}, {5, 1, false}, {-1, 1, false},
	}
	for _, tt := range tests {
		if got := PointInRing(tt.x, tt.y, l); got != tt.want {
			t.Errorf("PointInRing(%v,%v) = %v, want %v", tt.x, tt.y, got, tt.want)
		}
	}
}

func TestDecodePolyline(t *testing.T) {
	// The canonical Google example, precision 5.
	pts, err := DecodePolyline("_p~iF~ps|U_ulLnnqC_mqNvxq`@", 5)
	if err != nil {
		t.Fatal(err)
	}
	want := [][2]float64{{38.5, -120.2}, {40.7, -120.95}, {43.252, -126.453}}
	if len(pts) != len(want) {
		t.Fatalf("decoded %d points, want %d", len(pts), len(want))
	}
	for i := range want {
		if math.Abs(pts[i][0]-want[i][0]) > 1e-9 || math.Abs(pts[i][1]-want[i][1]) > 1e-9 {
			t.Errorf("point %d = %v, want %v", i, pts[i], want[i])
		}
	}
	if _, err := DecodePolyline("_p~iF~ps|U_ulLn", 5); err == nil {
		t.Error("expected truncation error")
	}
	if _, err := DecodePolyline("abc", 12); err == nil {
		t.Error("expected precision error")
	}
}
