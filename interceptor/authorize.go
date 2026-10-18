package interceptor

import (
	"context"
	"errors"
	"slices"

	"connectrpc.com/connect"

	"github.com/raveesh-me/http-proto-lint/flowid"
	"github.com/raveesh-me/http-proto-lint/policy"
)

// authorize runs before validation, so an unauthorised caller learns nothing
// from validation errors.
func (i *Interceptor) authorize(ctx context.Context, procedure string) (policy.Method, error) {
	m, ok := i.cfg.Table[procedure]
	if !ok {
		return m, connect.NewError(connect.CodeUnimplemented, errors.New("procedure has no policy"))
	}
	if granted(i.cfg.Scopes(ctx), m.Rules.GetScopes()) {
		return m, nil
	}
	if !i.cfg.EnforceScopes {
		i.cfg.Log.WarnContext(ctx, "scope check failed in audit mode",
			"procedure", procedure, "flow_id", flowid.From(ctx))
		return m, nil
	}
	return m, connect.NewError(connect.CodePermissionDenied, errors.New("missing required scope"))
}

// granted reports whether the caller holds at least one declared scope.
func granted(have, want []string) bool {
	for _, w := range want {
		if slices.Contains(have, w) {
			return true
		}
	}
	return false
}
