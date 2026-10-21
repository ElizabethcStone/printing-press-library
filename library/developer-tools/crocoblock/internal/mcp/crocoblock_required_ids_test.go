package mcp

import (
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/server"
	"github.com/mvanhorn/printing-press-library/library/developer-tools/crocoblock/internal/cli"
)

// Required site identifiers must be representable by MCP clients. A bare
// uppercase token in Cobra Use silently removes that property from the mirror.
func TestCrocoblockRequiredIDsReachMCPSchema(t *testing.T) {
	s := server.NewMCPServer("required-site-identifiers", "test")
	RegisterTools(s)
	tools := s.ListTools()
	cases := map[string]string{
		"appointments_get": "id", "appointments_update": "id", "appointments_delete": "id",
		"appointments_providers": "service-id", "appointments_slots": "service-id",
		"bookings_config": "resource-id", "bookings_update": "id", "bookings_delete": "id",
		"fields_get": "id", "forms_update": "id",
	}
	for name, identifier := range cases {
		t.Run(name, func(t *testing.T) {
			entry := tools[name]
			if entry == nil {
				t.Fatalf("missing tool %s", name)
			}
			if _, ok := entry.Tool.InputSchema.Properties[identifier]; !ok {
				t.Fatalf("missing identifier %q in %s schema", identifier, name)
			}
			required := false
			for _, property := range entry.Tool.InputSchema.Required {
				if property == identifier {
					required = true
				}
			}
			if !required {
				t.Fatalf("identifier %q is not required in %s", identifier, name)
			}
		})
	}
}

// MCP cannot accept the operator-selected local paths these workflows require.
// Keep the full CLI surface while omitting unusable tools from the mirror.
func TestCrocoblockLocalFileWorkflowsRemainCLIOnly(t *testing.T) {
	s := server.NewMCPServer("local-file-workflows", "test")
	RegisterTools(s)
	tools := s.ListTools()
	root := cli.RootCmd()
	for _, path := range []string{"schema diff", "cct reconcile", "reservations overlaps", "fields restore", "fields update", "import"} {
		t.Run(path, func(t *testing.T) {
			name := strings.ReplaceAll(path, " ", "_")
			if tools[name] != nil {
				t.Fatalf("local-file tool %s is advertised despite unavailable required paths", name)
			}
			command, remaining, err := root.Find(strings.Fields(path))
			if err != nil || len(remaining) != 0 || command == root || !command.Runnable() {
				t.Fatalf("CLI command %q must remain runnable: command=%v remaining=%v err=%v", path, command, remaining, err)
			}
			if command.Annotations["mcp:hidden"] != "true" {
				t.Fatalf("CLI command %q lacks explicit MCP exclusion", path)
			}
			if path == "fields update" && command.Annotations["mcp:write-flags"] != "snapshot" {
				t.Fatal("snapshot must remain declared as a filesystem write destination")
			}
		})
	}
	for _, name := range []string{"forms_field_usage", "filters_impact", "relations_audit", "sync"} {
		if tools[name] == nil {
			t.Fatalf("configured-store tool %s should remain available", name)
		}
	}
}
