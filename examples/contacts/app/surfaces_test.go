package app_test

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/raveesh-me/dedo/examples/contacts/app"
	contactsv1 "github.com/raveesh-me/dedo/examples/contacts/gen/go/raveesh_me/contacts/v1"
)

// bearer adds a token to every request of the MCP client.
type bearer struct{ token string }

func (b bearer) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", "Bearer "+b.token)
	return http.DefaultTransport.RoundTrip(req)
}

func mcpSession(t *testing.T, e env, token string) *mcp.ClientSession {
	t.Helper()
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "v1"}, nil)
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{
		Endpoint:   e.url + "/mcp",
		HTTPClient: &http.Client{Transport: bearer{token: token}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func text(res *mcp.CallToolResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		if t, ok := c.(*mcp.TextContent); ok {
			b.WriteString(t.Text)
		}
	}
	return b.String()
}

// callTool calls a tool and fails the test on a transport error; a tool
// error is in the result.
func callTool(t *testing.T, s *mcp.ClientSession, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	res, err := s.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return res
}

// denied reports whether a tool result is the interceptor's tenant or
// scope denial.
func denied(res *mcp.CallToolResult) bool {
	return res.IsError && strings.Contains(text(res), "PERMISSION_DENIED")
}

// Every RPC of both services is a tool.
func TestMCPTools(t *testing.T) {
	e := newEnv(t)
	res, err := mcpSession(t, e, app.Loc1Reader).ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range res.Tools {
		names = append(names, tool.Name)
	}
	slices.Sort(names)
	want := []string{"create_contact", "get_contact", "get_location", "list_contacts", "list_locations", "update_contact"}
	if !slices.Equal(names, want) {
		t.Errorf("tools = %v, want %v", names, want)
	}
}

// A writer creates, reads, updates and lists contacts in its own location
// through MCP tools.
func TestMCPCRUD(t *testing.T) {
	e := newEnv(t)
	w := mcpSession(t, e, app.Loc1Writer)
	ok := func(name string, args map[string]any) string {
		t.Helper()
		res := callTool(t, w, name, args)
		if res.IsError {
			t.Fatalf("%s: %s", name, text(res))
		}
		return text(res)
	}
	created := ok("create_contact", map[string]any{
		"parent": "locations/1", "contact_id": "a",
		"contact": map[string]any{"display_name": "Ada", "email": "ada@example.com"},
	})
	if !strings.Contains(created, "locations/1/contacts/a") {
		t.Errorf("create_contact: %s", created)
	}
	if got := ok("get_contact", map[string]any{"name": "locations/1/contacts/a"}); !strings.Contains(got, "ada@example.com") {
		t.Errorf("get_contact: %s", got)
	}
	updated := ok("update_contact", map[string]any{"contact": map[string]any{"name": "locations/1/contacts/a", "display_name": "Ada L."}})
	if !strings.Contains(updated, "Ada L.") || !strings.Contains(updated, "ada@example.com") {
		t.Errorf("update_contact without a mask should keep email: %s", updated)
	}
	if list := ok("list_contacts", map[string]any{"parent": "locations/1"}); !strings.Contains(list, "Ada L.") {
		t.Errorf("list_contacts: %s", list)
	}
	if got := ok("get_location", map[string]any{"name": "locations/1"}); !strings.Contains(got, "Downtown") {
		t.Errorf("get_location: %s", got)
	}
}

// A writer of locations/1 cannot reach locations/2 through MCP tools: every
// call gets the interceptor's PERMISSION_DENIED, whether or not the
// resource exists.
func TestMCPCrossTenantDenied(t *testing.T) {
	e := newEnv(t)
	create(t, e, app.Loc2Writer, "locations/2", "x", &contactsv1.Contact{DisplayName: "theirs"})
	w := mcpSession(t, e, app.Loc1Writer)
	calls := []struct {
		tool string
		args map[string]any
	}{
		{"get_contact", map[string]any{"name": "locations/2/contacts/x"}},
		{"get_contact", map[string]any{"name": "locations/2/contacts/nope"}},
		{"list_contacts", map[string]any{"parent": "locations/2"}},
		{"create_contact", map[string]any{"parent": "locations/2", "contact": map[string]any{"display_name": "x"}}},
		{"update_contact", map[string]any{"contact": map[string]any{"name": "locations/2/contacts/x", "display_name": "mine now"}}},
		{"get_location", map[string]any{"name": "locations/2"}},
	}
	for _, c := range calls {
		if res := callTool(t, w, c.tool, c.args); !denied(res) {
			t.Errorf("%s %v: isError=%v %s", c.tool, c.args, res.IsError, text(res))
		}
	}
	got := callTool(t, mcpSession(t, e, app.Loc2Writer), "get_contact", map[string]any{"name": "locations/2/contacts/x"})
	if got.IsError || !strings.Contains(text(got), "theirs") {
		t.Errorf("the denied update changed the contact: %s", text(got))
	}
}

// update_contact checks contact.home_location, a reference nested in the
// request, as well as the contact's own name.
func TestMCPNestedReference(t *testing.T) {
	e := newEnv(t)
	create(t, e, app.Loc1Writer, "locations/1", "a", &contactsv1.Contact{DisplayName: "Ada"})
	w := mcpSession(t, e, app.Loc1Writer)
	update := func(home string) *mcp.CallToolResult {
		return callTool(t, w, "update_contact", map[string]any{
			"contact":     map[string]any{"name": "locations/1/contacts/a", "home_location": home},
			"update_mask": "home_location",
		})
	}
	if res := update("locations/2"); !denied(res) {
		t.Errorf("home_location locations/2: isError=%v %s", res.IsError, text(res))
	}
	res := callTool(t, w, "create_contact", map[string]any{
		"parent": "locations/1", "contact": map[string]any{"display_name": "x", "home_location": "locations/2"},
	})
	if !denied(res) {
		t.Errorf("create with home_location locations/2: isError=%v %s", res.IsError, text(res))
	}
	if res := update("locations/1"); res.IsError || !strings.Contains(text(res), `"home_location":"locations/1"`) {
		t.Errorf("home_location locations/1: isError=%v %s", res.IsError, text(res))
	}
}

// AllReader reads every location through MCP tools, and writes none.
func TestMCPAllReader(t *testing.T) {
	e := newEnv(t)
	create(t, e, app.Loc1Writer, "locations/1", "a", &contactsv1.Contact{DisplayName: "one"})
	create(t, e, app.Loc2Writer, "locations/2", "b", &contactsv1.Contact{DisplayName: "two"})
	r := mcpSession(t, e, app.AllReader)
	for _, name := range []string{"locations/1/contacts/a", "locations/2/contacts/b"} {
		if res := callTool(t, r, "get_contact", map[string]any{"name": name}); res.IsError {
			t.Errorf("get_contact %s: %s", name, text(res))
		}
	}
	res := callTool(t, r, "create_contact", map[string]any{"parent": "locations/1", "contact": map[string]any{"display_name": "x"}})
	if !denied(res) {
		t.Errorf("create_contact: isError=%v %s", res.IsError, text(res))
	}
	res = callTool(t, r, "update_contact", map[string]any{"contact": map[string]any{"name": "locations/1/contacts/a", "display_name": "x"}})
	if !denied(res) {
		t.Errorf("update_contact: isError=%v %s", res.IsError, text(res))
	}
}

// list_locations returns only the caller's own locations.
func TestMCPListLocations(t *testing.T) {
	e := newEnv(t)
	tests := map[string][]string{
		app.Loc1Writer: {"locations/1"},
		app.AllReader:  {"locations/1", "locations/2", "locations/3"},
	}
	for token, want := range tests {
		res := callTool(t, mcpSession(t, e, token), "list_locations", map[string]any{})
		if res.IsError {
			t.Fatalf("%s: %s", token, text(res))
		}
		var out struct {
			Locations []struct {
				Name string `json:"name"`
			} `json:"locations"`
		}
		raw, _ := json.Marshal(res.StructuredContent)
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, l := range out.Locations {
			got = append(got, l.Name)
		}
		if !slices.Equal(got, want) {
			t.Errorf("%s: locations %v, want %v", token, got, want)
		}
	}
}
