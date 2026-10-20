package mcpserver

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
)

// specSchemaEnv names a Tool schema to check against instead of the
// vendored one; the weekly MCP spec job points it at the latest spec.
const specSchemaEnv = "MCP_TOOL_SCHEMA"

// TestToolsMatchSpec checks every served tool against the MCP
// specification's Tool definition (testdata/mcp-tool-schema.json, made by
// scripts/mcp-tool-schema.py). The spec allows unknown keys, so the test also
// fails on a key the spec no longer defines: a renamed or dropped field.
func TestToolsMatchSpec(t *testing.T) {
	path := os.Getenv(specSchemaEnv)
	if path == "" {
		path = "testdata/mcp-tool-schema.json"
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var spec jsonschema.Schema
	if err := json.Unmarshal(raw, &spec); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	resolved, err := spec.Resolve(nil)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	res, err := session(t).ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range res.Tools {
		b, err := json.Marshal(tool)
		if err != nil {
			t.Fatal(err)
		}
		var wire map[string]any
		if err := json.Unmarshal(b, &wire); err != nil {
			t.Fatal(err)
		}
		if err := resolved.Validate(wire); err != nil {
			t.Errorf("%s does not match the spec's Tool: %v", tool.Name, err)
		}
		unknownKeys(t, tool.Name, wire, spec.Defs["Tool"])
		if annotations, ok := wire["annotations"].(map[string]any); ok {
			unknownKeys(t, tool.Name+".annotations", annotations, spec.Defs["ToolAnnotations"])
		}
	}
}

func unknownKeys(t *testing.T, where string, got map[string]any, def *jsonschema.Schema) {
	t.Helper()
	if def == nil {
		t.Fatalf("the spec schema has no definition for %s", where)
	}
	for key := range got {
		if _, ok := def.Properties[key]; !ok {
			t.Errorf("%s sends %q, which the spec does not define", where, key)
		}
	}
}
