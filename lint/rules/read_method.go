package rules

import (
	"context"
	"strings"

	"buf.build/go/bufplugin/check"
	"buf.build/go/bufplugin/check/checkutil"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
)

var readMethodNoSideEffects = lintRule(
	idReadMethodNoSideEffects,
	"Checks that unary Get and List RPCs set idempotency_level = NO_SIDE_EFFECTS.",
	checkutil.NewMethodRuleHandler(checkReadMethod, checkutil.WithoutImports()),
)

func checkReadMethod(_ context.Context, w check.ResponseWriter, _ check.Request, m protoreflect.MethodDescriptor) error {
	if m.IsStreamingClient() || m.IsStreamingServer() || !hasVerb(m, "Get", "List") {
		return nil
	}
	if idempotency(m) != descriptorpb.MethodOptions_NO_SIDE_EFFECTS {
		w.AddAnnotation(check.WithDescriptor(m),
			messagef(idReadMethodNoSideEffects, "RPC %q must set idempotency_level = NO_SIDE_EFFECTS.", m.Name()))
	}
	return nil
}

// hasVerb reports whether the method name starts with one of the verbs
// followed by an upper-case letter, so GetBook matches and Getaway does not.
func hasVerb(m protoreflect.MethodDescriptor, verbs ...string) bool {
	name := string(m.Name())
	for _, verb := range verbs {
		rest, ok := strings.CutPrefix(name, verb)
		if ok && rest != "" && rest[0] >= 'A' && rest[0] <= 'Z' {
			return true
		}
	}
	return false
}
