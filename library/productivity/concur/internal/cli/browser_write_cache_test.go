// Copyright 2026 Allen Lew and contributors. Licensed under Apache-2.0. See LICENSE.
// PATCH(amend-2026-10-18: browser writes bypassed cache eviction)

package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/printing-press-library/library/productivity/concur/internal/cliutil"
)

// seedStaleCache plants a cached response where the client keeps them and
// returns a func reporting whether it survived. A write made through the
// browser never reaches the client's own mutation path, so the browser flows
// must evict the cache themselves; otherwise a read soon after the edit shows
// the state from before it.
func seedStaleCache(t *testing.T) (stillCached func() bool) {
	t.Helper()
	t.Setenv("CONCUR_CACHE_DIR", t.TempDir())
	cacheDir, err := cliutil.CacheDir()
	if err != nil {
		t.Fatalf("resolving the cache dir: %v", err)
	}
	dir := filepath.Join(cacheDir, "http", "resources", "reports")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("creating the cache resource dir: %v", err)
	}
	stale := filepath.Join(dir, "stale.json")
	if err := os.WriteFile(stale, []byte(`{"stale":true}`), 0o600); err != nil {
		t.Fatalf("seeding a cached response: %v", err)
	}
	return func() bool {
		_, err := os.Stat(stale)
		return err == nil
	}
}
