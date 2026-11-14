package mcpserver

import (
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// What an access file requires of a caller is its own paragraph after the
// tool description and before the declared errors; without it the
// description is Describe's.
func TestDescribeWithRequirements(t *testing.T) {
	m := service(t).Methods().ByName("GetThing")
	tool, err := DescribeWithRequirements(m, "Only for: Accounts on the Pro plan (`pro`).")
	if err != nil {
		t.Fatal(err)
	}
	if want := "Returns a thing.\n\nOnly for: Accounts on the Pro plan (`pro`).\n\nErrors: THING_NOT_FOUND (NOT_FOUND)."; tool.Description != want {
		t.Errorf("description = %q, want %q", tool.Description, want)
	}
	plain, err := Describe(m)
	if err != nil {
		t.Fatal(err)
	}
	none, err := DescribeWithRequirements(m, "")
	if err != nil {
		t.Fatal(err)
	}
	if none.Description != plain.Description || plain.Description != "Returns a thing.\n\nErrors: THING_NOT_FOUND (NOT_FOUND)." {
		t.Errorf("without requirements: %q, Describe: %q", none.Description, plain.Description)
	}
}

// Meta decodes the JSON object protoc-gen-dedo-mcp writes into a tool's
// _meta, which the tool then carries on the wire.
func TestMeta(t *testing.T) {
	tool := &mcp.Tool{Name: "get_thing", Meta: Meta(`{"conditions":[{"id":"pro","label":"Accounts on the Pro plan"}]}`)}
	b, err := json.Marshal(tool)
	if err != nil {
		t.Fatal(err)
	}
	var wire struct {
		Meta struct {
			Conditions []struct{ ID, Label string } `json:"conditions"`
		} `json:"_meta"`
	}
	if err := json.Unmarshal(b, &wire); err != nil || len(wire.Meta.Conditions) != 1 || wire.Meta.Conditions[0].ID != "pro" {
		t.Errorf("tool = %s (%v)", b, err)
	}
	for _, bad := range []string{`[1]`, `{`, `null`, ``} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("Meta(%q) did not panic", bad)
				}
			}()
			Meta(bad)
		}()
	}
}
