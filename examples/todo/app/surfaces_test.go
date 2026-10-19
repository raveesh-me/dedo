package app_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pb33f/libopenapi"

	"github.com/raveesh-me/http-proto-lint/examples/todo/app"
)

func get(t *testing.T, url string) string {
	t.Helper()
	res, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: %d", url, res.StatusCode)
	}
	return string(b)
}

// The REST reference documents the google.api.http bindings only, and is a
// valid OpenAPI 3.1 document.
func TestRESTReference(t *testing.T) {
	e := newEnv(t)
	spec := get(t, e.url+"/openapi.json")
	doc, err := libopenapi.NewDocument([]byte(spec))
	if err != nil {
		t.Fatal(err)
	}
	model, err := doc.BuildV3Model()
	if err != nil {
		t.Fatalf("invalid OpenAPI: %v", err)
	}
	var paths []string
	for path := range model.Model.Paths.PathItems.KeysFromOldest() {
		paths = append(paths, path)
		if !strings.HasPrefix(path, "/commerce/todo/v1/") {
			t.Errorf("path %q is outside the service root", path)
		}
	}
	if len(paths) != 3 {
		t.Errorf("paths = %v, want todos, todos/{todo} and todos/{todo}:remove", paths)
	}
	item, ok := model.Model.Paths.PathItems.Get("/commerce/todo/v1/todos/{todo}")
	if !ok || item.Get == nil {
		t.Fatalf("no GET /commerce/todo/v1/todos/{todo} in %v", paths)
	}
	if item.Get.Summary != "Get a to-do" || item.Get.Security[0].Requirements.GetOrZero("bearer")[0] != "todo.todo.read" {
		t.Errorf("GetTodo summary %q, security %v", item.Get.Summary, item.Get.Security)
	}
	if _, ok := item.Get.Responses.Codes.Get("404"); !ok {
		t.Error("GetTodo documents no 404 for TODO_NOT_FOUND")
	}
	if page := get(t, e.url+"/docs"); !strings.Contains(page, `apiDescriptionUrl="/openapi.json"`) {
		t.Errorf("docs page does not load the spec: %s", page)
	}
}

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

const (
	getTool    = "get_todo"
	createTool = "create_todo"
	deleteTool = "delete_todo"
)

// MCP tools carry the declared title, description and behaviour hints.
func TestMCPToolsUseDeclaredText(t *testing.T) {
	e := newEnv(t)
	res, err := mcpSession(t, e, app.ReaderToken).ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	tools := map[string]*mcp.Tool{}
	for _, tool := range res.Tools {
		tools[tool.Name] = tool
	}
	if len(tools) != 6 {
		t.Fatalf("got %d tools, want 6", len(tools))
	}
	get := tools[getTool]
	if get.Title != "Get a to-do" || !strings.HasPrefix(get.Description, "Returns one to-do by its resource name") {
		t.Errorf("GetTodo tool: title %q, description %q", get.Title, get.Description)
	}
	// Read-only: destructive and idempotent hints do not apply, so they are
	// left out, as the MCP specification says.
	if a := get.Annotations; !a.ReadOnlyHint || a.DestructiveHint != nil || a.OpenWorldHint == nil || *a.OpenWorldHint {
		t.Errorf("GetTodo hints: %+v", a)
	}
	if a := tools[deleteTool].Annotations; a.ReadOnlyHint || !*a.DestructiveHint || !a.IdempotentHint || *a.OpenWorldHint {
		t.Errorf("DeleteTodo hints: %+v", a)
	}
	if a := tools[createTool].Annotations; a.ReadOnlyHint || *a.DestructiveHint || a.IdempotentHint {
		t.Errorf("CreateTodo hints: %+v", a)
	}
	var schema map[string]any
	raw, _ := json.Marshal(get.InputSchema)
	if err := json.Unmarshal(raw, &schema); err != nil || schema["description"] != "Which to-do to get." {
		t.Errorf("GetTodo input schema description: %v", schema["description"])
	}
}

// MCP calls run through the same scope checks as REST and RPC calls.
func TestMCPCallsEnforceScopes(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	args := map[string]any{"todo": map[string]any{"title": "from a model"}, "todo_id": "m"}

	denied, err := mcpSession(t, e, app.ReaderToken).CallTool(ctx, &mcp.CallToolParams{Name: createTool, Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	if !denied.IsError || !strings.Contains(text(denied), "PERMISSION_DENIED") {
		t.Errorf("reader create via MCP: isError=%v %s", denied.IsError, text(denied))
	}

	created, err := mcpSession(t, e, app.WriterToken).CallTool(ctx, &mcp.CallToolParams{Name: createTool, Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	if created.IsError || !strings.Contains(text(created), "todos/m") {
		t.Fatalf("writer create via MCP: isError=%v %s", created.IsError, text(created))
	}
	got, err := mcpSession(t, e, app.ReaderToken).CallTool(ctx, &mcp.CallToolParams{Name: getTool, Arguments: map[string]any{"name": "todos/m"}})
	if err != nil {
		t.Fatal(err)
	}
	if got.IsError || !strings.Contains(text(got), "from a model") {
		t.Errorf("reader get via MCP: isError=%v %s", got.IsError, text(got))
	}
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
