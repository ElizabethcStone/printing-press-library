// Package metamcp implements the official WhatsApp Business Tools MCP contract.
package metamcp

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

//go:embed catalog.json
var catalogJSON []byte

//go:embed commands.json
var commandJSON []byte

type Tool struct {
	raw          map[string]json.RawMessage
	Name         string         `json:"name"`
	Description  string         `json:"description"`
	InputSchema  map[string]any `json:"inputSchema"`
	OutputSchema map[string]any `json:"outputSchema,omitempty"`
	Annotations  struct {
		ReadOnly bool `json:"readOnlyHint"`
	} `json:"annotations"`
}

var once sync.Once
var tools []Tool
var commands map[string]string

func Catalog() []Tool {
	once.Do(func() {
		var doc struct {
			Tools []Tool `json:"tools"`
		}
		if err := json.Unmarshal(catalogJSON, &doc); err != nil {
			panic(err)
		}
		tools = doc.Tools
		if err := json.Unmarshal(commandJSON, &commands); err != nil {
			panic(err)
		}
	})
	return tools
}
func Command(name string) string { Catalog(); return commands[name] }
func Lookup(name string) (Tool, bool) {
	for _, t := range Catalog() {
		if t.Name == name {
			return t, true
		}
	}
	return Tool{}, false
}
func Validate(tool Tool, args map[string]any) error {
	props, _ := tool.InputSchema["properties"].(map[string]any)
	for k := range args {
		if _, ok := props[k]; !ok {
			return fmt.Errorf("unknown argument %q for %s", k, tool.Name)
		}
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource("urn:whatsapp:input", tool.InputSchema); err != nil {
		return fmt.Errorf("input schema unavailable for %s", tool.Name)
	}
	s, err := c.Compile("urn:whatsapp:input")
	if err != nil {
		return fmt.Errorf("input schema invalid for %s", tool.Name)
	}
	// Validate normalized JSON values; integer-like flag values and nested inputs
	// follow the same JSON Schema contract as direct MCP clients.
	raw, err := json.Marshal(args)
	if err != nil {
		return fmt.Errorf("arguments must be JSON")
	}
	value, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return err
	}
	if err = s.Validate(value); err != nil {
		return fmt.Errorf("arguments do not match %s input schema; inspect command help and required flags", tool.Name)
	}
	return nil
}

// Retain beta-server fields unknown to this client so catalog evidence does
// not silently discard future metadata or additional safety annotations.
func (t *Tool) UnmarshalJSON(data []byte) error {
	type plain Tool
	var value plain
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*t = Tool(value)
	t.raw = raw
	return nil
}
func (t Tool) MarshalJSON() ([]byte, error) {
	out := map[string]any{}
	for k, v := range t.raw {
		out[k] = v
	}
	out["name"] = t.Name
	out["description"] = t.Description
	out["inputSchema"] = t.InputSchema
	if t.OutputSchema != nil {
		out["outputSchema"] = t.OutputSchema
	}
	annotations := map[string]any{}
	if raw, ok := t.raw["annotations"]; ok {
		_ = json.Unmarshal(raw, &annotations)
	}
	if annotations == nil {
		annotations = map[string]any{}
	}
	annotations["readOnlyHint"] = t.Annotations.ReadOnly
	out["annotations"] = annotations
	return json.Marshal(out)
}
