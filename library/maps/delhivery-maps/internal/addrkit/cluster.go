// Copyright 2026 Som Samantray and contributors. Licensed under Apache-2.0. See LICENSE.

package addrkit

import (
	"math"
	"sort"
)

// LatLng is a coordinate in degrees.
type LatLng struct {
	Lat, Lng float64
}

// clusterMetresPerDegree sizes the grid cells. Haversine here uses a 6371 km
// sphere, about 111195 m per degree; using a smaller figure makes cells a
// little wider than the radius so a pair just inside the radius can never be
// split across non-adjacent cells.
const clusterMetresPerDegree = 110000.0

// MaxClusterRadiusM is the largest radius ClusterByRadius accepts.
const MaxClusterRadiusM = 100000.0

type cell struct{ x, y int }

// ClusterByRadius groups points whose chains of neighbours lie within
// radiusM metres of each other (single-linkage). It returns only clusters
// with two or more members, as sorted index slices, ordered by their first
// member. A grid of cells at least radiusM wide keeps the neighbour search
// near-linear instead of comparing every pair.
func ClusterByRadius(points []LatLng, radiusM float64) [][]int {
	n := len(points)
	if n < 2 || !(radiusM > 0) || radiusM > MaxClusterRadiusM {
		return [][]int{}
	}
	for _, p := range points {
		if math.IsNaN(p.Lat) || math.IsNaN(p.Lng) || math.IsInf(p.Lat, 0) || math.IsInf(p.Lng, 0) {
			return [][]int{}
		}
	}
	// Longitude degrees shrink with latitude. Sizing cells for the highest
	// latitude present makes them at least radiusM wide everywhere, so a
	// 3x3 neighbourhood always covers the radius.
	minCos := 1.0
	for _, p := range points {
		if c := math.Cos(p.Lat * math.Pi / 180); c < minCos {
			minCos = c
		}
	}
	if minCos < 0.01 {
		minCos = 0.01
	}
	cellLat := radiusM / clusterMetresPerDegree
	cellLng := radiusM / (clusterMetresPerDegree * minCos)

	grid := make(map[cell][]int, n)
	cells := make([]cell, n)
	for i, p := range points {
		c := cell{int(math.Floor(p.Lng / cellLng)), int(math.Floor(p.Lat / cellLat))}
		cells[i] = c
		grid[c] = append(grid[c], i)
	}

	parent := make([]int, n)
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(i int) int {
		for parent[i] != i {
			parent[i] = parent[parent[i]]
			i = parent[i]
		}
		return i
	}
	union := func(a, b int) {
		ra, rb := find(a), find(b)
		if ra == rb {
			return
		}
		if ra < rb {
			parent[rb] = ra
		} else {
			parent[ra] = rb
		}
	}

	for i, p := range points {
		c := cells[i]
		for dx := -1; dx <= 1; dx++ {
			for dy := -1; dy <= 1; dy++ {
				for _, j := range grid[cell{c.x + dx, c.y + dy}] {
					if j <= i {
						continue
					}
					if Haversine(p.Lat, p.Lng, points[j].Lat, points[j].Lng) <= radiusM {
						union(i, j)
					}
				}
			}
		}
	}

	byRoot := make(map[int][]int)
	for i := range points {
		r := find(i)
		byRoot[r] = append(byRoot[r], i)
	}
	out := make([][]int, 0)
	for _, members := range byRoot {
		if len(members) >= 2 {
			sort.Ints(members)
			out = append(out, members)
		}
	}
	sort.Slice(out, func(a, b int) bool { return out[a][0] < out[b][0] })
	return out
}
