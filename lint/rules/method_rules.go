package rules

import (
	"context"

	"buf.build/go/bufplugin/check"
	"buf.build/go/bufplugin/check/checkutil"
	"google.golang.org/protobuf/reflect/protoreflect"
)

var methodRulesRequired = lintRule(
	idMethodRulesRequired,
	"Checks that every RPC declares raveesh_me.http_proto_lint.v1.method with a scope and an error reason.",
	checkutil.NewMethodRuleHandler(checkMethodRules, checkutil.WithoutImports()),
)

func checkMethodRules(_ context.Context, w check.ResponseWriter, _ check.Request, m protoreflect.MethodDescriptor) error {
	rules := methodRules(m)
	switch {
	case rules == nil:
		w.AddAnnotation(check.WithDescriptor(m),
			messagef(idMethodRulesRequired, "RPC %q has no raveesh_me.http_proto_lint.v1.method option.", m.Name()))
	case len(rules.GetScopes()) == 0:
		w.AddAnnotation(check.WithDescriptor(m),
			messagef(idMethodRulesRequired, "RPC %q declares no scopes.", m.Name()))
	case len(rules.GetErrors()) == 0:
		w.AddAnnotation(check.WithDescriptor(m),
			messagef(idMethodRulesRequired, "RPC %q declares no error reasons.", m.Name()))
	}
	return nil
}
