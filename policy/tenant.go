package policy

import (
	"fmt"
	"regexp"
	"strings"

	"google.golang.org/genproto/googleapis/api/annotations"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// Tenant is how the runtime finds the tenant of a request to a method of a
// multi-tenant service.
type Tenant struct {
	// Collection is the tenant's collection ID, such as "locations".
	Collection string
	// Field is the path of the request field that holds a resource name
	// under the tenant, such as "parent" or "todo.name".
	Field string
	names []protoreflect.Name
}

// pathVariable matches a variable of an HTTP path template, such as
// {parent=locations/*} or {todo.name=locations/*/todos/*}.
var pathVariable = regexp.MustCompile(`\{([A-Za-z0-9_.]+)(?:=([^}]*))?\}`)

// TenantField returns the request field that an HTTP path binds to a name
// under the tenant collection: "parent" for
// /commerce/todo/v1/{parent=locations/*}/todos with collection "locations".
func TenantField(path, collection string) (string, bool) {
	for _, m := range pathVariable.FindAllStringSubmatch(path, -1) {
		segments := strings.Split(m[2], "/")
		if len(segments) >= 2 && segments[0] == collection && segments[1] == "*" {
			return m[1], true
		}
	}
	return "", false
}

// HTTPPaths lists the path of an HTTP rule and of every additional binding.
func HTTPPaths(rule *annotations.HttpRule) []string {
	var paths []string
	var path string
	switch p := rule.GetPattern().(type) {
	case *annotations.HttpRule_Get:
		path = p.Get
	case *annotations.HttpRule_Put:
		path = p.Put
	case *annotations.HttpRule_Post:
		path = p.Post
	case *annotations.HttpRule_Delete:
		path = p.Delete
	case *annotations.HttpRule_Patch:
		path = p.Patch
	case *annotations.HttpRule_Custom:
		path = p.Custom.GetPath()
	}
	if path != "" {
		paths = append(paths, path)
	}
	for _, binding := range rule.GetAdditionalBindings() {
		paths = append(paths, HTTPPaths(binding)...)
	}
	return paths
}

// MethodTenant finds the tenant field of a method of a multi-tenant service
// from its HTTP rule. Every binding must bind the same string field of the
// request to a name under the tenant, since the runtime reads that one field
// whatever the transport. buf-plugin-dedo-lint calls it too, so the lint and
// the runtime agree on where each request names its tenant.
func MethodTenant(m protoreflect.MethodDescriptor, rule *annotations.HttpRule, collection string) (*Tenant, error) {
	paths := HTTPPaths(rule)
	if len(paths) == 0 {
		return nil, fmt.Errorf("it has no HTTP binding to name its tenant in, such as {parent=%s/*}", collection)
	}
	var field string
	for _, path := range paths {
		f, ok := TenantField(path, collection)
		switch {
		case !ok:
			return nil, fmt.Errorf("HTTP path %q binds no field to %s/*", path, collection)
		case field != "" && f != field:
			return nil, fmt.Errorf("HTTP bindings name the tenant in %q and %q", field, f)
		}
		field = f
	}
	names, err := stringFieldPath(m.Input(), field)
	if err != nil {
		return nil, err
	}
	return &Tenant{Collection: collection, Field: field, names: names}, nil
}

// methodTenant reads a method's HTTP rule and finds its tenant field.
func methodTenant(m protoreflect.MethodDescriptor, collection string) (*Tenant, error) {
	rule, _ := proto.GetExtension(m.Options(), annotations.E_Http).(*annotations.HttpRule)
	tenant, err := MethodTenant(m, rule, collection)
	if err != nil {
		return nil, fmt.Errorf("policy: method %s of a multi-tenant service: %w", m.FullName(), err)
	}
	return tenant, nil
}

// stringFieldPath resolves a dotted field path to a singular string field.
func stringFieldPath(msg protoreflect.MessageDescriptor, path string) ([]protoreflect.Name, error) {
	var names []protoreflect.Name
	parts := strings.Split(path, ".")
	for i, part := range parts {
		fd := msg.Fields().ByName(protoreflect.Name(part))
		if fd == nil || fd.IsList() || fd.IsMap() {
			return nil, fmt.Errorf("request %s has no singular field %q", msg.FullName(), path)
		}
		names = append(names, fd.Name())
		if i == len(parts)-1 {
			if fd.Kind() != protoreflect.StringKind {
				return nil, fmt.Errorf("tenant field %q of %s is not a string", path, msg.FullName())
			}
			break
		}
		if fd.Message() == nil {
			return nil, fmt.Errorf("request %s field %q is not a message", msg.FullName(), part)
		}
		msg = fd.Message()
	}
	return names, nil
}

// Of returns the tenant ID a request names, such as "1" for parent
// "locations/1" or name "locations/1/todos/2", and "-" for AIP-159's
// wildcard. It reports false when the field holds no name under the tenant.
func (t *Tenant) Of(req proto.Message) (string, bool) {
	msg := req.ProtoReflect()
	for i, name := range t.names {
		fd := msg.Descriptor().Fields().ByName(name)
		if fd == nil {
			return "", false
		}
		if i < len(t.names)-1 {
			msg = msg.Get(fd).Message()
			continue
		}
		rest, ok := strings.CutPrefix(msg.Get(fd).String(), t.Collection+"/")
		if !ok {
			return "", false
		}
		id, _, _ := strings.Cut(rest, "/")
		return id, id != ""
	}
	return "", false
}
