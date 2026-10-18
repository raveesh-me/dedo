package rules

import (
	"context"
	"regexp"

	"buf.build/go/bufplugin/check"
	"buf.build/go/bufplugin/check/checkutil"
	"google.golang.org/genproto/googleapis/rpc/code"
	"google.golang.org/protobuf/reflect/protoreflect"
)

var reasonPattern = regexp.MustCompile(`^[A-Z][A-Z0-9]*(_[A-Z0-9]+)*$`)

var errorReasonFormat = lintRule(
	"RAVEESH_ME_ERROR_REASON_FORMAT",
	"Checks that error reasons are UPPER_SNAKE_CASE and their codes are neither OK nor UNKNOWN.",
	checkutil.NewMethodRuleHandler(checkErrorReasons, checkutil.WithoutImports()),
)

func checkErrorReasons(_ context.Context, w check.ResponseWriter, _ check.Request, m protoreflect.MethodDescriptor) error {
	for _, e := range methodRules(m).GetErrors() {
		if !reasonPattern.MatchString(e.GetReason()) {
			w.AddAnnotation(check.WithDescriptor(m),
				check.WithMessagef("Error reason %q must be UPPER_SNAKE_CASE.", e.GetReason()))
		}
		if c := e.GetCode(); c == code.Code_OK || c == code.Code_UNKNOWN {
			w.AddAnnotation(check.WithDescriptor(m),
				check.WithMessagef("Error reason %q must not use code %s.", e.GetReason(), c))
		}
	}
	return nil
}
