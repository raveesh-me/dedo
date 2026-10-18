package rules

import (
	"context"

	"buf.build/go/bufplugin/check"
	"buf.build/go/bufplugin/check/checkutil"
	"google.golang.org/protobuf/reflect/protoreflect"

	hplv1 "github.com/raveesh-me/http-proto-lint/gen/go/raveesh_me/http_proto_lint/v1"
)

var serviceRulesRequired = lintRule(
	idServiceRulesRequired,
	"Checks that every service declares raveesh_me.http_proto_lint.v1.service with an audience and an owner team.",
	checkutil.NewServiceRuleHandler(checkServiceRules, checkutil.WithoutImports()),
)

func checkServiceRules(_ context.Context, w check.ResponseWriter, _ check.Request, s protoreflect.ServiceDescriptor) error {
	rules := serviceRules(s)
	switch {
	case rules == nil:
		w.AddAnnotation(check.WithDescriptor(s),
			messagef(idServiceRulesRequired, "Service %q has no raveesh_me.http_proto_lint.v1.service option.", s.Name()))
	case rules.GetAudience() == hplv1.Audience_AUDIENCE_UNSPECIFIED:
		w.AddAnnotation(check.WithDescriptor(s),
			messagef(idServiceRulesRequired, "Service %q declares no audience.", s.Name()))
	case rules.GetOwnerTeam() == "":
		w.AddAnnotation(check.WithDescriptor(s),
			messagef(idServiceRulesRequired, "Service %q declares no owner team.", s.Name()))
	}
	return nil
}
