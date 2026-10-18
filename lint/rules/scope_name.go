package rules

import (
	"context"
	"regexp"

	"buf.build/go/bufplugin/check"
	"buf.build/go/bufplugin/check/checkutil"
	"google.golang.org/protobuf/reflect/protoreflect"
)

var scopePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*\.[a-z][a-z0-9_]*\.(read|write)$`)

var scopeNameFormat = lintRule(
	"HOUSE_SCOPE_NAME_FORMAT",
	"Checks that scopes are named <app>.<resource>.<read|write>.",
	checkutil.NewMethodRuleHandler(checkScopeNames, checkutil.WithoutImports()),
)

func checkScopeNames(_ context.Context, w check.ResponseWriter, _ check.Request, m protoreflect.MethodDescriptor) error {
	for _, scope := range methodRules(m).GetScopes() {
		if !scopePattern.MatchString(scope) {
			w.AddAnnotation(check.WithDescriptor(m),
				check.WithMessagef("Scope %q must match <app>.<resource>.<read|write>.", scope))
		}
	}
	return nil
}
