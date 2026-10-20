package mcpserver

import (
	"net/http"
	"net/http/httptest"
)

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
