package client

import (
	"strings"
	"testing"
)

func TestTruncateBody(t *testing.T) {
	short := strings.Repeat("a", 200)
	if got := truncateBody([]byte(short)); got != short {
		t.Fatalf("short body changed")
	}
	atLimit := strings.Repeat("a", maxErrorBodyBytes)
	if got := truncateBody([]byte(atLimit)); got != atLimit {
		t.Fatalf("body at the limit must be returned whole")
	}
	long := strings.Repeat("a", maxErrorBodyBytes+1)
	got := truncateBody([]byte(long))
	if want := strings.Repeat("a", maxErrorBodyBytes) + "..."; got != want {
		t.Fatalf("long body: want cut + marker, got len %d", len(got))
	}
	// a multibyte rune straddling the limit must not leave invalid UTF-8
	split := strings.Repeat("a", maxErrorBodyBytes-1) + "é" // é is 2 bytes
	if out := truncateBody([]byte(split)); !strings.HasSuffix(out, "...") || strings.ContainsRune(out, '\uFFFD') {
		t.Fatalf("rune split at the limit: %q", out[len(out)-8:])
	}
}
