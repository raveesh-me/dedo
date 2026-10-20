package interceptor

import (
	"context"
	"errors"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connectproto"
	"google.golang.org/genproto/googleapis/rpc/errdetails"

	"github.com/raveesh-me/dedo/flowid"
	"github.com/raveesh-me/dedo/policy"
)

// auditError logs errors whose reason the method did not declare. Version 1
// logs only; version 2 enforces.
func (i *guard) auditError(ctx context.Context, m policy.Method, err error) {
	var cerr *connect.Error
	if !errors.As(err, &cerr) {
		i.cfg.Log.WarnContext(ctx, "error is not a connect error",
			"procedure", m.Procedure, "flow_id", flowid.From(ctx))
		return
	}
	info := errorInfo(cerr)
	if info == nil {
		i.cfg.Log.WarnContext(ctx, "error without ErrorInfo",
			"procedure", m.Procedure, "code", cerr.Code().String(), "flow_id", flowid.From(ctx))
		return
	}
	if declared(m, info.GetReason(), cerr.Code()) {
		return
	}
	i.cfg.Log.WarnContext(ctx, "undeclared error reason",
		"procedure", m.Procedure, "reason", info.GetReason(),
		"code", cerr.Code().String(), "flow_id", flowid.From(ctx))
}

// errorInfo returns the first ErrorInfo detail of the error, or nil.
func errorInfo(cerr *connect.Error) *errdetails.ErrorInfo {
	for _, detail := range cerr.Details() {
		value, err := connectproto.UnmarshalErrorDetail(detail)
		if info, ok := value.(*errdetails.ErrorInfo); err == nil && ok {
			return info
		}
	}
	return nil
}

// declared reports whether the method declares the reason with the code.
// Connect codes share gRPC's numbering, so google.rpc.Code compares directly.
func declared(m policy.Method, reason string, code connect.Code) bool {
	for _, e := range m.Rules.GetErrors() {
		if e.GetReason() == reason && int32(e.GetCode()) == int32(code) {
			return true
		}
	}
	return false
}
