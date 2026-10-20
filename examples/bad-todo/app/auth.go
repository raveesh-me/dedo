package app

import (
	"context"
	"strings"

	"connectrpc.com/connect/v2"
)

// Demo bearer tokens and the scopes they grant. A real service reads scopes
// from a verified token instead.
const (
	ReaderToken = "reader"
	WriterToken = "writer"
)

var tokenScopes = map[string][]string{
	ReaderToken: {"todo.todo.read"},
	WriterToken: {"todo.todo.read", "todo.todo.write"},
}

type scopesKey struct{}

// Auth stores the scopes of the caller's bearer token in the call's
// context, for the policy interceptor that runs after it. As a server
// interceptor it covers every transport that reaches the server: Connect,
// gRPC, REST and MCP tool calls. An unknown or missing token grants no
// scopes.
func Auth() connect.ServerInterceptor {
	return func(next connect.ServerFunc) connect.ServerFunc {
		return func(ctx context.Context, spec connect.Spec, stream connect.ServerStream) error {
			var token string
			if info, ok := connect.CallInfoForServerContext(ctx); ok {
				token, _ = strings.CutPrefix(info.RequestHeader().Get("Authorization"), "Bearer ")
			}
			return next(context.WithValue(ctx, scopesKey{}, tokenScopes[token]), spec, stream)
		}
	}
}

// ScopesFromContext returns the scopes stored by Auth.
func ScopesFromContext(ctx context.Context) []string {
	scopes, _ := ctx.Value(scopesKey{}).([]string)
	return scopes
}
