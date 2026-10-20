// Package interceptor applies the declared operational contract of each RPC:
// it checks scopes, sets cache and deprecation headers, and logs undeclared
// error reasons.
package interceptor

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"connectrpc.com/connect/v2"

	"github.com/raveesh-me/dedo/policy"
)

// Scopes returns the scopes granted to the caller. An auth interceptor that
// runs before this one supplies them.
type Scopes func(ctx context.Context) []string

// Config wires the interceptor to a service.
type Config struct {
	Table         policy.Table
	Scopes        Scopes
	EnforceScopes bool // false logs denials only, for rollout
	// Tenants decides tenant access for multi-tenant services. It is
	// required when the table has one, and always enforced: a tenant
	// boundary has no audit mode.
	Tenants TenantAccess
	Log     *slog.Logger
}

// New returns a server interceptor that applies the declared operational
// contract to every call, on every transport that reaches the server:
// Connect, gRPC, REST through Vanguard, and MCP tool calls made in-process.
// It fails when the table, the scope source or a needed tenant check is
// missing, since any of them would leave methods open.
func New(cfg Config) (connect.ServerInterceptor, error) {
	if len(cfg.Table) == 0 {
		return nil, errors.New("interceptor: empty policy table")
	}
	if cfg.Scopes == nil {
		return nil, errors.New("interceptor: no scope source")
	}
	if cfg.Tenants == nil {
		for _, m := range cfg.Table {
			if m.Tenant != nil {
				return nil, fmt.Errorf("interceptor: %s is tenant-scoped, but Config.Tenants is not set", m.Procedure)
			}
		}
	}
	if cfg.Log == nil {
		cfg.Log = slog.Default()
	}
	i := &guard{cfg: cfg}
	return i.wrap, nil
}

type guard struct{ cfg Config }

// wrap checks scopes, sets the response headers the method declares, and
// audits the error the call ends with.
func (i *guard) wrap(next connect.ServerFunc) connect.ServerFunc {
	return func(ctx context.Context, spec connect.Spec, stream connect.ServerStream) error {
		m, err := i.authorize(ctx, spec.Procedure)
		if err != nil {
			return err
		}
		if info, ok := connect.CallInfoForServerContext(ctx); ok {
			// Deprecation headers go on every response, errors included;
			// Cache-Control only on a response message, so an error is
			// never cached.
			setDeprecation(info.ResponseHeader(), m.Rules)
			if cacheable(ctx, info, m.Rules) {
				stream = &cacheStream{ServerStream: stream, header: info.ResponseHeader(), value: cacheControl(m.Rules.GetCache())}
			}
		}
		var tenants *tenantStream
		if m.Tenant != nil {
			tenants = &tenantStream{ServerStream: stream, ctx: ctx, guard: i, method: m}
			stream = tenants
		}
		if err := next(ctx, spec, stream); err != nil {
			// A tenant denial is the guard's own answer, not the handler's,
			// so it is not audited against the declared reasons.
			if tenants == nil || !tenants.denied {
				i.auditError(ctx, m, err)
			}
			return err
		}
		return nil
	}
}
