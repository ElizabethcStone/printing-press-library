// Copyright 2026 Elizabeth Stone and contributors. Licensed under Apache-2.0. See LICENSE.

package pgmcp

import (
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// get_account_status field names were confirmed live on 2026-10-29
// (accountEmail, usedPinCredits, pinCreditLimit, remainingPinCredits,
// connectedPinterestProfiles). Field names for list_scheduled_pins,
// list_automations and the other reads are still unverified. These helpers accept several candidate keys so a renamed
// field degrades to "unknown" instead of a silent zero.

// List finds the first array in payload. A bare array is returned as-is;
// otherwise each key in keys is tried, then any array-valued top-level field.
func List(payload json.RawMessage, keys ...string) []map[string]any {
	var root any
	if err := json.Unmarshal(payload, &root); err != nil {
		return nil
	}
	if arr, ok := root.([]any); ok {
		return objects(arr)
	}
	obj, ok := root.(map[string]any)
	if !ok {
		return nil
	}
	for _, k := range keys {
		if arr, ok := obj[k].([]any); ok {
			return objects(arr)
		}
	}
	for _, v := range obj {
		if arr, ok := v.([]any); ok {
			return objects(arr)
		}
	}
	return nil
}

// ListAt returns the array under exactly key, or nil when payload is not an
// object or key is missing. Unlike List it never falls back to another array,
// so responses carrying several lists stay separated.
func ListAt(payload json.RawMessage, key string) []map[string]any {
	obj := Object(payload)
	if obj == nil {
		return nil
	}
	arr, ok := obj[key].([]any)
	if !ok {
		return nil
	}
	return objects(arr)
}

func objects(arr []any) []map[string]any {
	out := make([]map[string]any, 0, len(arr))
	for _, e := range arr {
		if m, ok := e.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

// Object decodes payload as a JSON object, or nil.
func Object(payload json.RawMessage) map[string]any {
	var m map[string]any
	if json.Unmarshal(payload, &m) != nil {
		return nil
	}
	return m
}

// Str returns the first non-empty string (or number rendered as a string)
// under any of keys. Slash-separated keys (credits/remaining) walk nested
// objects; dots are avoided so field paths never read as host names.
func Str(m map[string]any, keys ...string) string {
	for _, k := range keys {
		switch v := dig(m, k).(type) {
		case string:
			if strings.TrimSpace(v) != "" {
				return v
			}
		case float64:
			return strconv.FormatFloat(v, 'f', -1, 64)
		}
	}
	return ""
}

// Num returns the first numeric value under any of keys. Numeric strings
// count. ok is false when no key held a number.
func Num(m map[string]any, keys ...string) (float64, bool) {
	for _, k := range keys {
		switch v := dig(m, k).(type) {
		case float64:
			return v, true
		case int:
			return float64(v), true
		case int64:
			return float64(v), true
		case json.Number:
			if f, err := v.Float64(); err == nil {
				return f, true
			}
		case string:
			if f, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
				return f, true
			}
		}
	}
	return 0, false
}

// Time parses the first timestamp under keys: RFC3339, a date, or epoch
// seconds/milliseconds.
func Time(m map[string]any, keys ...string) (time.Time, bool) {
	for _, k := range keys {
		switch v := dig(m, k).(type) {
		case string:
			for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05", "2006-01-02 15:04:05", "2006-01-02T15:04", "2006-01-02 15:04", "2006-01-02"} {
				if t, err := time.Parse(layout, strings.TrimSpace(v)); err == nil {
					return t.UTC(), true
				}
			}
		case float64:
			if v > 1e12 {
				return time.UnixMilli(int64(v)).UTC(), true
			}
			if v > 1e9 {
				return time.Unix(int64(v), 0).UTC(), true
			}
		}
	}
	return time.Time{}, false
}

func dig(m map[string]any, path string) any {
	var cur any = m
	for _, part := range strings.Split(path, "/") {
		obj, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = obj[part]
	}
	return cur
}

// NormalizeURL makes destination URLs comparable: lowercase host without
// "www.", no fragment, no trailing slash, and the query kept minus tracking
// parameters (sorted), so ?p=1 and ?p=2 stay distinct posts.
func NormalizeURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return strings.TrimRight(strings.ToLower(raw), "/")
	}
	host := strings.TrimPrefix(strings.ToLower(u.Host), "www.")
	out := host + strings.TrimRight(u.EscapedPath(), "/")
	q := u.Query()
	for k := range q {
		lk := strings.ToLower(k)
		if strings.HasPrefix(lk, "utm_") || trackingParams[lk] {
			q.Del(k)
		}
	}
	if enc := q.Encode(); enc != "" { // Encode sorts keys
		out += "?" + enc
	}
	return out
}

var trackingParams = map[string]bool{"fbclid": true, "gclid": true, "msclkid": true, "mc_cid": true, "mc_eid": true, "ref": true, "epik": true, "_branch_match_id": true}

// NormalizeTitle folds case and collapses punctuation and whitespace.
func NormalizeTitle(s string) string {
	var b strings.Builder
	space := false
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r > 127 {
			b.WriteRune(r)
			space = false
			continue
		}
		if !space && b.Len() > 0 {
			b.WriteByte(' ')
			space = true
		}
	}
	return strings.TrimSpace(b.String())
}
