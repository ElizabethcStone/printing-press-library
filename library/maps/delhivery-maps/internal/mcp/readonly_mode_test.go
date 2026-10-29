package mcp

import (
	"testing"

	"github.com/mark3labs/mcp-go/server"
	"github.com/mvanhorn/printing-press-library/library/maps/delhivery-maps/internal/cliutil"
)

// The read-only switch keeps every tool that declares readOnlyHint. The POST
// compute endpoints only read or compute, so they must survive the switch.
func TestReadOnlyModeKeepsTheComputeEndpoints(t *testing.T) {
	t.Setenv(cliutil.MCPReadOnlyEnvVar, "1")
	s := server.NewMCPServer("read-only-mode", "test")
	RegisterTools(s)
	tools := s.ListTools()
	for _, name := range []string{"geocode_address", "route_compute", "matrix_compute", "validate_address"} {
		if _, ok := tools[name]; !ok {
			t.Errorf("read-only mode dropped %s", name)
		}
	}
}
