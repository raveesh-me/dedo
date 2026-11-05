package rules

import (
	"context"
	"regexp"
	"strings"

	"buf.build/go/bufplugin/check"
	"buf.build/go/bufplugin/check/checkutil"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
)

// eventSuffix is what follows the package prefix of an event type: the
// resource and the past-tense verb, each in snake_case.
var eventSuffix = regexp.MustCompile(`^[a-z][a-z0-9_]*\.[a-z][a-z0-9_]*$`)

var eventTypes = lintRule(
	idEventTypes,
	"Checks that the event types an RPC emits are <context>.<module>.<resource>.<verb> of its own package, each listed once, on an RPC that changes state.",
	checkutil.NewMethodRuleHandler(checkEventTypes, checkutil.WithoutImports()),
)

func checkEventTypes(_ context.Context, w check.ResponseWriter, _ check.Request, m protoreflect.MethodDescriptor) error {
	emits := methodRules(m).GetEmits()
	if len(emits) == 0 {
		return nil
	}
	if idempotency(m) == descriptorpb.MethodOptions_NO_SIDE_EFFECTS {
		w.AddAnnotation(check.WithDescriptor(m),
			messagef(idEventTypes, "RPC %q is NO_SIDE_EFFECTS, so it has nothing to announce; only an RPC that changes state emits events.", m.Name()))
	}
	prefix := eventPrefix(m.ParentFile().Package())
	seen := map[string]bool{}
	for _, t := range emits {
		if seen[t] {
			w.AddAnnotation(check.WithDescriptor(m),
				messagef(idEventTypes, "RPC %q lists event type %q more than once.", m.Name(), t))
		}
		seen[t] = true
		suffix, ok := strings.CutPrefix(t, prefix+".")
		if !ok || !eventSuffix.MatchString(suffix) {
			w.AddAnnotation(check.WithDescriptor(m),
				messagef(idEventTypes, "Event type %q must be %s.<resource>.<past-tense verb>, in snake_case, such as %s.order.cancelled.", t, prefix, prefix))
		}
	}
	return nil
}

// eventPrefix returns the part of a package that starts its event types:
// the package without its version, so acme.orders.v1 gives acme.orders.
// An event carries no payload, so its type does not change with the
// package's major version.
func eventPrefix(pkg protoreflect.FullName) string {
	segments := strings.Split(string(pkg), ".")
	if len(segments) > 1 && versionPattern.MatchString(segments[len(segments)-1]) {
		segments = segments[:len(segments)-1]
	}
	return strings.Join(segments, ".")
}
