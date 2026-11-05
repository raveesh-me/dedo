// Package aggregate finds the aggregate a service manages, in the sense of
// guidance/domains.md: the google.api.resource types its methods take or
// return, and the roots among them. The lint plugin's house rules and the
// REST doc generator share it.
package aggregate

import (
	"slices"
	"strings"

	"google.golang.org/genproto/googleapis/api/annotations"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// Resource is a resource type a service takes or returns.
type Resource struct {
	Type     string   // such as shop.example.com/Order
	Patterns []string // such as orders/{order}
	Plural   string   // such as orders; empty when undeclared
}

// Resources lists the resource types of the messages a service's methods
// take or return, and of the messages those hold, at any depth. Each type
// appears once, in the order a walk of the methods, inputs before outputs
// and fields in declaration order, first meets it.
func Resources(s protoreflect.ServiceDescriptor) []Resource {
	w := walker{seen: map[protoreflect.FullName]bool{}, types: map[string]bool{}, out: []Resource{}}
	for i := range s.Methods().Len() {
		m := s.Methods().Get(i)
		w.visit(m.Input())
		w.visit(m.Output())
	}
	return w.out
}

// Roots returns the resources none of whose patterns is nested under a
// pattern of another resource in all: orders/{order}/lines/{line} is nested
// under orders/{order}, so a line belongs to its order's aggregate.
func Roots(all []Resource) []Resource {
	out := []Resource{}
	for _, r := range all {
		if !slices.ContainsFunc(all, func(parent Resource) bool { return parent.Type != r.Type && nestedUnder(r, parent) }) {
			out = append(out, r)
		}
	}
	return out
}

type walker struct {
	seen  map[protoreflect.FullName]bool
	types map[string]bool
	out   []Resource
}

// visit records m if it is a resource, then the messages its fields hold.
// Resource messages are walked too, so an embedded resource counts.
func (w *walker) visit(m protoreflect.MessageDescriptor) {
	if w.seen[m.FullName()] {
		return
	}
	w.seen[m.FullName()] = true
	res, _ := proto.GetExtension(m.Options(), annotations.E_Resource).(*annotations.ResourceDescriptor)
	if t := res.GetType(); t != "" && !w.types[t] {
		w.types[t] = true
		w.out = append(w.out, Resource{Type: t, Patterns: res.GetPattern(), Plural: res.GetPlural()})
	}
	for i := range m.Fields().Len() {
		f := m.Fields().Get(i)
		if f.IsMap() {
			f = f.MapValue()
		}
		if f.Message() != nil {
			w.visit(f.Message())
		}
	}
}

func nestedUnder(child, parent Resource) bool {
	for _, c := range child.Patterns {
		for _, p := range parent.Patterns {
			if strings.HasPrefix(c, p+"/") {
				return true
			}
		}
	}
	return false
}
