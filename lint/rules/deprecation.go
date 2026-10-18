package rules

import (
	"context"

	"buf.build/go/bufplugin/check"
	"buf.build/go/bufplugin/check/checkutil"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
)

var deprecationConsistent = lintRule(
	"HOUSE_DEPRECATION_CONSISTENT",
	"Checks that deprecated = true and a Deprecation option appear together, sunset follows deprecation, and the successor exists.",
	checkutil.NewMethodRuleHandler(checkDeprecation, checkutil.WithoutImports()),
)

func checkDeprecation(_ context.Context, w check.ResponseWriter, req check.Request, m protoreflect.MethodDescriptor) error {
	dep := methodRules(m).GetDeprecation()
	deprecated := isDeprecated(m)
	switch {
	case deprecated && dep == nil:
		w.AddAnnotation(check.WithDescriptor(m),
			check.WithMessagef("RPC %q is deprecated but declares no deprecation schedule.", m.Name()))
		return nil
	case !deprecated && dep != nil:
		w.AddAnnotation(check.WithDescriptor(m),
			check.WithMessagef("RPC %q declares a deprecation schedule but does not set deprecated = true.", m.Name()))
		return nil
	case dep == nil:
		return nil
	}
	if dep.GetDeprecationTime() == nil || dep.GetSunsetTime() == nil {
		w.AddAnnotation(check.WithDescriptor(m),
			check.WithMessagef("RPC %q deprecation schedule needs both a deprecation and a sunset time.", m.Name()))
	} else if !dep.GetSunsetTime().AsTime().After(dep.GetDeprecationTime().AsTime()) {
		w.AddAnnotation(check.WithDescriptor(m),
			check.WithMessagef("RPC %q sunset time must follow its deprecation time.", m.Name()))
	}
	if successor := dep.GetSuccessor(); successor != "" && !methodExists(req, protoreflect.FullName(successor)) {
		w.AddAnnotation(check.WithDescriptor(m),
			check.WithMessagef("RPC %q names successor %q, which does not exist.", m.Name(), successor))
	}
	return nil
}

func isDeprecated(m protoreflect.MethodDescriptor) bool {
	opts, _ := m.Options().(*descriptorpb.MethodOptions)
	return opts.GetDeprecated()
}

// methodExists reports whether any file in the request, imports included,
// defines the fully qualified method.
func methodExists(req check.Request, name protoreflect.FullName) bool {
	for _, f := range req.FileDescriptors() {
		services := f.ProtoreflectFileDescriptor().Services()
		for i := 0; i < services.Len(); i++ {
			if services.Get(i).Methods().ByName(name.Name()) != nil && services.Get(i).FullName() == name.Parent() {
				return true
			}
		}
	}
	return false
}
