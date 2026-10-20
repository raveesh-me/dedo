// Package flowid reads or creates the X-Flow-ID header and propagates it on
// outbound calls.
package flowid

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"regexp"

	"connectrpc.com/connect/v2"
)

// Header is the HTTP header that carries the flow ID.
const Header = "X-Flow-ID"

var valid = regexp.MustCompile(`^[A-Za-z0-9_\-+/=]{1,128}$`)

type key struct{}

// Interceptor ensures every RPC carries a flow ID, on every transport that
// reaches the server, and returns it in the response headers. A malformed
// inbound ID is replaced, so callers cannot inject text into logs. An ID
// that Middleware already set is kept.
func Interceptor() connect.ServerInterceptor {
	return func(next connect.ServerFunc) connect.ServerFunc {
		return func(ctx context.Context, spec connect.Spec, stream connect.ServerStream) error {
			info, ok := connect.CallInfoForServerContext(ctx)
			id := From(ctx)
			if id == "" && ok {
				id = info.RequestHeader().Get(Header)
			}
			if !valid.MatchString(id) {
				id = generate()
			}
			if ok {
				info.ResponseHeader().Set(Header, id)
			}
			return next(WithID(ctx, id), spec, stream)
		}
	}
}

// Middleware gives plain HTTP routes a flow ID, such as an MCP endpoint whose
// tool calls forward it to the server.
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(Header)
		if !valid.MatchString(id) {
			id = generate()
		}
		r = r.WithContext(WithID(r.Context(), id))
		r.Header.Set(Header, id)
		w.Header().Set(Header, id)
		next.ServeHTTP(w, r)
	})
}

// WithID returns a context carrying the flow ID.
func WithID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, key{}, id)
}

// From returns the flow ID stored by Interceptor or Middleware, or "".
func From(ctx context.Context) string {
	id, _ := ctx.Value(key{}).(string)
	return id
}

// ClientInterceptor copies the flow ID onto outbound calls.
func ClientInterceptor() connect.ClientInterceptor {
	return func(next connect.ClientFunc) connect.ClientFunc {
		return func(ctx context.Context, spec connect.Spec) (connect.ClientStream, error) {
			if id := From(ctx); id != "" {
				info, ok := connect.CallInfoForClientContext(ctx)
				if !ok {
					ctx, info = connect.NewClientContext(ctx)
				}
				info.RequestHeader().Set(Header, id)
			}
			return next(ctx, spec)
		}
	}
}

func generate() string {
	b := make([]byte, 16)
	// crypto/rand.Read never returns an error on supported platforms.
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}
