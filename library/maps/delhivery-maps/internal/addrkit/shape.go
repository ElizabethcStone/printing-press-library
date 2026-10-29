// Copyright 2026 Som Samantray and contributors. Licensed under Apache-2.0. See LICENSE.

package addrkit

import (
	"errors"
	"math"
)

// Downsample returns at most max points from pts, evenly spaced and always
// keeping the first and last point. Order is preserved. max < 2 is treated
// as 2, and inputs already within the limit are returned unchanged.
func Downsample(pts [][2]float64, max int) [][2]float64 {
	if max < 2 {
		max = 2
	}
	n := len(pts)
	if n <= max {
		return pts
	}
	out := make([][2]float64, 0, max)
	last := n - 1
	for i := 0; i < max; i++ {
		idx := int(math.Round(float64(i) * float64(last) / float64(max-1)))
		out = append(out, pts[idx])
	}
	return out
}

// Ring is a closed polygon ring whose points are [x, y] = [lng, lat], the
// GeoJSON order. Callers holding (lat, lng) must swap when probing it.
type Ring [][2]float64

// PointInRing reports whether (x, y) lies inside ring using ray casting.
// The ring may be open or closed (first point repeated). Points exactly on an
// edge are not guaranteed either way.
func PointInRing(x, y float64, ring Ring) bool {
	n := len(ring)
	if n < 3 {
		return false
	}
	inside := false
	j := n - 1
	for i := 0; i < n; i++ {
		xi, yi := ring[i][0], ring[i][1]
		xj, yj := ring[j][0], ring[j][1]
		if (yi > y) != (yj > y) && x < (xj-xi)*(y-yi)/(yj-yi)+xi {
			inside = !inside
		}
		j = i
	}
	return inside
}

// Polygon is a GeoJSON polygon: rings[0] is the outer ring, the rest are holes.
type Polygon []Ring

// Contains reports whether (x, y) = (lng, lat) is inside the outer ring and
// outside every hole.
func (p Polygon) Contains(x, y float64) bool {
	if len(p) == 0 || !PointInRing(x, y, p[0]) {
		return false
	}
	for _, hole := range p[1:] {
		if PointInRing(x, y, hole) {
			return false
		}
	}
	return true
}

// MultiPolygon is a set of polygons; a point is inside when any contains it.
type MultiPolygon []Polygon

// Contains reports whether (x, y) = (lng, lat) is inside any polygon.
func (m MultiPolygon) Contains(x, y float64) bool {
	for _, p := range m {
		if p.Contains(x, y) {
			return true
		}
	}
	return false
}

// DecodePolyline decodes a Google-style encoded polyline into [lat, lng]
// pairs. precision is the number of decimal digits (5 for classic Google,
// 6 for the Delhivery route geometry).
func DecodePolyline(encoded string, precision int) ([][2]float64, error) {
	if precision < 0 || precision > 9 {
		return nil, errors.New("polyline precision must be between 0 and 9")
	}
	factor := math.Pow10(precision)
	var out [][2]float64
	var lat, lng int64
	i := 0
	next := func() (int64, error) {
		var result, shift int64
		for {
			if i >= len(encoded) {
				return 0, errors.New("truncated polyline")
			}
			b := int64(encoded[i]) - 63
			i++
			if b < 0 || b > 63 {
				return 0, errors.New("invalid polyline character")
			}
			result |= (b & 0x1f) << shift
			shift += 5
			if b < 0x20 {
				break
			}
			if shift > 60 {
				return 0, errors.New("polyline value too large")
			}
		}
		if result&1 != 0 {
			return ^(result >> 1), nil
		}
		return result >> 1, nil
	}
	for i < len(encoded) {
		dLat, err := next()
		if err != nil {
			return nil, err
		}
		dLng, err := next()
		if err != nil {
			return nil, err
		}
		lat += dLat
		lng += dLng
		out = append(out, [2]float64{float64(lat) / factor, float64(lng) / factor})
	}
	return out, nil
}
