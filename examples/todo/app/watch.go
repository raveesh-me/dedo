package app

import (
	"context"
	"sync"

	"connectrpc.com/connect"

	todov1 "github.com/raveesh-me/dedo/examples/todo/gen/go/raveesh_me/todo/v1"
)

// subscriberBuffer is how many changes a slow subscriber may fall behind
// before it misses changes. A UI that misses one refetches the list anyway.
const subscriberBuffer = 64

// changes fans each change out to every subscriber.
type changes struct {
	mu   sync.Mutex
	subs map[chan *todov1.WatchTodosResponse]struct{}
}

func newChanges() *changes {
	return &changes{subs: map[chan *todov1.WatchTodosResponse]struct{}{}}
}

// subscribe returns a channel of changes and a function that ends the
// subscription.
func (c *changes) subscribe() (<-chan *todov1.WatchTodosResponse, func()) {
	ch := make(chan *todov1.WatchTodosResponse, subscriberBuffer)
	c.mu.Lock()
	c.subs[ch] = struct{}{}
	c.mu.Unlock()
	return ch, func() {
		c.mu.Lock()
		delete(c.subs, ch)
		c.mu.Unlock()
	}
}

// publish sends the change to every subscriber without blocking; a full
// subscriber skips it.
func (c *changes) publish(change todov1.WatchTodosResponse_Change, todo *todov1.Todo) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for ch := range c.subs {
		select {
		case ch <- &todov1.WatchTodosResponse{Change: change, Todo: clone(todo)}:
		default:
		}
	}
}

// WatchTodos streams every change until the caller disconnects.
func (s *Server) WatchTodos(ctx context.Context, _ *connect.Request[todov1.WatchTodosRequest], stream *connect.ServerStream[todov1.WatchTodosResponse]) error {
	events, cancel := s.changes.subscribe()
	defer cancel()
	// Send the headers now, so the client knows the watch is open before
	// the first change.
	if err := stream.Conn().Send(nil); err != nil {
		return err
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case event := <-events:
			if err := stream.Send(event); err != nil {
				return err
			}
		}
	}
}
