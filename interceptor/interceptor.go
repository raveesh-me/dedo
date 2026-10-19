// Package interceptor applies the declared operational contract of each RPC:
// it checks scopes, sets cache and deprecation headers, and logs undeclared
// error reasons.
package interceptor

import (
	"context"
	"errors"
	"log/slog"

	"connectrpc.com/connect"

	"github.com/raveesh-me/dedo/policy"
)

// Scopes returns the scopes granted to the caller. Auth middleware supplies it.
type Scopes func(ctx context.Context) []string

// Config wires the interceptor to a service.
type Config struct {
	Table         policy.Table
	Scopes        Scopes
	EnforceScopes bool // false logs denials only, for rollout
	Log           *slog.Logger
}

// Interceptor applies the declared operational contract to every call.
type Interceptor struct{ cfg Config }

var _ connect.Interceptor = (*Interceptor)(nil)

// New returns an interceptor for the given configuration. It fails when the
// table or the scope source is missing, since either would leave every
// method open.
func New(cfg Config) (*Interceptor, error) {
	if len(cfg.Table) == 0 {
		return nil, errors.New("interceptor: empty policy table")
	}
	if cfg.Scopes == nil {
		return nil, errors.New("interceptor: no scope source")
	}
	if cfg.Log == nil {
		cfg.Log = slog.Default()
	}
	return &Interceptor{cfg: cfg}, nil
}

// WrapUnary checks scopes, then sets headers and audits errors.
func (i *Interceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		if req.Spec().IsClient {
			return next(ctx, req)
		}
		m, err := i.authorize(ctx, req.Spec().Procedure)
		if err != nil {
			return nil, err
		}
		res, err := next(ctx, req)
		if err != nil {
			i.auditError(ctx, m, err)
			return nil, err
		}
		setHeaders(res.Header(), req.HTTPMethod(), m.Rules)
		return res, nil
	}
}

// WrapStreamingClient leaves outbound streams unchanged.
func (i *Interceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

// WrapStreamingHandler checks scopes on inbound streams.
func (i *Interceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		if _, err := i.authorize(ctx, conn.Spec().Procedure); err != nil {
			return err
		}
		return next(ctx, conn)
	}
}
