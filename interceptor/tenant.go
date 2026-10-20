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

// tenantStream checks the tenants of every request message the handler
// receives, before the handler or the validation interceptor sees it.
type tenantStream struct {
	connect.ServerStream
	ctx    context.Context
	guard  *guard
	method policy.Method
	// denial is the error the stream answered a request with, which the
	// guard does not audit as the handler's own.
	denial error
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
	tenants, ok := t.Tenants(req)
	if !ok {
		return s.deny(req, connect.NewError(connect.CodeInvalidArgument,
			t.Field+" and every resource name under "+t.Collection+"/ must be well formed, such as "+t.Collection+"/1"))
	}
	for _, tenant := range tenants {
		if !s.guard.cfg.Tenants(s.ctx, tenant) {
			s.guard.cfg.Log.WarnContext(s.ctx, "tenant check failed",
				"procedure", s.method.Procedure, "tenant", tenant, "flow_id", flowid.From(s.ctx))
			// The same answer whether or not the resource exists (AIP-211),
			// so a caller cannot probe another tenant.
			return s.deny(req, connect.NewError(connect.CodePermissionDenied, "no access to this tenant"))
		}
	}
	return nil
}

// deny clears the request, so a handler that ignores the error holds
// nothing of another tenant's, and answers with err.
func (s *tenantStream) deny(req proto.Message, err error) error {
	proto.Reset(req)
	s.denial = err
	return err
}
