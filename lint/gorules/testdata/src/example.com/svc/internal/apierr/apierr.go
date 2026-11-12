// Package apierr builds the errors handlers return, as services do.
package apierr

import (
	"context"

	"connectrpc.com/connect/v2"
	"google.golang.org/genproto/googleapis/rpc/errdetails"

	"github.com/raveesh-me/dedo/events"
)

// New puts reason into an ErrorInfo: a call's constant reason is checked.
func New(code connect.Code, reason, message string) *connect.Error {
	info := &errdetails.ErrorInfo{Reason: reason, Domain: "example.com"}
	return connect.NewError(code, message).WithDetail(info)
}

// NotFound passes reason on to New.
func NotFound(reason, message string) *connect.Error {
	return New(connect.CodeNotFound, reason, message)
}

// Unavailable sets the reason field by assignment.
func Unavailable(reason string) *connect.Error {
	info := &errdetails.ErrorInfo{}
	info.Reason = reason
	return connect.NewError(connect.CodeUnavailable, "").WithDetail(info)
}

// Prefixed changes reason before it reaches the ErrorInfo: not followed.
func Prefixed(reason string) *connect.Error {
	reason = "PREFIX_" + reason
	return New(connect.CodeNotFound, reason, "")
}

// Gone always returns ORDER_GONE.
func Gone() *connect.Error {
	return NotFound("ORDER_GONE", "the order is gone")
}

// OrderNotFound always returns ORDER_NOT_FOUND.
func OrderNotFound() *connect.Error {
	return NotFound("ORDER_NOT_FOUND", "no such order")
}

// FromStore returns one of several reasons, or none: not followed.
func FromStore(err error) *connect.Error {
	if err == nil {
		return nil
	}
	if err.Error() == "gone" {
		return NotFound("ORDER_GONE", "gone")
	}
	return NotFound("ORDER_NOT_FOUND", "missing")
}

// Variadic takes its reasons variadically: not followed.
func Variadic(reasons ...string) *connect.Error {
	return New(connect.CodeNotFound, reasons[0], "")
}

// Publish passes eventType on to events.Publisher.Publish.
func Publish(ctx context.Context, p *events.Publisher, eventType, name string) error {
	_, err := p.Publish(ctx, eventType, name)
	return err
}
