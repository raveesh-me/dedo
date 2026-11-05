package house

import (
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/raveesh-me/dedo/aggregate"
	dedov1 "github.com/raveesh-me/dedo/gen/go/dedo/v1"
)

// aggregateVars are a service's variables about the aggregate it manages:
// resources, roots and title.
func aggregateVars(s protoreflect.ServiceDescriptor) map[string]any {
	all := aggregate.Resources(s)
	rules, _ := proto.GetExtension(s.Options(), dedov1.E_Service).(*dedov1.ServiceRules)
	return map[string]any{
		"resources": types(all),
		"roots":     types(aggregate.Roots(all)),
		"title":     rules.GetTitle(),
	}
}

func types(rs []aggregate.Resource) []string {
	out := make([]string, 0, len(rs))
	for _, r := range rs {
		out = append(out, r.Type)
	}
	return out
}
