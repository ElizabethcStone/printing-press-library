package cli

import "os"

// The operator-selected snapshot is shared by curated commands and framework/MCP reads.
func crocoConfiguredDBPath() string { return os.Getenv("CROCOBLOCK_DB") }
