// Copyright 2026 Som Samantray and contributors. Licensed under Apache-2.0. See LICENSE.

package addrkit

import (
	"math"
	"reflect"
	"testing"
)

// metresNorth shifts a latitude by roughly the given metres.
func metresNorth(lat, m float64) float64 { return lat + m/111195.0 }

func TestClusterByRadius(t *testing.T) {
	base := LatLng{28.4595, 77.0266}
	tests := []struct {
		name   string
		points []LatLng
		radius float64
		want   [][]int
	}{
		{"nothing", nil, 30, [][]int{}},
		{"single", []LatLng{base}, 30, [][]int{}},
		{"two within 30m", []LatLng{base, {metresNorth(base.Lat, 20), base.Lng}}, 30, [][]int{{0, 1}}},
		{"5 km apart are not grouped", []LatLng{base, {metresNorth(base.Lat, 5000), base.Lng}}, 30, [][]int{}},
		{"just outside the radius", []LatLng{base, {metresNorth(base.Lat, 45), base.Lng}}, 30, [][]int{}},
		{"chain links across hops", []LatLng{base, {metresNorth(base.Lat, 25), base.Lng}, {metresNorth(base.Lat, 50), base.Lng}}, 30, [][]int{{0, 1, 2}}},
		{"two separate clusters", []LatLng{base, {metresNorth(base.Lat, 10), base.Lng}, {26.9124, 75.7873}, {26.9124, 75.78731}}, 30, [][]int{{0, 1}, {2, 3}}},
		{"identical points", []LatLng{base, base, base}, 1, [][]int{{0, 1, 2}}},
		{"radius zero", []LatLng{base, base}, 0, [][]int{}},
	}
	for _, tt := range tests {
		got := ClusterByRadius(tt.points, tt.radius)
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("%s: got %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestClusterByRadiusAcrossCellBoundariesAndHighLatitude(t *testing.T) {
	// Walk a line of points 12 m apart across many grid cells at a high
	// latitude, where a degree of longitude is short. Every neighbour pair
	// is within 30 m so the whole line must come back as one cluster.
	lat := 34.0
	step := 12.0 / (111195.0 * math.Cos(lat*math.Pi/180))
	pts := make([]LatLng, 0, 60)
	for i := 0; i < 60; i++ {
		pts = append(pts, LatLng{lat, 74.8 + float64(i)*step})
	}
	got := ClusterByRadius(pts, 30)
	if len(got) != 1 || len(got[0]) != len(pts) {
		t.Fatalf("expected one cluster of %d points, got %v", len(pts), got)
	}
}

func TestClusterByRadiusScales(t *testing.T) {
	// 20000 points on a sparse lattice 1 km apart must produce no clusters
	// and finish quickly: the grid avoids the quadratic comparison.
	pts := make([]LatLng, 0, 20000)
	for i := 0; i < 200; i++ {
		for j := 0; j < 100; j++ {
			pts = append(pts, LatLng{20 + float64(i)*0.009, 70 + float64(j)*0.009})
		}
	}
	if got := ClusterByRadius(pts, 30); len(got) != 0 {
		t.Fatalf("expected no clusters, got %d", len(got))
	}
}
