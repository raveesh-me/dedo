package app

import (
	"context"
	"net/http"
	"strings"
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

// AuthMiddleware stores the scopes of the caller's bearer token in the
// request context. An unknown or missing token grants no scopes.
func AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		ctx := context.WithValue(r.Context(), scopesKey{}, tokenScopes[token])
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// ScopesFromContext returns the scopes stored by AuthMiddleware.
func ScopesFromContext(ctx context.Context) []string {
	scopes, _ := ctx.Value(scopesKey{}).([]string)
	return scopes
}
