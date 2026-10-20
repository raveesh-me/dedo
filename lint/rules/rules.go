// Package rules holds dedo's lint rules, in two classes:
//
//   - ZALANDO_* enforce a specific Zalando RESTful API guideline.
//   - DEDO_* enforce dedo's own conventions.
//
// DEDO_MULTI_TENANT_* rules are an opt-in category of DEDO rules, for
// services that partition their data between tenants.
//
// Together with the AIP_* rules of package aip they make up buf-plugin-dedo-lint
// (see Spec). HOUSE_* rules are each organisation's own, from its rules.yml
// (package house), so they never collide with these.
package rules

import (
	"fmt"
	"os"
	"strings"

	"buf.build/go/bufplugin/check"

	"github.com/raveesh-me/dedo/lint/aip"
	"github.com/raveesh-me/dedo/lint/house"
	"github.com/raveesh-me/dedo/lint/house/config"
)

// The categories of dedo's own rules, named after their ID prefix.
const (
	CategoryZalando = "ZALANDO"
	CategoryDedo    = "DEDO"
	// CategoryMultiTenant holds the opt-in DEDO_MULTI_TENANT_* rules, for
	// services that partition their data between tenants. Its rules are not
	// in DEDO, so they run only where lint.use lists the category.
	CategoryMultiTenant = "DEDO_MULTI_TENANT"
)

// All lists dedo's ZALANDO_* and DEDO_* rules.
var All = []*check.RuleSpec{
	serviceRulesRequired,
	methodRulesRequired,
	scopeNameFormat,
	errorReasonFormat,
	cacheRequiresNoSideEffects,
	deprecationConsistent,
	readMethodNoSideEffects,
	httpResponseBody,
	httpPathVersion,
	noJSONName,
	moneyFieldType,
	toolRequired,
	toolAnnotations,
	messageDescriptionRequired,
	multiTenantServiceTenancy,
	multiTenantResourcePattern,
	multiTenantRequestTenant,
	multiTenantCachePrivate,
}

// NewSpec returns the plugin specification: the AIP_*, ZALANDO_* and DEDO_*
// rules, run in one pass.
func NewSpec() (*check.Spec, error) {
	aipRules, aipCategories, before, err := aip.Rules()
	if err != nil {
		return nil, err
	}
	houseRules, err := loadHouseRules()
	if err != nil {
		return nil, err
	}
	categories := aipCategories
	if len(houseRules) > 0 {
		// A category needs rules, so HOUSE exists only with a rules.yml.
		categories = append(categories, house.CategorySpec)
	}
	return &check.Spec{
		Rules: append(append(append([]*check.RuleSpec{}, All...), aipRules...), houseRules...),
		Categories: append([]*check.CategorySpec{
			{ID: CategoryZalando, Purpose: "Checks rules from Zalando's RESTful API guidelines that dedo adopts."},
			{ID: CategoryDedo, Purpose: "Checks dedo's own conventions for public APIs, REST and MCP."},
			{ID: CategoryMultiTenant, Purpose: "Checks that a multi-tenant API names the tenant in every resource and request, so the runtime can enforce it."},
		}, categories...),
		Before: before,
	}, nil
}

// loadHouseRules reads the HOUSE_* rules of the repository buf runs in:
// rules.yml next to buf.yaml and what it imports. Buf starts the plugin in
// the directory it runs in, and asks for the rules before any lint request,
// so they come from there rather than from a plugin option.
func loadHouseRules() ([]*check.RuleSpec, error) {
	wd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	path := config.Find(wd)
	if path == "" {
		return nil, nil
	}
	rules, err := config.Load(path)
	if err != nil {
		return nil, fmt.Errorf("house rules: %w", err)
	}
	return house.Specs(rules)
}

// category returns a rule's category from its ID prefix.
func category(id string) string {
	if strings.HasPrefix(id, CategoryMultiTenant+"_") {
		return CategoryMultiTenant
	}
	prefix, _, _ := strings.Cut(id, "_")
	return prefix
}

// optInRule is a rule that runs only where lint.use lists its category.
func optInRule(id, purpose string, handler check.RuleHandler) *check.RuleSpec {
	spec := lintRule(id, purpose, handler)
	spec.Default = false
	return spec
}

func lintRule(id, purpose string, handler check.RuleHandler) *check.RuleSpec {
	return &check.RuleSpec{
		ID:          id,
		CategoryIDs: []string{category(id)},
		Default:     true,
		Purpose:     purpose,
		Type:        check.RuleTypeLint,
		Handler:     handler,
	}
}
