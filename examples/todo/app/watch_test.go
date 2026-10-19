package app_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/raveesh-me/http-proto-lint/examples/todo/app"
	todov1 "github.com/raveesh-me/http-proto-lint/examples/todo/gen/go/raveesh_me/todo/v1"
	"github.com/raveesh-me/http-proto-lint/examples/todo/gen/go/raveesh_me/todo/v1/todov1connect"
)

// streamClient sends the token on every request, streams included; the
// interceptor in env.client covers unary calls only.
func streamClient(e env, token string) todov1connect.TodoServiceClient {
	return todov1connect.NewTodoServiceClient(&http.Client{Transport: bearer{token: token}}, e.url)
}

// watch opens WatchTodos and returns its events on a channel.
func watch(t *testing.T, e env, token string) <-chan *todov1.WatchTodosResponse {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	stream, err := streamClient(e, token).WatchTodos(ctx, connect.NewRequest(&todov1.WatchTodosRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	// The server sends headers when the watch is open, so changes made
	// after this point are not missed.
	if _, err := stream.ResponseHeader(), stream.Err(); err != nil {
		t.Fatal(err)
	}
	events := make(chan *todov1.WatchTodosResponse, 16)
	go func() {
		defer close(events)
		for stream.Receive() {
			events <- stream.Msg()
		}
		if err := stream.Err(); err != nil {
			t.Logf("watch ended: %v", err)
		}
	}()
	return events
}

func next(t *testing.T, events <-chan *todov1.WatchTodosResponse) *todov1.WatchTodosResponse {
	t.Helper()
	select {
	case event, ok := <-events:
		if !ok {
			t.Fatal("watch ended")
		}
		return event
	case <-time.After(5 * time.Second):
		t.Fatal("no change within 5s")
	}
	return nil
}

// A to-do an agent adds through MCP reaches every open watch at once.
func TestWatchSeesChangesMadeThroughMCP(t *testing.T) {
	e := newEnv(t)
	events := watch(t, e, app.ReaderToken)

	ctx := context.Background()
	res, err := mcpSession(t, e, app.WriterToken).CallTool(ctx, &mcp.CallToolParams{
		Name:      createTool,
		Arguments: map[string]any{"todo": map[string]any{"title": "added by an agent"}, "todo_id": "agent"},
	})
	if err != nil || res.IsError {
		t.Fatalf("create via MCP: %v %s", err, text(res))
	}
	created := next(t, events)
	if created.GetChange() != todov1.WatchTodosResponse_CHANGE_CREATED || created.GetTodo().GetTitle() != "added by an agent" {
		t.Fatalf("event = %v", created)
	}

	if _, err := e.client(app.WriterToken).DeleteTodo(ctx, connect.NewRequest(&todov1.DeleteTodoRequest{Name: "todos/agent"})); err != nil {
		t.Fatal(err)
	}
	deleted := next(t, events)
	if deleted.GetChange() != todov1.WatchTodosResponse_CHANGE_DELETED || deleted.GetTodo().GetName() != "todos/agent" {
		t.Fatalf("event = %v", deleted)
	}
}

// The stream needs the read scope like any other read.
func TestWatchRequiresReadScope(t *testing.T) {
	e := newEnv(t)
	stream, err := streamClient(e, "nobody").WatchTodos(context.Background(), connect.NewRequest(&todov1.WatchTodosRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	if stream.Receive() || connect.CodeOf(stream.Err()) != connect.CodePermissionDenied {
		t.Fatalf("err = %v, want permission_denied", stream.Err())
	}
}
