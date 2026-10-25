package app

import (
	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connectproto"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
)

// The error reasons the contract declares.
const (
	reasonContactNotFound  = "CONTACT_NOT_FOUND"
	reasonAlreadyExists    = "CONTACT_ALREADY_EXISTS"
	reasonEmailTaken       = "EMAIL_TAKEN"
	reasonLocationNotFound = "LOCATION_NOT_FOUND"
	reasonInvalidPageToken = "INVALID_PAGE_TOKEN"
	errorDomain            = "contacts.raveesh.me"
)

// apiError returns a Connect error carrying an ErrorInfo with the reason.
func apiError(code connect.Code, reason, message string) error {
	err := connect.NewError(code, message)
	if detail, derr := connectproto.NewErrorDetail(&errdetails.ErrorInfo{Reason: reason, Domain: errorDomain}); derr == nil {
		err = err.WithDetail(detail)
	}
	return err
}

func contactNotFound(name string) error {
	return apiError(connect.CodeNotFound, reasonContactNotFound, "no contact named "+name)
}

func locationNotFound(name string) error {
	return apiError(connect.CodeNotFound, reasonLocationNotFound, "no location named "+name)
}

func invalidPageToken() error {
	return apiError(connect.CodeInvalidArgument, reasonInvalidPageToken, "malformed page token")
}
