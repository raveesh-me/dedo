package restdoc

import (
	"fmt"
	"strings"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/proto"

	dedov1 "github.com/raveesh-me/dedo/gen/go/dedo/v1"
)

// defaultMinAudience is the audience a service needs to be documented when
// min_audience is unset. It leaves out only AUDIENCE_COMPONENT_INTERNAL
// services, such as a backend for frontend's: their one caller is built
// with them, so they have no reference to publish.
const defaultMinAudience = dedov1.Audience_AUDIENCE_BUSINESS_UNIT_INTERNAL

// parseAudience reads an Audience by its enum name, with or without the
// AUDIENCE_ prefix: COMPANY_INTERNAL and AUDIENCE_COMPANY_INTERNAL are one
// audience. AUDIENCE_UNSPECIFIED is not an audience a service can have.
func parseAudience(value string) (dedov1.Audience, error) {
	name := value
	if !strings.HasPrefix(name, "AUDIENCE_") {
		name = "AUDIENCE_" + name
	}
	a, ok := dedov1.Audience_value[name]
	if !ok || dedov1.Audience(a) == dedov1.Audience_AUDIENCE_UNSPECIFIED {
		return 0, fmt.Errorf("min_audience: unknown audience %q; want one of %s", value, audienceNames())
	}
	return dedov1.Audience(a), nil
}

// audienceNames lists the audiences min_audience accepts, narrowest first.
func audienceNames() string {
	var names []string
	for a := dedov1.Audience_AUDIENCE_COMPONENT_INTERNAL; a <= dedov1.Audience_AUDIENCE_EXTERNAL_PUBLIC; a++ {
		names = append(names, strings.TrimPrefix(a.String(), "AUDIENCE_"))
	}
	return strings.Join(names, ", ")
}

// documents reports whether the document includes svc: its audience is at
// least the configured minimum. A service that declares no audience is kept:
// lint rejects it (ZALANDO_SERVICE_RULES_REQUIRED), and leaving it out
// would hide that.
func (c Config) documents(svc *protogen.Service) bool {
	rules, _ := proto.GetExtension(svc.Desc.Options(), dedov1.E_Service).(*dedov1.ServiceRules)
	a := rules.GetAudience()
	if a == dedov1.Audience_AUDIENCE_UNSPECIFIED {
		return true
	}
	least := c.MinAudience
	if least == dedov1.Audience_AUDIENCE_UNSPECIFIED {
		least = defaultMinAudience
	}
	return a >= least
}
