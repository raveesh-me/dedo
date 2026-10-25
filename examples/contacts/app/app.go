// Package app wires the ContactService and LocationService to the dedo
// runtime: the policy table, the policy interceptor with its tenant check,
// flow IDs, REST transcoding, the REST reference and the MCP server.
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

	"github.com/raveesh-me/dedo/examples/contacts/docs"
	"github.com/raveesh-me/dedo/examples/contacts/gen/go/raveesh_me/contacts/v1/contactsv1connect"
	"github.com/raveesh-me/dedo/flowid"
	"github.com/raveesh-me/dedo/interceptor"
	"github.com/raveesh-me/dedo/mcp/mcpserver"
	"github.com/raveesh-me/dedo/policy"
)

// Config configures the handler.
type Config struct {
	Log           *slog.Logger
	EnforceScopes bool
	// MCPAccess says who may reach /mcp: anyone (public, the default), or
	// only callers with a known token (authenticated).
	MCPAccess mcpserver.Access
}

// NewHandler returns the HTTP handler for both services. It fails when a
// service's declared contract is incomplete, such as an RPC whose tenant
// the runtime cannot find.
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
	endpoint, err := mcpserver.Guard(tools, cfg.MCPAccess, Authenticated)
	if err != nil {
		return nil, err
	}
	mux.Handle("/mcp", flowid.Middleware(endpoint))
	return mux, nil
}

// newServer returns both services behind their policy. The interceptors run
// for every transport, in order: flow IDs, auth, the declared contract
// (scopes, tenants, headers, error audit), then the buf.validate rules, so
// an unauthorised caller learns nothing from validation errors.
func newServer(cfg Config) (*connect.Server, error) {
	table, err := policy.Build(contactsv1connect.ContactServiceName, contactsv1connect.LocationServiceName)
	if err != nil {
		return nil, err
	}
	guard, err := interceptor.New(interceptor.Config{
		Table:         table,
		Scopes:        ScopesFromContext,
		EnforceScopes: cfg.EnforceScopes,
		Tenants:       TenantAccess,
		Log:           cfg.Log,
	})
	if err != nil {
		return nil, err
	}
	server := connect.NewServer(flowid.Interceptor(), Auth(), guard, validate.NewServerInterceptor())
	api := NewServer()
	contactsv1connect.RegisterContactServiceHandler(server, api)
	contactsv1connect.RegisterLocationServiceHandler(server, api)
	return server, nil
}
