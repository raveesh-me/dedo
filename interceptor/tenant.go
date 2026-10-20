package interceptor

import (
	"context"

	"connectrpc.com/connect/v2"
	"google.golang.org/protobuf/proto"

	"github.com/raveesh-me/dedo/flowid"
	"github.com/raveesh-me/dedo/policy"
)

// TenantAccess reports whether the caller may act in a tenant, such as "1"
// for a request naming locations/1/todos/2. An auth interceptor that runs
// before this one supplies the caller. The tenant is "-" when the request
// reads across tenants (AIP-159), so grant it only to callers who may see
// every tenant.
type TenantAccess func(ctx context.Context, tenant string) bool

// tenantStream checks the tenant of every request message the handler
// receives, before the handler or the validation interceptor sees it.
type tenantStream struct {
	connect.ServerStream
	ctx    context.Context
	guard  *guard
	method policy.Method
	denied bool
}

func (s *tenantStream) Receive(msg any) error {
	if err := s.ServerStream.Receive(msg); err != nil {
		return err
	}
	req, ok := msg.(proto.Message)
	if !ok {
		return connect.NewError(connect.CodeInternal, "request is not a protobuf message")
	}
	t := s.method.Tenant
	tenant, ok := t.Of(req)
	if !ok {
		s.denied = true
		return connect.NewError(connect.CodeInvalidArgument, t.Field+" must name a resource under "+t.Collection+"/")
	}
	if !s.guard.cfg.Tenants(s.ctx, tenant) {
		s.denied = true
		s.guard.cfg.Log.WarnContext(s.ctx, "tenant check failed",
			"procedure", s.method.Procedure, "tenant", tenant, "flow_id", flowid.From(s.ctx))
		// The same answer whether or not the resource exists (AIP-211), so
		// a caller cannot probe another tenant.
		return connect.NewError(connect.CodePermissionDenied, "no access to this tenant")
	}
	return nil
}
