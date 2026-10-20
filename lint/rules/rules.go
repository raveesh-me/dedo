// Package rules holds dedo's lint rules, in two classes:
//
//   - ZALANDO_* enforce a specific Zalando RESTful API guideline.
//   - DEDO_* enforce dedo's own conventions.
//
// Together with the AIP_* rules of package aip they make up buf-plugin-dedo-lint
// (see Spec). HOUSE_* is left to each organisation's own plugin, so it never
// collides with these.
package rules

import (
	"strings"

	"buf.build/go/bufplugin/check"

	"github.com/raveesh-me/dedo/lint/aip"
)

// The categories of dedo's own rules, named after their ID prefix.
const (
	CategoryZalando = "ZALANDO"
	CategoryDedo    = "DEDO"
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
}

// NewSpec returns the plugin specification: the AIP_*, ZALANDO_* and DEDO_*
// rules, run in one pass.
func NewSpec() (*check.Spec, error) {
	aipRules, aipCategories, before, err := aip.Rules()
	if err != nil {
		return nil, err
	}
	return &check.Spec{
		Rules: append(append([]*check.RuleSpec{}, All...), aipRules...),
		Categories: append([]*check.CategorySpec{
			{ID: CategoryZalando, Purpose: "Checks rules from Zalando's RESTful API guidelines that dedo adopts."},
			{ID: CategoryDedo, Purpose: "Checks dedo's own conventions for public APIs, REST and MCP."},
		}, aipCategories...),
		Before: before,
	}, nil
}

// category returns a rule's category from its ID prefix.
func category(id string) string {
	prefix, _, _ := strings.Cut(id, "_")
	return prefix
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
