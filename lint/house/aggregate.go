package house

import (
	"slices"
	"strings"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	dedov1 "github.com/raveesh-me/dedo/gen/go/dedo/v1"
)

// aggregateVars are a service's variables about the aggregate it manages:
// resources, roots and title.
func aggregateVars(s protoreflect.ServiceDescriptor) map[string]any {
	found := serviceResources(s)
	types := make([]string, 0, len(found))
	for _, r := range found {
		types = append(types, r.typ)
	}
	rules, _ := proto.GetExtension(s.Options(), dedov1.E_Service).(*dedov1.ServiceRules)
	return map[string]any{
		"resources": types,
		"roots":     roots(found),
		"title":     rules.GetTitle(),
	}
}

// found is a resource type a service takes or returns, with its patterns.
type found struct {
	typ      string
	patterns []string
}

// serviceResources lists the resource types of the messages a service's
// methods take or return, and of the messages those hold, at any depth.
// Each type appears once, in the order a walk of the methods, inputs before
// outputs and fields in declaration order, first meets it.
func serviceResources(s protoreflect.ServiceDescriptor) []found {
	w := walker{seen: map[protoreflect.FullName]bool{}, types: map[string]bool{}}
	for i := range s.Methods().Len() {
		m := s.Methods().Get(i)
		w.visit(m.Input())
		w.visit(m.Output())
	}
	return w.out
}

type walker struct {
	seen  map[protoreflect.FullName]bool
	types map[string]bool
	out   []found
}

// visit records m if it is a resource, then the messages its fields hold.
// Resource messages are walked too, so an embedded resource counts.
func (w *walker) visit(m protoreflect.MessageDescriptor) {
	if w.seen[m.FullName()] {
		return
	}
	w.seen[m.FullName()] = true
	if res := resource(m); res != nil && res.GetType() != "" && !w.types[res.GetType()] {
		w.types[res.GetType()] = true
		w.out = append(w.out, found{typ: res.GetType(), patterns: res.GetPattern()})
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

// roots returns the types none of whose patterns is nested under a pattern
// of another type in the list: orders/{order}/lines/{line} is nested under
// orders/{order}, so Line belongs to Order's aggregate.
func roots(all []found) []string {
	out := []string{}
	for _, r := range all {
		if !slices.ContainsFunc(all, func(parent found) bool { return parent.typ != r.typ && nestedUnder(r, parent) }) {
			out = append(out, r.typ)
		}
	}
	return out
}

func nestedUnder(child, parent found) bool {
	for _, c := range child.patterns {
		for _, p := range parent.patterns {
			if strings.HasPrefix(c, p+"/") {
				return true
			}
		}
	}
	return false
}
