package app_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pb33f/libopenapi"
	"google.golang.org/genproto/googleapis/type/money"

	"github.com/raveesh-me/dedo/examples/todo/app"
	todov1 "github.com/raveesh-me/dedo/examples/todo/gen/go/raveesh_me/todo/v1"
	"github.com/raveesh-me/dedo/flowid"
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
	// The deprecated remove_todo is not a tool.
	if len(tools) != 5 || tools["remove_todo"] != nil {
		t.Fatalf("got %d tools, want 5 without remove_todo", len(tools))
	}
	get := tools[getTool]
	if get.Title != "Get a to-do" || !strings.HasPrefix(get.Description, "Returns one to-do by its resource name") {
		t.Errorf("GetTodo tool: title %q, description %q", get.Title, get.Description)
	}
	if !strings.Contains(get.Description, "Errors: TODO_NOT_FOUND (NOT_FOUND).") {
		t.Errorf("GetTodo description lacks its error reasons: %q", get.Description)
	}
	update, _ := json.Marshal(tools["update_todo"].InputSchema)
	for _, want := range []string{`"required":["name"]`, "Valid names: title, done, budget_amount, assignee_email."} {
		if !strings.Contains(string(update), want) {
			t.Errorf("update_todo input schema lacks %s: %s", want, update)
		}
	}
	if strings.Contains(string(update), "create_time") {
		t.Errorf("update_todo input schema offers the output-only create_time: %s", update)
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

// An agent marks a to-do done with its name alone, and an update without a
// mask leaves the fields it does not set alone (AIP-134).
func TestMCPPartialUpdate(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	writer := mcpSession(t, e, app.WriterToken)
	call := func(name string, args map[string]any) string {
		t.Helper()
		res, err := writer.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil || res.IsError {
			t.Fatalf("%s: %v %s", name, err, text(res))
		}
		return text(res)
	}
	call(createTool, map[string]any{
		"todo_id": "p",
		"todo": map[string]any{
			"title":          "keep my fields",
			"assignee_email": "a@example.com",
			"budget_amount":  map[string]any{"currency_code": "EUR", "units": "5"},
		},
	})
	done := call("update_todo", map[string]any{"todo": map[string]any{"name": "todos/p", "done": true}})
	for _, want := range []string{`"done":true`, "keep my fields", "a@example.com", `"units":"5"`} {
		if !strings.Contains(done, want) {
			t.Errorf("after marking done, the to-do lacks %s: %s", want, done)
		}
	}
	// The mask names fields the way the input schema does; lowerCamelCase
	// works too.
	masked := call("update_todo", map[string]any{"todo": map[string]any{"name": "todos/p"}, "update_mask": "assignee_email"})
	if strings.Contains(masked, "a@example.com") || !strings.Contains(masked, "keep my fields") {
		t.Errorf("a masked update should clear only assignee_email: %s", masked)
	}
	camel := call("update_todo", map[string]any{"todo": map[string]any{"name": "todos/p"}, "update_mask": "budgetAmount"})
	if strings.Contains(camel, `"units"`) {
		t.Errorf("a lowerCamelCase mask should clear budget_amount: %s", camel)
	}
}

// An empty list says so: the fields are present with their defaults.
func TestMCPEmptyListIsExplicit(t *testing.T) {
	e := newEnv(t)
	res, err := mcpSession(t, e, app.ReaderToken).CallTool(context.Background(), &mcp.CallToolParams{
		Name: "list_todos", Arguments: map[string]any{},
	})
	if err != nil || res.IsError {
		t.Fatalf("list_todos: %v %s", err, text(res))
	}
	got, _ := json.Marshal(res.StructuredContent)
	if string(got) != `{"next_page_token":"","todos":[]}` {
		t.Errorf("empty list = %s", got)
	}
}

// The buf.validate rules the schemas state hold on every transport: the
// MCP schema rejects bad input before the call, and Connect rejects it too.
func TestValidationRules(t *testing.T) {
	e := newEnv(t)
	res, err := mcpSession(t, e, app.WriterToken).CallTool(context.Background(), &mcp.CallToolParams{
		Name:      createTool,
		Arguments: map[string]any{"todo": map[string]any{"title": "x", "assignee_email": "not-an-email"}},
	})
	if err == nil && !res.IsError {
		t.Errorf("MCP accepted an invalid email: %s", text(res))
	}
	_, err = e.client(app.WriterToken).CreateTodo(context.Background(), &todov1.CreateTodoRequest{
		Todo: &todov1.Todo{Title: "x", AssigneeEmail: "not-an-email"},
	})
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("Connect CreateTodo with an invalid email: %v, want InvalidArgument", err)
	}
	_, err = e.client(app.WriterToken).CreateTodo(context.Background(), &todov1.CreateTodoRequest{
		Todo: &todov1.Todo{Title: "x", BudgetAmount: &money.Money{CurrencyCode: "EUR", Units: 1, Nanos: -500000000}},
	})
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("CreateTodo with units and nanos of opposite signs: %v, want InvalidArgument", err)
	}
	_, err = e.client(app.ReaderToken).ListTodos(context.Background(), &todov1.ListTodosRequest{PageSize: -1})
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("ListTodos with page_size -1: %v, want InvalidArgument", err)
	}
}

// A browser page on another origin cannot call the MCP endpoint (the MCP
// transport's Origin check, against DNS rebinding); MCP clients, which send
// no Origin, and same-origin pages can.
func TestMCPRejectsCrossOrigin(t *testing.T) {
	e := newEnv(t)
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`
	tests := []struct {
		origin string
		want   int
	}{
		{"", http.StatusOK},
		{e.url, http.StatusOK},
		{"http://evil.example", http.StatusForbidden},
	}
	for _, tt := range tests {
		req, err := http.NewRequest(http.MethodPost, e.url+"/mcp", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		req.Header.Set("Authorization", "Bearer "+app.ReaderToken)
		if tt.origin != "" {
			req.Header.Set("Origin", tt.origin)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != tt.want {
			t.Errorf("Origin %q: status %d, want %d", tt.origin, res.StatusCode, tt.want)
		}
	}
}

// Every surface reaches the same connect.Server, so the same interceptors
// run on each: a reader cannot create a to-do, and every response carries a
// flow ID.
func TestEverySurfaceRunsTheInterceptors(t *testing.T) {
	e := newEnv(t)
	denied := func(surface string, err error) {
		t.Helper()
		if connect.CodeOf(err) != connect.CodePermissionDenied {
			t.Errorf("%s: err = %v, want permission_denied", surface, err)
		}
	}
	req := &todov1.CreateTodoRequest{TodoId: "x", Todo: &todov1.Todo{Title: "x"}}
	for name, opts := range map[string][]connecthttp.Option{
		"connect":  nil,
		"grpc-web": {connecthttp.WithGRPCWeb()},
	} {
		ctx, info := connect.NewClientContext(context.Background())
		_, err := e.client(app.ReaderToken, opts...).CreateTodo(ctx, req)
		denied(name, err)
		if info.ResponseHeader().Get(flowid.Header) == "" {
			t.Errorf("%s: no flow ID", name)
		}
	}
	res, body := e.rest(t, http.MethodPost, "/commerce/todo/v1/todos?todoId=x", app.ReaderToken, `{"title":"x"}`)
	if res.StatusCode != http.StatusForbidden || res.Header.Get(flowid.Header) == "" {
		t.Errorf("rest: %d %s, flow ID %q", res.StatusCode, body, res.Header.Get(flowid.Header))
	}
	tool, err := mcpSession(t, e, app.ReaderToken).CallTool(context.Background(), &mcp.CallToolParams{
		Name: createTool, Arguments: map[string]any{"todo_id": "x", "todo": map[string]any{"title": "x"}},
	})
	if err != nil || !tool.IsError || !strings.Contains(text(tool), "PERMISSION_DENIED") {
		t.Errorf("mcp: %v %s", err, text(tool))
	}
}
