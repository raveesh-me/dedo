package app

import (
	"context"
	"strconv"

	"connectrpc.com/connect"

	todov1 "github.com/raveesh-me/dedo/examples/bad-todo/gen/go/raveesh_me/todo/v1"
	"github.com/raveesh-me/dedo/examples/bad-todo/gen/go/raveesh_me/todo/v1/todov1connect"
)

const maxPageSize = 100

// Server implements the TodoService over an in-memory store. It holds no
// policy code: scopes, headers and error audit come from the interceptor.
type Server struct {
	store *store
}

var _ todov1connect.TodoServiceHandler = (*Server)(nil)

// NewServer returns a server with an empty list.
func NewServer() *Server { return &Server{store: newStore()} }

// GetTodo returns one to-do.
func (s *Server) GetTodo(_ context.Context, req *connect.Request[todov1.GetTodoRequest]) (*connect.Response[todov1.GetTodoResponse], error) {
	todo, ok := s.store.get(req.Msg.GetName())
	if !ok {
		return nil, notFound(req.Msg.GetName())
	}
	return connect.NewResponse(&todov1.GetTodoResponse{Todo: todo}), nil
}

// ListTodos returns a page of to-dos. The page token is the offset of the
// next to-do.
func (s *Server) ListTodos(_ context.Context, req *connect.Request[todov1.ListTodosRequest]) (*connect.Response[todov1.ListTodosResponse], error) {
	offset := 0
	if token := req.Msg.GetPageToken(); token != "" {
		n, err := strconv.Atoi(token)
		if err != nil || n < 0 {
			return nil, apiError(connect.CodeInvalidArgument, reasonInvalidPageToken, "malformed page token")
		}
		offset = n
	}
	size := int(req.Msg.GetPageSize())
	if size <= 0 || size > maxPageSize {
		size = maxPageSize
	}
	todos, more := s.store.list(offset, size)
	res := &todov1.ListTodosResponse{Todos: todos}
	if more {
		res.NextPageToken = strconv.Itoa(offset + len(todos))
	}
	return connect.NewResponse(res), nil
}

// CreateTodo adds a to-do.
func (s *Server) CreateTodo(_ context.Context, req *connect.Request[todov1.CreateTodoRequest]) (*connect.Response[todov1.CreateTodoResponse], error) {
	if req.Msg.GetTodo().GetTitle() == "" {
		return nil, apiError(connect.CodeInvalidArgument, reasonTitleRequired, "title is required")
	}
	todo, ok := s.store.create(req.Msg.GetTodoId(), req.Msg.GetTodo())
	if !ok {
		return nil, apiError(connect.CodeAlreadyExists, reasonAlreadyExists, "to-do ID is taken")
	}
	return connect.NewResponse(&todov1.CreateTodoResponse{Todo: todo}), nil
}

// UpdateTodo changes the fields named in the update mask, or every mutable
// field when the mask is empty.
func (s *Server) UpdateTodo(_ context.Context, req *connect.Request[todov1.UpdateTodoRequest]) (*connect.Response[todov1.UpdateTodoResponse], error) {
	patch := req.Msg.GetTodo()
	paths := req.Msg.GetUpdateMask().GetPaths()
	if (len(paths) == 0 || contains(paths, "title")) && patch.GetTitle() == "" {
		return nil, apiError(connect.CodeInvalidArgument, reasonTitleRequired, "title is required")
	}
	todo, ok := s.store.update(patch, paths)
	if !ok {
		return nil, notFound(patch.GetName())
	}
	return connect.NewResponse(&todov1.UpdateTodoResponse{Todo: todo}), nil
}

// DeleteTodo removes a to-do.
func (s *Server) DeleteTodo(_ context.Context, req *connect.Request[todov1.DeleteTodoRequest]) (*connect.Response[todov1.DeleteTodoResponse], error) {
	if !s.store.delete(req.Msg.GetName()) {
		return nil, notFound(req.Msg.GetName())
	}
	return connect.NewResponse(&todov1.DeleteTodoResponse{}), nil
}

// RemoveTodo is the deprecated form of DeleteTodo.
func (s *Server) RemoveTodo(_ context.Context, req *connect.Request[todov1.RemoveTodoRequest]) (*connect.Response[todov1.RemoveTodoResponse], error) {
	if !s.store.delete(req.Msg.GetName()) {
		return nil, notFound(req.Msg.GetName())
	}
	return connect.NewResponse(&todov1.RemoveTodoResponse{}), nil
}

func contains(paths []string, path string) bool {
	for _, p := range paths {
		if p == path {
			return true
		}
	}
	return false
}
