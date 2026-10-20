package app_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
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

// watch opens WatchTodos and returns its events on a channel.
func watch(t *testing.T, e env, token string) <-chan *todov1.WatchTodosResponse {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	events := make(chan *todov1.WatchTodosResponse, 16)
	// The call itself runs in the background: connect-go v2.0.1's HTTP
	// server does not send headers on SendHeaders, so the call returns only
	// with the first change.
	go func() {
		defer close(events)
		stream, err := streamClient(e, token).WatchTodos(ctx, &todov1.WatchTodosRequest{})
		if err != nil {
			if ctx.Err() == nil {
				t.Errorf("open watch: %v", err)
			}
			return
		}
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

// eventFor repeats act, with a new attempt number each time, until the
// watch reports a change it caused. The stream gives no signal that it is
// open, so a change made before the server subscribed is missed; the next
// attempt is not.
func eventFor(t *testing.T, events <-chan *todov1.WatchTodosResponse, act func(attempt int) (name string)) *todov1.WatchTodosResponse {
	t.Helper()
	for attempt := range 20 {
		name := act(attempt)
		deadline := time.After(250 * time.Millisecond)
		for {
			select {
			case event, ok := <-events:
				if !ok {
					t.Fatal("watch ended")
				}
				if event.GetTodo().GetName() == name {
					return event
				}
				continue
			case <-deadline:
			}
			break
		}
	}
	t.Fatal("the watch saw none of 20 changes")
	return nil
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
	session := mcpSession(t, e, app.WriterToken)
	var id string
	created := eventFor(t, events, func(attempt int) string {
		id = "agent" + strconv.Itoa(attempt)
		res, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name:      createTool,
			Arguments: map[string]any{"todo": map[string]any{"title": "added by an agent"}, "todo_id": id},
		})
		if err != nil || res.IsError {
			t.Fatalf("create via MCP: %v %s", err, text(res))
		}
		return "todos/" + id
	})
	if created.GetChange() != todov1.WatchTodosResponse_CHANGE_CREATED || created.GetTodo().GetTitle() != "added by an agent" {
		t.Fatalf("event = %v", created)
	}

	// The watch is open now, so the next change arrives on its own.
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
