// Package rules holds the house lint rules. Every rule ID starts with
// HOUSE_, so it never collides with Buf or AIP rule IDs.
package rules

import "buf.build/go/bufplugin/check"

// Category groups every house rule under one name for buf.yaml.
const Category = "HOUSE"

// All lists every house rule the plugin registers.
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

// Spec is the plugin specification.
var Spec = &check.Spec{
	Rules: All,
	Categories: []*check.CategorySpec{
		{ID: Category, Purpose: "Checks the house rules for public APIs."},
	},
}

func lintRule(id, purpose string, handler check.RuleHandler) *check.RuleSpec {
	return &check.RuleSpec{
		ID:          id,
		CategoryIDs: []string{Category},
		Default:     true,
		Purpose:     purpose,
		Type:        check.RuleTypeLint,
		Handler:     handler,
	}
}
