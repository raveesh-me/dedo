package policy

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"google.golang.org/genproto/googleapis/api/annotations"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// Tenant is how the runtime finds the tenants of a request to a method of a
// multi-tenant service.
type Tenant struct {
	// Collection is the tenant's collection ID, such as "locations".
	Collection string
	// Field is the path of the request field that holds a resource name
	// under the tenant, such as "parent" or "todo.name". Every request must
	// set it.
	Field string
	names []protoreflect.Name
	// refs are the string fields, anywhere in the request, that reference a
	// resource (google.api.resource_reference); reach are the messages that
	// lead to one. A name under the tenant in any of them is checked too, so
	// a request cannot act in one tenant while naming another, such as a
	// move to destination locations/2.
	refs  map[protoreflect.FullName]bool
	reach map[protoreflect.FullName]bool
}

// pathVariable matches a variable of an HTTP path template, such as
// {parent=locations/*} or {todo.name=locations/*/todos/*}.
var pathVariable = regexp.MustCompile(`\{([A-Za-z0-9_.]+)(?:=([^}]*))?\}`)

// TenantField returns the request field that an HTTP path binds to a name
// under the tenant collection: "parent" for
// /commerce/todo/v1/{parent=locations/*}/todos with collection "locations".
func TenantField(path, collection string) (string, bool) {
	fields := tenantFields(path, collection)
	if len(fields) == 0 {
		return "", false
	}
	return fields[0], true
}

func tenantFields(path, collection string) []string {
	var fields []string
	for _, m := range pathVariable.FindAllStringSubmatch(path, -1) {
		segments := strings.Split(m[2], "/")
		if len(segments) >= 2 && segments[0] == collection && segments[1] == "*" {
			fields = append(fields, m[1])
		}
	}
	return fields
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
// from its HTTP rule. The runtime reads that one field on every transport,
// so every binding must bind the same one: a request through a binding that
// set another field would be checked on a field it left empty.
// buf-plugin-dedo-lint calls it too, so a contract that lints clean also
// starts.
func MethodTenant(m protoreflect.MethodDescriptor, rule *annotations.HttpRule, collection string) (*Tenant, error) {
	paths := HTTPPaths(rule)
	if len(paths) == 0 {
		return nil, fmt.Errorf("it has no HTTP binding to name its tenant in, such as {parent=%s/*}", collection)
	}
	var field string
	for _, path := range paths {
		fields := tenantFields(path, collection)
		switch {
		case len(fields) == 0:
			return nil, fmt.Errorf("HTTP path %q binds no field to %s/*", path, collection)
		case len(fields) > 1:
			// Only the tenant field must be set and well formed; a second
			// tenant name in the path is checked only if it also carries a
			// resource_reference, and would otherwise go unchecked.
			return nil, fmt.Errorf("HTTP path %q binds more than one field to %s/*", path, collection)
		case field != "" && fields[0] != field:
			return nil, fmt.Errorf("HTTP bindings name the tenant in %q and %q", field, fields[0])
		}
		field = fields[0]
	}
	names, err := stringFieldPath(m.Input(), field)
	if err != nil {
		return nil, err
	}
	t := &Tenant{
		Collection: collection,
		Field:      field,
		names:      names,
		refs:       map[protoreflect.FullName]bool{},
		reach:      map[protoreflect.FullName]bool{},
	}
	t.findReferences(m.Input())
	return t, nil
}

func methodTenant(m protoreflect.MethodDescriptor, collection string) (*Tenant, error) {
	rule, _ := proto.GetExtension(m.Options(), annotations.E_Http).(*annotations.HttpRule)
	tenant, err := MethodTenant(m, rule, collection)
	if err != nil {
		return nil, fmt.Errorf("policy: method %s of a multi-tenant service: %w", m.FullName(), err)
	}
	return tenant, nil
}

// findReferences records the resource reference fields of a request and of
// every message it can hold, and which of those messages lead to one.
//
// Reach is settled over the whole type graph, not in one recursive pass: in
// mutually recursive messages, such as a Node holding a Wrapper holding a
// Node, a single pass reaches Wrapper before Node is known to hold a
// reference, marks Wrapper as reaching none, and the runtime would then
// never walk the Node inside it.
func (t *Tenant) findReferences(request protoreflect.MessageDescriptor) {
	children := map[protoreflect.FullName][]protoreflect.FullName{}
	var visit func(protoreflect.MessageDescriptor)
	visit = func(md protoreflect.MessageDescriptor) {
		if _, seen := children[md.FullName()]; seen {
			return
		}
		children[md.FullName()] = []protoreflect.FullName{}
		fields := md.Fields()
		for i := 0; i < fields.Len(); i++ {
			fd := fields.Get(i)
			child := fd.Message()
			if fd.IsMap() {
				child = fd.MapValue().Message()
			}
			switch {
			case fd.Kind() == protoreflect.StringKind && isReference(fd):
				t.refs[fd.FullName()] = true
				t.reach[md.FullName()] = true
			case child != nil:
				children[md.FullName()] = append(children[md.FullName()], child.FullName())
				visit(child)
			}
		}
	}
	visit(request)
	for changed := true; changed; {
		changed = false
		for parent, kids := range children {
			if t.reach[parent] {
				continue
			}
			for _, kid := range kids {
				if t.reach[kid] {
					t.reach[parent], changed = true, true
					break
				}
			}
		}
	}
}

func isReference(fd protoreflect.FieldDescriptor) bool {
	ref, _ := proto.GetExtension(fd.Options(), annotations.E_ResourceReference).(*annotations.ResourceReference)
	return ref.GetType() != "" || ref.GetChildType() != ""
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

// Of returns the tenant ID in the request's tenant field, such as "1" for
// parent "locations/1" or name "locations/1/todos/2", and "-" for AIP-159's
// wildcard. It reports false when the field holds no well-formed name under
// the tenant.
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
		return t.parse(msg.Get(fd).String())
	}
	return "", false
}

// Tenants returns every tenant a request names: the one in its tenant
// field, which must be well formed, then those of any resource reference
// field that holds a name under the tenant collection. It reports false
// when the tenant field, or a reference under the collection, is malformed.
func (t *Tenant) Tenants(req proto.Message) ([]string, bool) {
	id, ok := t.Of(req)
	if !ok {
		return nil, false
	}
	ids := []string{id}
	if !t.collect(req.ProtoReflect(), &ids) {
		return nil, false
	}
	return ids, true
}

func (t *Tenant) collect(msg protoreflect.Message, ids *[]string) bool {
	ok := true
	msg.Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		switch {
		case t.refs[fd.FullName()] && fd.IsList():
			list := v.List()
			for i := 0; i < list.Len() && ok; i++ {
				ok = t.add(list.Get(i).String(), ids)
			}
		case t.refs[fd.FullName()]:
			ok = t.add(v.String(), ids)
		case fd.IsMap() && fd.MapValue().Message() != nil && t.reach[fd.MapValue().Message().FullName()]:
			v.Map().Range(func(_ protoreflect.MapKey, mv protoreflect.Value) bool {
				ok = t.collect(mv.Message(), ids)
				return ok
			})
		case fd.Message() != nil && !fd.IsMap() && t.reach[fd.Message().FullName()] && fd.IsList():
			list := v.List()
			for i := 0; i < list.Len() && ok; i++ {
				ok = t.collect(list.Get(i).Message(), ids)
			}
		case fd.Message() != nil && !fd.IsMap() && t.reach[fd.Message().FullName()]:
			ok = t.collect(v.Message(), ids)
		}
		return ok
	})
	return ok
}

// add records the tenant of a reference that names one. A reference to a
// resource outside the tenant collection, such as a global catalogue entry,
// names no tenant.
func (t *Tenant) add(name string, ids *[]string) bool {
	if !strings.HasPrefix(name, t.Collection+"/") {
		return true
	}
	id, ok := t.parse(name)
	if ok && !slices.Contains(*ids, id) {
		*ids = append(*ids, id)
	}
	return ok
}

// parse returns the tenant ID of a resource name under the collection. The
// whole name must be well formed: no empty, "." or ".." segment and no
// space or control character, so a handler that cleans or re-parses the
// name cannot land in another tenant than the one checked.
func (t *Tenant) parse(name string) (string, bool) {
	rest, ok := strings.CutPrefix(name, t.Collection+"/")
	if !ok {
		return "", false
	}
	if strings.IndexFunc(name, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
		return "", false
	}
	segments := strings.Split(rest, "/")
	for _, s := range segments {
		if s == "" || s == "." || s == ".." {
			return "", false
		}
	}
	return segments[0], true
}
