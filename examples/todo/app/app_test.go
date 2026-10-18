package app_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"google.golang.org/genproto/googleapis/type/money"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"github.com/raveesh-me/http-proto-lint/examples/todo/app"
	todov1 "github.com/raveesh-me/http-proto-lint/examples/todo/gen/go/raveesh_me/todo/v1"
	"github.com/raveesh-me/http-proto-lint/examples/todo/gen/go/raveesh_me/todo/v1/todov1connect"
	"github.com/raveesh-me/http-proto-lint/flowid"
)

type env struct {
	url string
	log *bytes.Buffer
}

func newEnv(t *testing.T) env {
	t.Helper()
	var log bytes.Buffer
	handler, err := app.NewHandler(app.Config{
		Log:           slog.New(slog.NewTextHandler(&log, nil)),
		EnforceScopes: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return env{url: srv.URL, log: &log}
}

// client returns a Connect client that sends the token on every call.
func (e env) client(token string, opts ...connect.ClientOption) todov1connect.TodoServiceClient {
	auth := connect.UnaryInterceptorFunc(func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			req.Header().Set("Authorization", "Bearer "+token)
			return next(ctx, req)
		}
	})
	opts = append(opts, connect.WithInterceptors(auth))
	return todov1connect.NewTodoServiceClient(http.DefaultClient, e.url, opts...)
}

// rest sends a REST request and returns the response and its body.
func (e env) rest(t *testing.T, method, path, token, body string) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequest(method, e.url+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return res, string(b)
}

func create(t *testing.T, e env, id, title string) *todov1.Todo {
	t.Helper()
	res, err := e.client(app.WriterToken).CreateTodo(context.Background(), connect.NewRequest(&todov1.CreateTodoRequest{
		TodoId: id,
		Todo:   &todov1.Todo{Title: title, BudgetAmount: &money.Money{CurrencyCode: "EUR", Units: 5}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	return res.Msg.GetTodo()
}

func TestScopes(t *testing.T) {
	e := newEnv(t)
	create(t, e, "a", "write it")
	ctx := context.Background()

	_, err := e.client(app.ReaderToken).CreateTodo(ctx, connect.NewRequest(&todov1.CreateTodoRequest{Todo: &todov1.Todo{Title: "x"}}))
	if connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Errorf("reader create: %v, want permission_denied", err)
	}
	_, err = e.client("nobody").GetTodo(ctx, connect.NewRequest(&todov1.GetTodoRequest{Name: "todos/a"}))
	if connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Errorf("anonymous get: %v, want permission_denied", err)
	}
	if _, err := e.client(app.ReaderToken).GetTodo(ctx, connect.NewRequest(&todov1.GetTodoRequest{Name: "todos/a"})); err != nil {
		t.Errorf("reader get: %v", err)
	}
}

func TestCRUD(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	w := e.client(app.WriterToken)
	created := create(t, e, "", "first")
	if created.GetName() != "todos/1" || created.GetCreateTime() == nil {
		t.Fatalf("created = %v", created)
	}
	if _, err := w.CreateTodo(ctx, connect.NewRequest(&todov1.CreateTodoRequest{TodoId: "1", Todo: &todov1.Todo{Title: "dup"}})); connect.CodeOf(err) != connect.CodeAlreadyExists {
		t.Errorf("duplicate create: %v", err)
	}
	if _, err := w.CreateTodo(ctx, connect.NewRequest(&todov1.CreateTodoRequest{Todo: &todov1.Todo{}})); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("untitled create: %v", err)
	}
	updated, err := w.UpdateTodo(ctx, connect.NewRequest(&todov1.UpdateTodoRequest{
		Todo:       &todov1.Todo{Name: "todos/1", Done: true, Title: "ignored"},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"done"}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !updated.Msg.GetTodo().GetDone() || updated.Msg.GetTodo().GetTitle() != "first" {
		t.Errorf("updated = %v", updated.Msg.GetTodo())
	}
	if _, err := w.DeleteTodo(ctx, connect.NewRequest(&todov1.DeleteTodoRequest{Name: "todos/1"})); err != nil {
		t.Fatal(err)
	}
	if _, err := w.GetTodo(ctx, connect.NewRequest(&todov1.GetTodoRequest{Name: "todos/1"})); connect.CodeOf(err) != connect.CodeNotFound {
		t.Errorf("get after delete: %v", err)
	}
}

func TestListPagination(t *testing.T) {
	e := newEnv(t)
	for _, title := range []string{"a", "b", "c"} {
		create(t, e, "", title)
	}
	r := e.client(app.ReaderToken)
	ctx := context.Background()
	var titles []string
	token := ""
	for {
		res, err := r.ListTodos(ctx, connect.NewRequest(&todov1.ListTodosRequest{PageSize: 2, PageToken: token}))
		if err != nil {
			t.Fatal(err)
		}
		for _, todo := range res.Msg.GetTodos() {
			titles = append(titles, todo.GetTitle())
		}
		if token = res.Msg.GetNextPageToken(); token == "" {
			break
		}
	}
	if strings.Join(titles, ",") != "a,b,c" {
		t.Errorf("titles = %v", titles)
	}
	_, err := r.ListTodos(ctx, connect.NewRequest(&todov1.ListTodosRequest{PageToken: "x"}))
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("bad token: %v", err)
	}
}

func TestCacheHeaderOnConnectGet(t *testing.T) {
	e := newEnv(t)
	create(t, e, "a", "cache me")
	res, err := e.client(app.ReaderToken, connect.WithHTTPGet()).GetTodo(context.Background(),
		connect.NewRequest(&todov1.GetTodoRequest{Name: "todos/a"}))
	if err != nil {
		t.Fatal(err)
	}
	if got := res.Header().Get("Cache-Control"); got != "private, max-age=10" {
		t.Errorf("Cache-Control = %q", got)
	}
}

func TestDeprecationHeaders(t *testing.T) {
	e := newEnv(t)
	create(t, e, "a", "old")
	res, err := e.client(app.WriterToken).RemoveTodo(context.Background(), connect.NewRequest(&todov1.RemoveTodoRequest{Name: "todos/a"}))
	if err != nil {
		t.Fatal(err)
	}
	if got := res.Header().Get("Deprecation"); got != "@1767225600" {
		t.Errorf("Deprecation = %q", got)
	}
	if got := res.Header().Get("Sunset"); got != "Fri, 01 Jan 2027 00:00:00 GMT" {
		t.Errorf("Sunset = %q", got)
	}
}

func TestRESTUnwrapsResponseBody(t *testing.T) {
	e := newEnv(t)
	res, body := e.rest(t, http.MethodPost, "/v1/todos?todoId=r", app.WriterToken,
		`{"title":"via rest","budgetAmount":{"currencyCode":"EUR","units":"3"}}`)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("create: %d %s", res.StatusCode, body)
	}
	res, body = e.rest(t, http.MethodGet, "/v1/todos/r", app.ReaderToken, "")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("get: %d %s", res.StatusCode, body)
	}
	var todo map[string]any
	if err := json.Unmarshal([]byte(body), &todo); err != nil {
		t.Fatal(err)
	}
	// response_body unwraps GetTodoResponse, so REST callers get the bare
	// resource with lowerCamelCase field names.
	if todo["name"] != "todos/r" || todo["title"] != "via rest" || todo["budgetAmount"] == nil {
		t.Errorf("body = %s", body)
	}
	if got := res.Header.Get("Cache-Control"); got != "private, max-age=10" {
		t.Errorf("REST GET Cache-Control = %q", got)
	}
}

func TestRESTErrors(t *testing.T) {
	e := newEnv(t)
	res, body := e.rest(t, http.MethodGet, "/v1/todos/missing", app.ReaderToken, "")
	if res.StatusCode != http.StatusNotFound || !strings.Contains(body, "TODO_NOT_FOUND") {
		t.Errorf("missing: %d %s", res.StatusCode, body)
	}
	res, _ = e.rest(t, http.MethodDelete, "/v1/todos/missing", app.ReaderToken, "")
	if res.StatusCode != http.StatusForbidden {
		t.Errorf("reader delete: %d, want 403", res.StatusCode)
	}
}

func TestFlowIDAndErrorAudit(t *testing.T) {
	e := newEnv(t)
	res, _ := e.rest(t, http.MethodGet, "/v1/todos/missing", app.ReaderToken, "")
	if res.Header.Get(flowid.Header) == "" {
		t.Error("no flow ID on response")
	}
	// Declared reasons are not logged.
	if strings.Contains(e.log.String(), "undeclared error reason") {
		t.Errorf("declared reason logged as undeclared: %s", e.log.String())
	}
	// Scope denials in enforce mode are not audit-logged either.
	e.rest(t, http.MethodDelete, "/v1/todos/missing", app.ReaderToken, "")
	if strings.Contains(e.log.String(), "audit mode") {
		t.Errorf("unexpected audit log: %s", e.log.String())
	}
}
