// Package rules holds the HOUSE_* lint rules of an example organisation.
package rules

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"buf.build/go/bufplugin/check"
	"buf.build/go/bufplugin/check/checkutil"
	"buf.build/go/bufplugin/option"
	"google.golang.org/genproto/googleapis/api/annotations"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// Category holds every house rule.
const Category = "HOUSE"

// IDResourceTypeDomain is the ID of the rule that keeps resource types under
// the organisation's domain.
const IDResourceTypeDomain = "HOUSE_RESOURCE_TYPE_DOMAIN"

// OptionResourceDomain is the plugin option naming the organisation's domain.
const OptionResourceDomain = "resource_domain"

// Spec is the plugin specification.
var Spec = &check.Spec{
	Rules: []*check.RuleSpec{
		{
			ID:          IDResourceTypeDomain,
			CategoryIDs: []string{Category},
			Default:     true,
			Purpose:     "Checks that resource types live under the domain set by the resource_domain option.",
			Type:        check.RuleTypeLint,
			Handler:     checkutil.NewMessageRuleHandler(checkResourceTypeDomain, checkutil.WithoutImports()),
		},
	},
	Categories: []*check.CategorySpec{
		{ID: Category, Purpose: "Checks the organisation's own API rules."},
	},
}

// checkResourceTypeDomain reports a resource type such as
// "todo.example.com/Todo" whose service name is neither the configured
// domain nor a subdomain of it.
func checkResourceTypeDomain(_ context.Context, w check.ResponseWriter, req check.Request, m protoreflect.MessageDescriptor) error {
	domain, err := option.GetStringValue(req.Options(), OptionResourceDomain)
	if err != nil {
		return err
	}
	if domain == "" {
		return errors.New("buf-plugin-house: set the resource_domain plugin option in buf.yaml")
	}
	resource, ok := proto.GetExtension(m.Options(), annotations.E_Resource).(*annotations.ResourceDescriptor)
	if !ok || resource == nil {
		return nil
	}
	service, _, _ := strings.Cut(resource.GetType(), "/")
	if service != domain && !strings.HasSuffix(service, "."+domain) {
		w.AddAnnotation(check.WithDescriptor(m), check.WithMessage(fmt.Sprintf(
			"Resource type %q must live under %s, for example \"<app>.%s/%s\".",
			resource.GetType(), domain, domain, m.Name())))
	}
	return nil
}
