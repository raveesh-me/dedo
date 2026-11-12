package telemetry

import (
	"context"
	"errors"
	"strings"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	"connectrpc.com/connect/v2/connectproto"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
)

// outcome is how an RPC ended.
type outcome struct {
	ok      bool
	code    connect.Code
	message string
	reason  string // the ErrorInfo reason, or ""
}

// outcomeOf reads how an RPC ended from its error.
func outcomeOf(err error) outcome {
	if err == nil || connecthttp.IsNotModifiedError(err) {
		// Not modified is a cache hit, sent as a 304: a success.
		return outcome{ok: true}
	}
	var cerr *connect.Error
	switch {
	case errors.As(err, &cerr):
	case errors.Is(err, context.Canceled):
		return outcome{code: connect.CodeCanceled, message: err.Error()}
	case errors.Is(err, context.DeadlineExceeded):
		return outcome{code: connect.CodeDeadlineExceeded, message: err.Error()}
	default:
		return outcome{code: connect.CodeUnknown, message: err.Error()}
	}
	return outcome{code: cerr.Code(), message: cerr.Message(), reason: reasonOf(cerr)}
}

// reasonOf returns the reason of the error's first ErrorInfo detail.
func reasonOf(cerr *connect.Error) string {
	for _, detail := range cerr.Details() {
		value, err := connectproto.UnmarshalErrorDetail(detail)
		if info, ok := value.(*errdetails.ErrorInfo); err == nil && ok {
			return info.GetReason()
		}
	}
	return ""
}

// statusCode is rpc.status_code: the code's canonical upper-case name.
func (o outcome) statusCode() string {
	if o.ok {
		return "OK"
	}
	return strings.ToUpper(o.code.String())
}

// serverErrorCodes are the codes a server span counts as errors: the server
// failed, rather than the caller (semantic conventions for Connect RPC).
var serverErrorCodes = map[connect.Code]bool{
	connect.CodeUnknown:          true,
	connect.CodeDeadlineExceeded: true,
	connect.CodeUnimplemented:    true,
	connect.CodeInternal:         true,
	connect.CodeUnavailable:      true,
	connect.CodeDataLoss:         true,
}

// classifier decides whether a failed RPC counts as an error on one side.
type classifier func(procedure string, o outcome) bool

// serverClassifier counts the server error codes, unless the RPC's
// contract classifies the outcome's reason.
func serverClassifier(declared map[string]contract) classifier {
	return func(procedure string, o outcome) bool {
		if o.ok {
			return false
		}
		if isErr, ok := declared[procedure].classified[o.reason]; ok && o.reason != "" {
			return isErr
		}
		return serverErrorCodes[o.code]
	}
}

// clientIsError counts every code but OK: the caller did not get what it
// asked for.
func clientIsError(_ string, o outcome) bool { return !o.ok }

// statusAttributes are the attributes that describe the outcome, on spans
// and metrics alike. error.type is set only when the outcome counts as an
// error on this side, so that it agrees with the span status.
func statusAttributes(o outcome, isErr bool) []attribute.KeyValue {
	out := []attribute.KeyValue{attrRPCStatusCode.String(o.statusCode())}
	if isErr {
		out = append(out, attrErrorType.String(o.statusCode()))
	}
	if o.reason != "" {
		out = append(out, AttrErrorReason.String(o.reason))
	}
	return out
}

// spanStatus is the span status of the outcome.
func spanStatus(o outcome, isErr bool) (codes.Code, string) {
	if !isErr {
		return codes.Unset, ""
	}
	return codes.Error, o.message
}
