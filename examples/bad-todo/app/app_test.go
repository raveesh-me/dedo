package app_test

// These tests pin down what the rule-breaking contract does at runtime. Each
// one is a bug that buf lint reports before merge; see expected-lint.txt.

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/raveesh-me/dedo/examples/bad-todo/app"
	todov1 "github.com/raveesh-me/dedo/examples/bad-todo/gen/go/raveesh_me/todo/v1"
	"github.com/raveesh-me/dedo/examples/bad-todo/gen/go/raveesh_me/todo/v1/todov1connect"
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

func (e env) client(token string) todov1connect.TodoServiceClient {
	auth := connect.UnaryInterceptorFunc(func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			req.Header().Set("Authorization", "Bearer "+token)
			return next(ctx, req)
		}
	})
	return todov1connect.NewTodoServiceClient(http.DefaultClient, e.url, connect.WithInterceptors(auth))
}

func (e env) rest(t *testing.T, method, path, token, body string) (int, string) {
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
	return res.StatusCode, string(b)
}

// ZALANDO_SCOPE_NAME_FORMAT: CreateTodo declares "todo.write", which no
// token carries, so even a writer cannot create.
func TestMisnamedScopeLocksOutWriters(t *testing.T) {
	e := newEnv(t)
	_, err := e.client(app.WriterToken).CreateTodo(context.Background(),
		connect.NewRequest(&todov1.CreateTodoRequest{Todo: &todov1.Todo{Title: "x"}}))
	if connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("err = %v, want permission_denied", err)
	}
}

// DEDO_HTTP_PATH_VERSION: GetTodo is served outside /v1/.
func TestUnversionedPath(t *testing.T) {
	e := newEnv(t)
	// /v1/todos/{id} exists for PATCH and DELETE only.
	if code, _ := e.rest(t, http.MethodGet, "/v1/todos/a", app.ReaderToken, ""); code != http.StatusMethodNotAllowed {
		t.Errorf("GET /v1/todos/a: %d, want 405", code)
	}
	code, body := e.rest(t, http.MethodGet, "/todos/a", app.ReaderToken, "")
	if code != http.StatusNotFound || !strings.Contains(body, "TODO_NOT_FOUND") {
		t.Errorf("/todos/a: %d %s, want the handler's TODO_NOT_FOUND", code, body)
	}
}

// DEDO_ERROR_REASON_FORMAT and DEDO_METHOD_RULES_REQUIRED: the
// handlers return reasons the contract does not declare, so the runtime
// logs them.
func TestUndeclaredReasonsAreLogged(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	_, err := e.client(app.ReaderToken).ListTodos(ctx, connect.NewRequest(&todov1.ListTodosRequest{PageToken: "x"}))
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("list: %v", err)
	}
	_, err = e.client(app.WriterToken).DeleteTodo(ctx, connect.NewRequest(&todov1.DeleteTodoRequest{Name: "todos/x"}))
	if connect.CodeOf(err) != connect.CodeNotFound {
		t.Fatalf("delete: %v", err)
	}
	logs := e.log.String()
	for _, want := range []string{"procedure=/raveesh_me.todo.v1.TodoService/ListTodos", "procedure=/raveesh_me.todo.v1.TodoService/DeleteTodo"} {
		if !strings.Contains(logs, "undeclared error reason") || !strings.Contains(logs, want) {
			t.Errorf("no undeclared-reason log for %s in:\n%s", want, logs)
		}
	}
}

// DEDO_NO_JSON_NAME: responses name the field "todoTitle" instead of
// the protojson default "title". Every JSON client sees a different name
// from the one in the schema.
func TestCustomJSONName(t *testing.T) {
	b, err := protojson.Marshal(&todov1.Todo{Title: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if got := string(b); !strings.Contains(got, `"todoTitle"`) {
		t.Errorf("JSON = %s, want todoTitle", got)
	}
}
