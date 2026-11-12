// Package connect stands in for connect-go v2.
package connect

import "context"

type Spec struct{}

type ServerStream interface{}

type ServerFunc func(ctx context.Context, spec Spec, stream ServerStream) error

type ServerInterceptor func(next ServerFunc) ServerFunc

type Server struct{}

func NewServer(interceptors ...ServerInterceptor) *Server { return &Server{} }

type Code int

const (
	CodeNotFound    Code = 5
	CodeUnavailable Code = 14
)

type Error struct{}

func NewError(code Code, message string) *Error { return &Error{} }

func (e *Error) Error() string { return "" }

func (e *Error) WithDetail(detail any) *Error { return e }
