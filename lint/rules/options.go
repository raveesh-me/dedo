package rules

import (
	"google.golang.org/genproto/googleapis/api/annotations"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"

	dedov1 "github.com/raveesh-me/dedo/gen/go/dedo/v1"
)

// extension reads an option extension. When the plugin receives the option
// as unknown fields instead of a resolved extension (spike S2), it re-parses
// the options against the global type registry, where the generated packages
// imported here have registered the extensions.
func extension[T proto.Message](opts proto.Message, xt protoreflect.ExtensionType) T {
	var zero T
	if opts == nil || !opts.ProtoReflect().IsValid() {
		return zero
	}
	if proto.HasExtension(opts, xt) {
		v, _ := proto.GetExtension(opts, xt).(T)
		return v
	}
	if len(opts.ProtoReflect().GetUnknown()) == 0 {
		return zero
	}
	b, err := proto.Marshal(opts)
	if err != nil {
		return zero
	}
	resolved := opts.ProtoReflect().New().Interface()
	if err := (proto.UnmarshalOptions{Resolver: protoregistry.GlobalTypes}).Unmarshal(b, resolved); err != nil {
		return zero
	}
	v, _ := proto.GetExtension(resolved, xt).(T)
	return v
}

// serviceRules reads the dedo.v1.service option, or returns nil.
func serviceRules(s protoreflect.ServiceDescriptor) *dedov1.ServiceRules {
	return extension[*dedov1.ServiceRules](s.Options(), dedov1.E_Service)
}

// methodRules reads the dedo.v1.method option, or returns nil.
func methodRules(m protoreflect.MethodDescriptor) *dedov1.MethodRules {
	return extension[*dedov1.MethodRules](m.Options(), dedov1.E_Method)
}

// httpRule reads the google.api.http option, or returns nil.
func httpRule(m protoreflect.MethodDescriptor) *annotations.HttpRule {
	return extension[*annotations.HttpRule](m.Options(), annotations.E_Http)
}
