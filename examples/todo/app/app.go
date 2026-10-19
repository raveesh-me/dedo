// Package app wires the TodoService to the dedo runtime: the
// policy table, the policy interceptor, flow IDs, REST transcoding, the REST
// reference and the MCP server.
package app

import (
	"errors"
	"log/slog"
	"net/http"

	"connectrpc.com/connect"
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
//	/mcp                  the MCP server; tool calls run through the API below
//	everything else       the API over Connect, gRPC and REST
func NewHandler(cfg Config) (http.Handler, error) {
	if cfg.Log == nil {
		return nil, errors.New("app: no logger")
	}
	api, err := newAPI(cfg)
	if err != nil {
		return nil, err
	}
	tools, err := newMCP(api)
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	mux.Handle("GET /openapi.json", docs.Spec())
	mux.Handle("GET /docs", docs.Page())
	mux.Handle("/mcp", flowid.Middleware(tools))
	mux.Handle("/", api)
	return mux, nil
}

// newAPI returns the service behind its policy: flow IDs, auth, REST
// transcoding and the policy interceptor.
func newAPI(cfg Config) (http.Handler, error) {
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
