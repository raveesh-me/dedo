// Package app wires the TodoService to the http-proto-lint runtime: the
// policy table, the policy interceptor, flow IDs and REST transcoding.
package app

import (
	"errors"
	"log/slog"
	"net/http"

	"connectrpc.com/connect"
	"connectrpc.com/vanguard"

	"github.com/raveesh-me/http-proto-lint/examples/todo/gen/go/raveesh_me/todo/v1/todov1connect"
	"github.com/raveesh-me/http-proto-lint/flowid"
	"github.com/raveesh-me/http-proto-lint/interceptor"
	"github.com/raveesh-me/http-proto-lint/policy"
)

// Config configures the handler.
type Config struct {
	Log           *slog.Logger
	EnforceScopes bool
}

// NewHandler returns the HTTP handler for the service. It fails when the
// service's declared contract is incomplete.
func NewHandler(cfg Config) (http.Handler, error) {
	if cfg.Log == nil {
		return nil, errors.New("app: no logger")
	}
	table, err := policy.Build(todov1connect.TodoServiceName)
	if err != nil {
		return nil, err
	}
	guard, err := interceptor.New(interceptor.Config{
		Table:         table,
		Scopes:        ScopesFromContext,
		EnforceScopes: cfg.EnforceScopes,
		Log:           cfg.Log,
	})
	if err != nil {
		return nil, err
	}
	path, handler := todov1connect.NewTodoServiceHandler(
		NewServer(),
		connect.WithInterceptors(guard),
	)
	transcoder, err := vanguard.NewTranscoder([]*vanguard.Service{vanguard.NewService(path, handler)})
	if err != nil {
		return nil, err
	}
	return flowid.Middleware(AuthMiddleware(transcoder)), nil
}
