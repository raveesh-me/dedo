package main

import (
	"fmt"
	"strings"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/proto"

	dedov1 "github.com/raveesh-me/dedo/gen/go/dedo/v1"
)

// defaultMinAudience is the audience a service needs for its RPCs to become
// tools when min_audience is unset. It leaves out only
// AUDIENCE_COMPONENT_INTERNAL services, such as a backend for frontend's:
// their one caller is built with them, and an agent is not that caller.
const defaultMinAudience = dedov1.Audience_AUDIENCE_BUSINESS_UNIT_INTERNAL

// Config holds the plugin parameters.
type Config struct {
	// MinAudience is the narrowest audience whose services become tools.
	// Unset, only AUDIENCE_COMPONENT_INTERNAL services are left out.
	MinAudience dedov1.Audience
}

// Set applies one plugin parameter, as protogen.Options.ParamFunc. The
// parameter is protoc-gen-dedo-openapi's, so both plugins can share it.
func (c *Config) Set(name, value string) error {
	switch name {
	case "min_audience":
		a, err := parseAudience(value)
		if err != nil {
			return err
		}
		c.MinAudience = a
	default:
		return fmt.Errorf("unknown parameter %q", name)
	}
	return nil
}

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

// serves reports whether svc's RPCs become tools: its audience is at least
// the configured minimum. A service that declares no audience is kept: lint
// rejects it (ZALANDO_SERVICE_RULES_REQUIRED), and leaving it out would
// hide that.
func (c Config) serves(svc *protogen.Service) bool {
	a := audience(svc)
	if a == dedov1.Audience_AUDIENCE_UNSPECIFIED {
		return true
	}
	least := c.MinAudience
	if least == dedov1.Audience_AUDIENCE_UNSPECIFIED {
		least = defaultMinAudience
	}
	return a >= least
}

// audience is the audience svc declares in dedo.v1.service.
func audience(svc *protogen.Service) dedov1.Audience {
	rules, _ := proto.GetExtension(svc.Desc.Options(), dedov1.E_Service).(*dedov1.ServiceRules)
	return rules.GetAudience()
}
