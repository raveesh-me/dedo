package main

import (
	"slices"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/raveesh-me/dedo/lint/house/config"
)

// skippedRules are the rules dedo skips on purpose, which releases before
// v0.15.0 listed in buf.yaml's lint.except; the plugin now skips them
// itself (lint/rules.Skipped).
var skippedRules = []string{
	"AIP_0131_METHOD_SIGNATURE",
	"AIP_0132_METHOD_SIGNATURE",
	"AIP_0133_METHOD_SIGNATURE",
	"AIP_0134_METHOD_SIGNATURE",
	"AIP_0135_METHOD_SIGNATURE",
	"AIP_0192_HAS_COMMENTS",
}

// pluginRule reports whether a lint.use or lint.except entry names
// buf-plugin-dedo-lint's rules rather than Buf's own.
func pluginRule(id string) bool {
	for _, p := range []string{"AIP", "ZALANDO", "DEDO", house} {
		if id == p || strings.HasPrefix(id, p+"_") {
			return true
		}
	}
	return false
}

// selection is what buf.yaml chose of the plugin's rules before rules.yml
// did: the sets and rules lint.use named, and the rules lint.except named.
type selection struct {
	use, disable []string
}

// moveSelection takes the plugin's rules out of buf.yaml's lint section,
// leaving DEDO in use, and returns what they selected, for rules.yml.
func (in *initer) moveSelection(lint *yaml.Node) (selection, bool) {
	var sel selection
	changed := false
	if use := lookup(lint, "use"); use != nil {
		for _, id := range scalars(use) {
			if pluginRule(id) && id != "DEDO" {
				dropScalars(use, id)
				changed = true
				if id != house {
					sel.use = append(sel.use, id)
				}
			}
		}
	}
	if added := addScalars(child(lint, "use", yaml.SequenceNode), "DEDO"); len(added) > 0 {
		changed = true
	}
	if len(sel.use) > 0 {
		// DEDO stayed in buf.yaml, where it now means every rule.
		sel.use = append(sel.use, "DEDO")
	}
	if except := lookup(lint, "except"); except != nil {
		for _, id := range scalars(except) {
			if !pluginRule(id) {
				continue
			}
			dropScalars(except, id)
			changed = true
			if !slices.Contains(skippedRules, id) && !slices.Contains(overriddenRules, id) {
				sel.disable = append(sel.disable, id)
			}
		}
		if len(except.Content) == 0 {
			remove(lint, "except")
		}
	}
	if changed {
		in.say("buf.yaml: lint.use lists DEDO of buf-plugin-dedo-lint's rules, and rules.yml chooses which run")
	}
	return sel, changed
}

// ruleSet writes the rule selection to rules.yml, creating it from the
// template: buf.yaml's former choice, and DEDO_MULTI_TENANT for a service
// that declares tenancy.
func (in *initer) ruleSet(sel selection) error {
	path := in.path(config.FileName)
	doc, ok, err := loadYAML(path)
	if err != nil {
		return err
	}
	changed := false
	if !ok || len(doc.Content) == 0 || root(doc).Kind != yaml.MappingNode {
		if doc, err = parseYAML(asset("templates/" + config.FileName)); err != nil {
			return err
		}
		in.say("rules.yml: created, with dedo's default rule sets")
		changed = true
	}
	use := lookup(root(doc), "use")
	if use == nil {
		use = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		for _, id := range []string{"AIP_NOLANG", "ZALANDO", "DEDO"} {
			use.Content = append(use.Content, scalar(id))
		}
		// First, so the file opens with what runs.
		root(doc).Content = append([]*yaml.Node{scalar("use"), use}, root(doc).Content...)
		in.say("rules.yml: added use, with dedo's default rule sets")
		changed = true
	}
	if len(sel.use) > 0 && changedUse(use, sel.use) {
		in.say("rules.yml: use is now %s, as buf.yaml's lint.use had it", strings.Join(sel.use, ", "))
		changed = true
	}
	if in.p.tenancy != "" {
		if added := addScalars(use, multiTenant); len(added) > 0 {
			in.say("rules.yml: use gained %s, since the services declare tenancy", multiTenant)
			changed = true
		}
	}
	if len(sel.disable) > 0 {
		if added := addScalars(child(root(doc), "disable", yaml.SequenceNode), sel.disable...); len(added) > 0 {
			in.say("rules.yml: disable gained %s, from buf.yaml's lint.except", strings.Join(added, ", "))
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return saveYAML(path, doc)
}

// changedUse sets use to want when it holds only dedo's defaults, which
// init wrote, and reports whether it did: a use someone edited stays.
func changedUse(use *yaml.Node, want []string) bool {
	have := scalars(use)
	if slices.Equal(have, want) || !slices.Equal(have, []string{"AIP_NOLANG", "ZALANDO", "DEDO"}) {
		return false
	}
	use.Content = nil
	addScalars(use, want...)
	return true
}
