// Copyright 2026 Som Samantray and contributors. Licensed under Apache-2.0. See LICENSE.

// Package addrkit holds the pure-logic helpers behind the address, trip,
// coverage and fleet commands: table readers, key normalization, geometry
// and chunking. Nothing here touches the network or the database, so every
// helper is unit-testable in isolation.
package addrkit

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode"
)

// earthRadiusM is the mean Earth radius used by Haversine.
const earthRadiusM = 6371008.8

// NormalizeKey lowercases s, trims it and collapses every run of whitespace
// to a single space. Cache keys and raw-address keys are built from it so
// "Flat 4B,  Lotus Apt" and "flat 4b, lotus apt" share one entry.
func NormalizeKey(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(s)), " ")
}

// NormalizeText is a looser form of NormalizeKey for duplicate detection:
// punctuation becomes a space (combining marks are kept, so words written in
// Indic scripts are not split apart), so "Sector 45, Gurgaon" and "sector 45 gurgaon"
// compare equal.
func NormalizeText(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsMark(r) {
			b.WriteRune(r)
		} else {
			b.WriteRune(' ')
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// HashKey returns the hex sha256 of NormalizeKey(s).
func HashKey(s string) string {
	sum := sha256.Sum256([]byte(NormalizeKey(s)))
	return hex.EncodeToString(sum[:])
}

// Haversine returns the great-circle distance in metres between two
// latitude/longitude pairs given in degrees.
func Haversine(lat1, lng1, lat2, lng2 float64) float64 {
	rad := math.Pi / 180
	dLat := (lat2 - lat1) * rad
	dLng := (lng2 - lng1) * rad
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*rad)*math.Cos(lat2*rad)*math.Sin(dLng/2)*math.Sin(dLng/2)
	return 2 * earthRadiusM * math.Asin(math.Min(1, math.Sqrt(a)))
}

// ParseLatLng parses "lat,lng" (spaces allowed) and range-checks it.
// ok is false when s is not a coordinate pair, which callers treat as
// "this is an address, not coordinates".
func ParseLatLng(s string) (lat, lng float64, ok bool) {
	parts := strings.Split(strings.TrimSpace(s), ",")
	if len(parts) != 2 {
		return 0, 0, false
	}
	return ParseLatLngPair(parts[0], parts[1])
}

// ParseLatLngPair parses separate latitude and longitude strings.
func ParseLatLngPair(latS, lngS string) (lat, lng float64, ok bool) {
	lat, err1 := ParseFiniteFloat(latS)
	lng, err2 := ParseFiniteFloat(lngS)
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}
	if lat < -90 || lat > 90 || lng < -180 || lng > 180 {
		return 0, 0, false
	}
	return lat, lng, true
}

// ParseFiniteFloat parses a plain decimal number (optional sign, digits,
// fraction, exponent). It rejects NaN, Inf and hexadecimal floats, which
// strconv.ParseFloat would otherwise accept, so a spreadsheet cell or flag
// cannot smuggle a non-finite value into a request or a distance test.
func ParseFiniteFloat(s string) (float64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty number")
	}
	if strings.Trim(s, "0123456789+-.eE") != "" {
		return 0, fmt.Errorf("%q is not a plain decimal number", s)
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("%q is not a number", s)
	}
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, fmt.Errorf("%q is not a finite number", s)
	}
	return f, nil
}

// Chunk splits items into consecutive slices of at most size elements.
// size < 1 is treated as 1. The returned slices alias items.
func Chunk[T any](items []T, size int) [][]T {
	if size < 1 {
		size = 1
	}
	out := make([][]T, 0, (len(items)+size-1)/size)
	for start := 0; start < len(items); start += size {
		end := start + size
		if end > len(items) {
			end = len(items)
		}
		out = append(out, items[start:end])
	}
	return out
}

// FormatFloat renders f without trailing zeros, for CSV cells.
func FormatFloat(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// ValidatePoints confirms every point is a usable [lat, lng] pair.
func ValidatePoints(points [][2]float64) error {
	for i, p := range points {
		if math.IsNaN(p[0]) || math.IsNaN(p[1]) || p[0] < -90 || p[0] > 90 || p[1] < -180 || p[1] > 180 {
			return fmt.Errorf("point %d (%v) is outside valid lat/lng range", i, p)
		}
	}
	return nil
}
