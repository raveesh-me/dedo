package rules

import (
	"context"
	"regexp"
	"strings"
	"unicode"

	"buf.build/go/bufplugin/check"
	"buf.build/go/bufplugin/check/checkutil"
	"google.golang.org/genproto/googleapis/api/annotations"
	"google.golang.org/genproto/googleapis/rpc/code"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
)

// eventSuffix is what follows the package prefix of an event type: the
// resource and the past-tense verb, each in snake_case.
var eventSuffix = regexp.MustCompile(`^[a-z][a-z0-9_]*\.[a-z][a-z0-9_]*$`)

var eventTypes = lintRule(
	idEventTypes,
	"Checks that the event types an RPC emits are <context>.<module>.<resource>.<verb> of its own package, naming a resource the package declares, each listed once, on an RPC that changes state.",
	checkutil.NewMethodRuleHandler(checkEventTypes, checkutil.WithoutImports()),
)

// reasonNotPublished is the reason events.Publish returns, with
// UNAVAILABLE, when the bus did not take an event in time
// (events.ReasonNotPublished in the runtime).
const reasonNotPublished = "EVENT_NOT_PUBLISHED"

var eventNotPublishedDeclared = lintRule(
	idEventNotPublishedDeclared,
	"Checks that an RPC that emits events declares the error EVENT_NOT_PUBLISHED with UNAVAILABLE, which it returns when the bus does not take an event in time.",
	checkutil.NewMethodRuleHandler(checkEventNotPublishedDeclared, checkutil.WithoutImports()),
)

func checkEventNotPublishedDeclared(_ context.Context, w check.ResponseWriter, _ check.Request, m protoreflect.MethodDescriptor) error {
	rules := methodRules(m)
	if len(rules.GetEmits()) == 0 {
		return nil
	}
	for _, e := range rules.GetErrors() {
		if e.GetReason() == reasonNotPublished && e.GetCode() == code.Code_UNAVAILABLE {
			return nil
		}
	}
	w.AddAnnotation(check.WithDescriptor(m),
		messagef(idEventNotPublishedDeclared, "RPC %q emits events, so it declares errors: {reason: %q, code: UNAVAILABLE}: the error it returns when the bus does not take an event in time, so the caller retries.", m.Name(), reasonNotPublished))
	return nil
}

func checkEventTypes(_ context.Context, w check.ResponseWriter, req check.Request, m protoreflect.MethodDescriptor) error {
	emits := methodRules(m).GetEmits()
	if len(emits) == 0 {
		return nil
	}
	if idempotency(m) == descriptorpb.MethodOptions_NO_SIDE_EFFECTS {
		w.AddAnnotation(check.WithDescriptor(m),
			messagef(idEventTypes, "RPC %q is NO_SIDE_EFFECTS, so it has nothing to announce; only an RPC that changes state emits events.", m.Name()))
	}
	prefix := eventPrefix(m.ParentFile().Package())
	resources := packageResources(req, m.ParentFile().Package())
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
			continue
		}
		if resource, _, _ := strings.Cut(suffix, "."); !resources[resource] {
			w.AddAnnotation(check.WithDescriptor(m),
				messagef(idEventTypes, "Event type %q names %q, which is no resource of package %s; an event's subject is a resource's name. Declare the resource with google.api.resource, or name one that is declared.", t, resource, m.ParentFile().Package()))
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

// packageResources returns the resources a package declares, by the
// snake_case singular that event types use: order_line for a resource
// whose singular is orderLine, or whose type is example.com/OrderLine.
func packageResources(req check.Request, pkg protoreflect.FullName) map[string]bool {
	out := map[string]bool{}
	add := func(res *annotations.ResourceDescriptor) {
		singular := res.GetSingular()
		if singular == "" {
			_, singular, _ = strings.Cut(res.GetType(), "/")
		}
		if singular != "" {
			out[snakeCase(singular)] = true
		}
	}
	var walk func(protoreflect.MessageDescriptors)
	walk = func(msgs protoreflect.MessageDescriptors) {
		for i := 0; i < msgs.Len(); i++ {
			m := msgs.Get(i)
			if res := extension[*annotations.ResourceDescriptor](m.Options(), annotations.E_Resource); res != nil {
				add(res)
			}
			walk(m.Messages())
		}
	}
	for _, fd := range req.FileDescriptors() {
		f := fd.ProtoreflectFileDescriptor()
		if f.Package() != pkg {
			continue
		}
		definitions, _ := proto.GetExtension(f.Options(), annotations.E_ResourceDefinition).([]*annotations.ResourceDescriptor)
		for _, res := range definitions {
			add(res)
		}
		walk(f.Messages())
	}
	return out
}

// snakeCase turns orderLine or OrderLine into order_line, keeping an
// acronym whole: SKU is sku, APIKey api_key.
func snakeCase(s string) string {
	r := []rune(s)
	var b strings.Builder
	for i, c := range r {
		if unicode.IsUpper(c) && i > 0 {
			prev := r[i-1]
			nextLower := i+1 < len(r) && unicode.IsLower(r[i+1])
			if unicode.IsLower(prev) || unicode.IsDigit(prev) || (unicode.IsUpper(prev) && nextLower) {
				b.WriteByte('_')
			}
		}
		b.WriteRune(unicode.ToLower(c))
	}
	return b.String()
}
