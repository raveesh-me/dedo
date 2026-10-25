// Package app wires the TodoService to the dedo runtime: the
// policy table, the policy interceptor, flow IDs, REST transcoding, the REST
// reference and the MCP server.
package app

import (
	"errors"
	"log/slog"
	"net/http"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	"connectrpc.com/connect/v2/connectinprocess"
	"connectrpc.com/validate"
	"connectrpc.com/vanguard"

	"github.com/raveesh-me/dedo/examples/todo/docs"
	"github.com/raveesh-me/dedo/examples/todo/gen/go/raveesh_me/todo/v1/todov1connect"
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
//
// Routes:
//
//	/docs, /openapi.json  the REST reference
//	/mcp                  the MCP server; tool calls go to the server below
//	everything else       the server over Connect, gRPC and REST
func NewHandler(cfg Config) (http.Handler, error) {
	if cfg.Log == nil {
		return nil, errors.New("app: no logger")
	}
	server, err := newServer(cfg)
	if err != nil {
		return nil, err
	}
	// MCP tool calls go straight to the server, in-process.
	tools, err := NewMCP(connect.NewClient(connectinprocess.New(server)))
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	// Connect, gRPC and gRPC-Web, then REST from the google.api.http bindings.
	connecthttp.Mount(mux, server)
	if err := vanguard.Mount(mux, server); err != nil {
		return nil, err
	}
	mux.Handle("GET /openapi.json", docs.Spec())
	mux.Handle("GET /docs", docs.Page())
	mux.Handle("/mcp", flowid.Middleware(tools))
	return mux, nil
}

// newServer returns the TodoService behind its policy. The interceptors run
// for every transport, in order: flow IDs, auth, the declared contract
// (scopes, headers, error audit), then the buf.validate rules, so an
// unauthorised caller learns nothing from validation errors.
func newServer(cfg Config) (*connect.Server, error) {
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
	server := connect.NewServer(flowid.Interceptor(), Auth(), guard, validate.NewServerInterceptor())
	todov1connect.RegisterTodoServiceHandler(server, NewServer())
	return server, nil
}
