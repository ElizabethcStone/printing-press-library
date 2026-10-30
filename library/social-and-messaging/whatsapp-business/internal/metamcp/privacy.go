package metamcp

import (
	"fmt"
	"sort"
	"strings"
)

// SensitiveKey covers the official schemas' secrets, including webhook tokens.
func SensitiveKey(key string) bool {
	k := strings.ToLower(key)
	return strings.Contains(k, "token") || strings.Contains(k, "secret") || strings.Contains(k, "verification_code") || k == "pin" || strings.Contains(k, "password")
}

// PreviewArguments recursively redacts sensitive fields without changing input.
func PreviewArguments(input map[string]any) map[string]any {
	out := make(map[string]any, len(input))
	for k, v := range input {
		if SensitiveKey(k) {
			out[k] = "[REDACTED]"
		} else {
			out[k] = previewValue(v)
		}
	}
	return out
}
func previewValue(v any) any {
	switch x := v.(type) {
	case map[string]any:
		return PreviewArguments(x)
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = previewValue(item)
		}
		return out
	default:
		return v
	}
}
func sensitiveValues(input map[string]any) []string {
	set := map[string]bool{}
	var visit func(any, bool)
	visit = func(v any, secret bool) {
		switch x := v.(type) {
		case map[string]any:
			for k, item := range x {
				visit(item, secret || SensitiveKey(k))
			}
		case []any:
			for _, item := range x {
				visit(item, secret)
			}
		default:
			if secret && v != nil {
				s := fmt.Sprint(v)
				if s != "" {
					set[s] = true
				}
			}
		}
	}
	visit(input, false)
	out := make([]string, 0, len(set))
	for s := range set {
		out = append(out, s)
	}
	// Replace longer overlapping values before their substrings.
	sort.Slice(out, func(i, j int) bool { return len(out[i]) > len(out[j]) })
	return out
}
