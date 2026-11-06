package bfftools

import (
	"context"
	"net/textproto"
	"slices"

	"connectrpc.com/connect/v2"

	"github.com/raveesh-me/dedo/flowid"
)

// Authorization is the request header that carries the caller's
// credentials.
const Authorization = "Authorization"

// Forward returns a client interceptor that copies the caller's credentials,
// the request headers Authorization and flowid.Header, and the named
// headers of the call the BFF is serving onto each call it makes with that
// call's context, replacing any value the outbound call set. The flow ID is
// the one flowid.Interceptor kept or replaced, when the server runs it.
//
// A BFF holds no identity or credentials of its own. It reaches the services
// it reads as its caller, so each of them checks the caller's token and
// scopes itself, internal APIs included. Forward never invents a header: a
// call made outside a served call, or for a caller who sent none, goes
// without it.
func Forward(headers ...string) connect.ClientInterceptor {
	names := []string{Authorization, flowid.Header}
	for _, h := range headers {
		if !slices.ContainsFunc(names, func(n string) bool {
			return textproto.CanonicalMIMEHeaderKey(n) == textproto.CanonicalMIMEHeaderKey(h)
		}) {
			names = append(names, h)
		}
	}
	return func(next connect.ClientFunc) connect.ClientFunc {
		return func(ctx context.Context, spec connect.Spec) (connect.ClientStream, error) {
			in, ok := connect.CallInfoForServerContext(ctx)
			if !ok {
				return next(ctx, spec)
			}
			out, ok := connect.CallInfoForClientContext(ctx)
			if !ok {
				ctx, out = connect.NewClientContext(ctx)
			}
			for _, name := range names {
				if values := forwarded(ctx, in, name); len(values) > 0 {
					out.RequestHeader().SetValues(name, values)
				}
			}
			return next(ctx, spec)
		}
	}
}

// forwarded returns a copy of the values of a request header of the served
// call. The flow ID is the one the BFF uses, which flowid.Interceptor may
// have put in place of a malformed one.
func forwarded(ctx context.Context, in *connect.CallInfo, name string) []string {
	if textproto.CanonicalMIMEHeaderKey(name) == textproto.CanonicalMIMEHeaderKey(flowid.Header) {
		if id := flowid.From(ctx); id != "" {
			return []string{id}
		}
	}
	return slices.Clone(in.RequestHeader().Values(name))
}
