package house

import (
	"fmt"
	"sync"

	"buf.build/go/bufplugin/check"
	"buf.build/go/bufplugin/descriptor"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/dynamicpb"
)

// The options a check reads, as the variables options and service_options:
// every extension set on an element's options, keyed by its full name, such
// as "acme.auth.v1.method". A message value is a map of its fields by their
// names in the .proto; an enum is its value's name; a repeated field is a
// list and a map field a map with string keys. A scalar field that is unset
// reads as its zero value and an unset repeated or map field as empty; an
// unset message field, and an unset member of a oneof (a proto3 optional
// field among them), are left out, so a check tests them with has().
//
// Options of any extension the request's files declare are read, not only
// dedo's: buf passes every file a module imports, and the extensions are
// resolved from those files.

// typeCache holds the extensions declared by the files of each side of the
// request being checked. A request is checked element by element, so they
// are built once per request and side.
var typeCache struct {
	sync.Mutex
	req   check.Request
	types [2]*protoregistry.Types // the request's files, then the against files
}

func requestTypes(req check.Request, against bool) (*protoregistry.Types, error) {
	typeCache.Lock()
	defer typeCache.Unlock()
	if typeCache.req != req {
		typeCache.req, typeCache.types = req, [2]*protoregistry.Types{}
	}
	side := 0
	if against {
		side = 1
	}
	if typeCache.types[side] != nil {
		return typeCache.types[side], nil
	}
	files := req.FileDescriptors()
	if against {
		files = req.AgainstFileDescriptors()
	}
	types, err := extensionTypes(files)
	if err != nil {
		return nil, err
	}
	typeCache.types[side] = types
	return types, nil
}

// extensionTypes registers every extension the files declare, at the top
// level or nested in a message.
func extensionTypes(files []descriptor.FileDescriptor) (*protoregistry.Types, error) {
	types := new(protoregistry.Types)
	var register func(protoreflect.ExtensionDescriptors, protoreflect.MessageDescriptors) error
	register = func(xs protoreflect.ExtensionDescriptors, ms protoreflect.MessageDescriptors) error {
		for i := range xs.Len() {
			if err := types.RegisterExtension(dynamicpb.NewExtensionType(xs.Get(i))); err != nil {
				return fmt.Errorf("extension %s: %w", xs.Get(i).FullName(), err)
			}
		}
		for i := range ms.Len() {
			if err := register(ms.Get(i).Extensions(), ms.Get(i).Messages()); err != nil {
				return err
			}
		}
		return nil
	}
	for _, f := range files {
		fd := f.ProtoreflectFileDescriptor()
		if err := register(fd.Extensions(), fd.Messages()); err != nil {
			return nil, err
		}
	}
	return types, nil
}

// options returns the extensions set on opts, resolved with types.
func options(opts proto.Message, types *protoregistry.Types) (map[string]any, error) {
	out := map[string]any{}
	if opts == nil || !opts.ProtoReflect().IsValid() {
		return out, nil
	}
	// The plugin parsed the options knowing only the extensions compiled
	// into it, and kept the others as unknown fields. Parsing them again
	// with the request's own extensions reads both.
	b, err := proto.MarshalOptions{Deterministic: true}.Marshal(opts)
	if err != nil {
		return nil, err
	}
	m := opts.ProtoReflect().New().Interface()
	if err := (proto.UnmarshalOptions{Resolver: types}).Unmarshal(b, m); err != nil {
		return nil, err
	}
	m.ProtoReflect().Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		if fd.IsExtension() {
			out[string(fd.FullName())] = celValue(fd, v)
		}
		return true
	})
	return out, nil
}

// celValue converts a field's value to the types CEL reads natively.
func celValue(fd protoreflect.FieldDescriptor, v protoreflect.Value) any {
	switch {
	case fd.IsList():
		l := v.List()
		out := make([]any, l.Len())
		for i := range l.Len() {
			out[i] = celScalar(fd, l.Get(i))
		}
		return out
	case fd.IsMap():
		out := map[string]any{}
		v.Map().Range(func(k protoreflect.MapKey, mv protoreflect.Value) bool {
			out[k.String()] = celScalar(fd.MapValue(), mv)
			return true
		})
		return out
	}
	return celScalar(fd, v)
}

func celScalar(fd protoreflect.FieldDescriptor, v protoreflect.Value) any {
	switch fd.Kind() {
	case protoreflect.MessageKind, protoreflect.GroupKind:
		return celMessage(v.Message())
	case protoreflect.EnumKind:
		if ev := fd.Enum().Values().ByNumber(v.Enum()); ev != nil {
			return string(ev.Name())
		}
		return int64(v.Enum())
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind,
		protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
		return v.Int()
	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind, protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		return v.Uint()
	case protoreflect.FloatKind, protoreflect.DoubleKind:
		return v.Float()
	case protoreflect.BoolKind:
		return v.Bool()
	case protoreflect.StringKind:
		return v.String()
	case protoreflect.BytesKind:
		return v.Bytes()
	}
	return v.Interface()
}

// celMessage maps a message's fields by name. Unset message fields and
// unset oneof members are left out, so has() tells them apart.
func celMessage(m protoreflect.Message) map[string]any {
	out := map[string]any{}
	fields := m.Descriptor().Fields()
	for i := range fields.Len() {
		fd := fields.Get(i)
		single := !fd.IsList() && !fd.IsMap()
		if !m.Has(fd) && (fd.ContainingOneof() != nil || (single && fd.Message() != nil)) {
			continue
		}
		out[string(fd.Name())] = celValue(fd, m.Get(fd))
	}
	return out
}

// withOptions adds the options variables of d to vars: options, and for a
// method its service's as service_options.
func withOptions(vars map[string]any, d protoreflect.Descriptor, types *protoregistry.Types) error {
	opts, err := options(d.Options(), types)
	if err != nil {
		return fmt.Errorf("the options of %s could not be read: %w", d.FullName(), err)
	}
	vars["options"] = opts
	if m, ok := d.(protoreflect.MethodDescriptor); ok {
		svc, err := options(m.Parent().Options(), types)
		if err != nil {
			return fmt.Errorf("the options of %s could not be read: %w", m.Parent().FullName(), err)
		}
		vars["service_options"] = svc
	}
	return nil
}
