package app_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/raveesh-me/dedo/examples/todo/app"
	todov1 "github.com/raveesh-me/dedo/examples/todo/gen/go/raveesh_me/todo/v1"
	"github.com/raveesh-me/dedo/examples/todo/gen/go/raveesh_me/todo/v1/todov1connect"
)

// streamClient sends the token on every request, streams included.
func streamClient(e env, token string) todov1connect.TodoServiceClient {
	return todov1connect.NewTodoServiceClient(connect.NewClient(connecthttp.NewTransport(http.DefaultClient, e.url), bearerAuth(token)))
}

// watch opens WatchTodos, waits until the server says the watch is open, and
// returns the changes after that on a channel.
func watch(t *testing.T, e env, token string) <-chan *todov1.WatchTodosResponse {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	stream, err := streamClient(e, token).WatchTodos(ctx, &todov1.WatchTodosRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if first, err := stream.Receive(); err != nil || first.GetChange() != todov1.WatchTodosResponse_CHANGE_WATCHING {
		t.Fatalf("first message = %v, %v; want CHANGE_WATCHING", first, err)
	}
	events := make(chan *todov1.WatchTodosResponse, 16)
	go func() {
		defer close(events)
		for {
			msg, err := stream.Receive()
			if err != nil {
				if !errors.Is(err, io.EOF) && ctx.Err() == nil {
					t.Logf("watch ended: %v", err)
				}
				return
			}
			events <- msg
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
	const id = "agent"
	res, err := mcpSession(t, e, app.WriterToken).CallTool(ctx, &mcp.CallToolParams{
		Name:      createTool,
		Arguments: map[string]any{"todo": map[string]any{"title": "added by an agent"}, "todo_id": id},
	})
	if err != nil || res.IsError {
		t.Fatalf("create via MCP: %v %s", err, text(res))
	}
	created := next(t, events)
	if created.GetChange() != todov1.WatchTodosResponse_CHANGE_CREATED || created.GetTodo().GetTitle() != "added by an agent" {
		t.Fatalf("event = %v", created)
	}

	if _, err := e.client(app.WriterToken).DeleteTodo(ctx, &todov1.DeleteTodoRequest{Name: "todos/" + id}); err != nil {
		t.Fatal(err)
	}
	deleted := next(t, events)
	if deleted.GetChange() != todov1.WatchTodosResponse_CHANGE_DELETED || deleted.GetTodo().GetName() != "todos/"+id {
		t.Fatalf("event = %v", deleted)
	}
}

// The stream needs the read scope like any other read.
func TestWatchRequiresReadScope(t *testing.T) {
	e := newEnv(t)
	stream, err := streamClient(e, "nobody").WatchTodos(context.Background(), &todov1.WatchTodosRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stream.Receive(); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("err = %v, want permission_denied", err)
	}
}
