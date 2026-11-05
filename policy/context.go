package policy

import "context"

type methodKey struct{}

// WithMethod returns a context carrying the contract of the RPC being
// served. The policy interceptor sets it for every call, so code that
// runs inside a handler, such as an event publisher, can read what the
// RPC declares.
func WithMethod(ctx context.Context, m Method) context.Context {
	return context.WithValue(ctx, methodKey{}, m)
}

// MethodFrom returns the contract of the RPC being served, and false
// outside a call.
func MethodFrom(ctx context.Context) (Method, bool) {
	m, ok := ctx.Value(methodKey{}).(Method)
	return m, ok
}
