package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCrocoConfiguredStoreSharedWithoutIO(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "snapshot.db")
	t.Setenv("CROCOBLOCK_DB", path)
	if got := defaultDBPath("crocoblock-pp-cli"); got != path {
		t.Fatalf("CLI got %s", got)
	}
	if got := crocoDBPath(); got != path {
		t.Fatalf("curated got %s", got)
	}
	if got, err := MCPStorePath(); err != nil || got != path {
		t.Fatalf("MCP got %s: %v", got, err)
	}
	if _, err := os.Stat(filepath.Dir(path)); !os.IsNotExist(err) {
		t.Fatalf("path resolution created files: %v", err)
	}
}
