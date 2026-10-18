package mcpserver

import (
	"context"
	"net/http"
	"net/http/httptest"

	"connectrpc.com/connect"
)

// ForwardHeaders is a Connect client interceptor that sends the MCP caller's
// Authorization and X-Flow-ID headers on the forwarded call.
func ForwardHeaders() connect.UnaryInterceptorFunc {
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			if h, ok := ctx.Value(headersKey{}).(http.Header); ok && req.Spec().IsClient {
				for name, values := range h {
					req.Header()[name] = values
				}
			}
			return next(ctx, req)
		}
	}
}

// InProcessClient returns an HTTP client that serves every request with
// handler, without a network hop. Give it the service's whole HTTP stack,
// so forwarded calls pass the same middleware as external ones. It suits
// unary calls, which is all MCP tools make.
func InProcessClient(handler http.Handler) *http.Client {
	return &http.Client{Transport: inProcess{handler: handler}}
}

type inProcess struct{ handler http.Handler }

func (t inProcess) RoundTrip(req *http.Request) (*http.Response, error) {
	rec := httptest.NewRecorder()
	t.handler.ServeHTTP(rec, req)
	res := rec.Result()
	res.Request = req
	return res, nil
}
