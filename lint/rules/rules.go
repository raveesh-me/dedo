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
	"context"
	"fmt"
	"os"
	"strings"

	"buf.build/go/bufplugin/check"

	"github.com/raveesh-me/dedo/lint/aip"
	"github.com/raveesh-me/dedo/lint/house/config"
)

// The categories of dedo's own rules, named after their ID prefix.
const (
	CategoryZalando = "ZALANDO"
	CategoryDedo    = "DEDO"
	// CategoryMultiTenant holds the opt-in DEDO_MULTI_TENANT_* rules, for
	// services that partition their data between tenants. Its rules are not
	// in DEDO, so they run only where rules.yml's use lists the set.
	CategoryMultiTenant = "DEDO_MULTI_TENANT"
)

// DefaultUse are the rule sets that run when rules.yml does not say use.
var DefaultUse = []string{aip.CategoryNoLang, CategoryZalando, CategoryDedo}

// Skipped are the rules dedo turns off on purpose, by why: they check
// things dedo has no use for. They run only where rules.yml's use names
// them by ID.
var Skipped = map[string]string{
	"AIP_0131_METHOD_SIGNATURE": methodSignature,
	"AIP_0132_METHOD_SIGNATURE": methodSignature,
	"AIP_0133_METHOD_SIGNATURE": methodSignature,
	"AIP_0134_METHOD_SIGNATURE": methodSignature,
	"AIP_0135_METHOD_SIGNATURE": methodSignature,
	"AIP_0192_HAS_COMMENTS":     "Buf's COMMENTS category reports the same thing.",
}

const methodSignature = "method_signature drives Google's client generators; Connect ignores it."

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

// NewSpec returns the plugin specification: the AIP_*, ZALANDO_*, DEDO_*
// and HOUSE_* rules, consolidated (see Consolidate), run in one pass.
// Every rule is in the one category DEDO, so buf.yaml lists only that, and
// rules.yml chooses which of them run.
func NewSpec() (*check.Spec, error) {
	c, before, err := load()
	if err != nil {
		return nil, err
	}
	return &check.Spec{
		Rules: c.Rules,
		Categories: []*check.CategorySpec{
			{ID: CategoryDedo, Purpose: "Checks every rule of buf-plugin-dedo-lint that rules.yml chooses: AIP, ZALANDO, DEDO and house rules."},
		},
		Before: before,
	}, nil
}

// Load returns the consolidated rules of the repository the plugin runs
// in, for dedo tools to report.
func Load() (*Consolidated, error) {
	c, _, err := load()
	return c, err
}

// load reads dedo's own rules and the rules.yml of the repository buf runs
// in, next to buf.yaml, with what it imports, and consolidates them. Buf
// starts the plugin in the directory it runs in, and asks for the rules
// before any lint request, so they come from there rather than from a
// plugin option.
func load() (*Consolidated, func(context.Context, check.Request) (context.Context, check.Request, error), error) {
	aipRules, _, before, err := aip.Rules()
	if err != nil {
		return nil, nil, err
	}
	wd, err := os.Getwd()
	if err != nil {
		return nil, nil, err
	}
	cfg, err := loadConfig(wd)
	if err != nil {
		return nil, nil, err
	}
	builtin := append(append([]*check.RuleSpec{}, All...), aipRules...)
	c, err := consolidateHouse(cfg, builtin)
	if err != nil {
		return nil, nil, err
	}
	return c, before, nil
}

// embedded holds a rules.yml built into the plugin, as config.Encode wrote
// it, set by dedo tools -wasm with
// -ldflags "-X github.com/raveesh-me/dedo/lint/rules.embedded=<rules>":
// the WebAssembly plugin cannot read rules.yml.
var embedded string

// loadConfig returns the rules.yml the plugin built in, or else the one
// for a lint run in dir; with neither, dedo's default rules run.
func loadConfig(dir string) (config.Config, error) {
	if embedded != "" {
		return config.Decode(embedded)
	}
	path := config.Find(dir)
	if path == "" {
		return config.Config{}, nil
	}
	c, err := config.Load(path)
	if err != nil {
		return config.Config{}, fmt.Errorf("rules.yml: %w", err)
	}
	return c, nil
}

// category returns a rule's category from its ID prefix.
func category(id string) string {
	if strings.HasPrefix(id, CategoryMultiTenant+"_") {
		return CategoryMultiTenant
	}
	prefix, _, _ := strings.Cut(id, "_")
	return prefix
}

// optInRule is a rule that runs only where rules.yml's use lists its set.
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
