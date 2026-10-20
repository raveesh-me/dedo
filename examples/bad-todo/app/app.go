// Package app wires the TodoService to the dedo runtime: the
// policy table, the policy interceptor, flow IDs and REST transcoding.
package app

import (
	"errors"
	"log/slog"
	"net/http"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	"connectrpc.com/vanguard"

	"github.com/raveesh-me/dedo/examples/bad-todo/gen/go/raveesh_me/todo/v1/todov1connect"
	"github.com/raveesh-me/dedo/flowid"
	"github.com/raveesh-me/dedo/interceptor"
	"github.com/raveesh-me/dedo/policy"
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
	server := connect.NewServer(flowid.Interceptor(), Auth(), guard)
	todov1connect.RegisterTodoServiceHandler(server, NewServer())
	mux := http.NewServeMux()
	connecthttp.Mount(mux, server)
	if err := vanguard.Mount(mux, server); err != nil {
		return nil, err
	}
	return mux, nil
}
