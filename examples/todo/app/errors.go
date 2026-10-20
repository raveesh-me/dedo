package app

import (
	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connectproto"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
)

// The error reasons the contract declares.
const (
	reasonNotFound         = "TODO_NOT_FOUND"
	reasonAlreadyExists    = "TODO_ALREADY_EXISTS"
	reasonTitleRequired    = "TITLE_REQUIRED"
	reasonInvalidPageToken = "INVALID_PAGE_TOKEN"
	errorDomain            = "todo.raveesh.me"
)

// apiError returns a Connect error carrying an ErrorInfo with the reason.
func apiError(code connect.Code, reason, message string) error {
	err := connect.NewError(code, message)
	if detail, derr := connectproto.NewErrorDetail(&errdetails.ErrorInfo{Reason: reason, Domain: errorDomain}); derr == nil {
		err = err.WithDetail(detail)
	}
	return err
}

func notFound(name string) error {
	return apiError(connect.CodeNotFound, reasonNotFound, "no to-do named "+name)
}
