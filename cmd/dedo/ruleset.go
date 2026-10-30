package main

import (
	"fmt"
	"os"
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

// defaultUse are the rule sets the lint plugin runs when dedo.yaml does not
// say use (lint/rules.DefaultUse), which a new dedo.yaml lists.
var defaultUse = []string{"AIP_NOLANG", "ZALANDO", "DEDO", house}

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

// selection is what buf.yaml chose of the plugin's rules before dedo.yaml
// did: the sets and rules lint.use named, and the rules lint.except named.
type selection struct {
	use, disable []string
}

// moveSelection takes the plugin's rules out of buf.yaml's lint section,
// leaving DEDO in use, and returns what they selected, for dedo.yaml.
func (in *initer) moveSelection(lint *yaml.Node) (selection, bool) {
	var sel selection
	changed := false
	if use := lookup(lint, "use"); use != nil {
		for _, id := range scalars(use) {
			if pluginRule(id) && id != "DEDO" {
				dropScalars(use, id)
				changed = true
				sel.use = append(sel.use, id)
			}
		}
	}
	if added := addScalars(child(lint, "use", yaml.SequenceNode), "DEDO"); len(added) > 0 {
		changed = true
	}
	if len(sel.use) > 0 {
		// DEDO stayed in buf.yaml, where it now means every rule; HOUSE
		// joins, so house rules added later run.
		sel.use = append(sel.use, "DEDO")
		if !slices.Contains(sel.use, house) {
			sel.use = append(sel.use, house)
		}
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
		in.say("buf.yaml: lint.use lists DEDO of buf-plugin-dedo-lint's rules, and dedo.yaml chooses which run")
	}
	return sel, changed
}

// ruleSet writes the rule selection to dedo.yaml's rules section: buf.yaml's
// former choice, and DEDO_MULTI_TENANT for a service that declares tenancy.
// It creates dedo.yaml from the template, moving the rules of a rules.yml,
// which releases before v0.20.0 wrote, into it.
func (in *initer) ruleSet(sel selection) error {
	path := in.path(config.FileName)
	doc, ok, err := loadYAML(path)
	if err != nil {
		return err
	}
	changed, moved := false, false
	if !ok || len(doc.Content) == 0 || root(doc).Kind != yaml.MappingNode {
		if doc, moved, err = in.newDedoYAML(); err != nil {
			return err
		}
		changed = true
	}
	rules := child(root(doc), "rules", yaml.MappingNode)
	use := lookup(rules, "use")
	if use == nil {
		use = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		for _, id := range defaultUse {
			use.Content = append(use.Content, scalar(id))
		}
		// First, so the section opens with what runs.
		rules.Content = append([]*yaml.Node{scalar("use"), use}, rules.Content...)
		in.say("dedo.yaml: added use, with dedo's default rule sets")
		changed = true
	}
	if len(sel.use) > 0 && changedUse(use, sel.use) {
		in.say("dedo.yaml: use is now %s, as buf.yaml's lint.use had it", strings.Join(sel.use, ", "))
		changed = true
	}
	if in.p.tenancy != "" {
		if added := addScalars(use, multiTenant); len(added) > 0 {
			in.say("dedo.yaml: use gained %s, since the services declare tenancy", multiTenant)
			changed = true
		}
	}
	if len(sel.disable) > 0 {
		if added := addScalars(child(rules, "disable", yaml.SequenceNode), sel.disable...); len(added) > 0 {
			in.say("dedo.yaml: disable gained %s, from buf.yaml's lint.except", strings.Join(added, ", "))
			changed = true
		}
	}
	if !changed {
		return nil
	}
	if err := saveYAML(path, doc); err != nil {
		return err
	}
	if moved {
		return os.Remove(in.path(config.RulesFileName))
	}
	return nil
}

// newDedoYAML returns a dedo.yaml from the template, with the rules of the
// repository's rules.yml in its rules section when it has one, and reports
// whether it had.
func (in *initer) newDedoYAML() (*yaml.Node, bool, error) {
	doc, err := parseYAML(asset("templates/" + config.FileName))
	if err != nil {
		return nil, false, err
	}
	old, ok, err := loadYAML(in.path(config.RulesFileName))
	if err != nil {
		return nil, false, fmt.Errorf("%s: %w", config.RulesFileName, err)
	}
	if !ok {
		in.say("dedo.yaml: created, with dedo's default rule sets")
		return doc, false, nil
	}
	set(root(doc), "rules", root(old))
	in.say("dedo.yaml: created, with the rules of rules.yml, which it replaces")
	return doc, true, nil
}

// changedUse sets use to want when it holds only dedo's defaults, which
// init wrote, and reports whether it did: a use someone edited stays.
func changedUse(use *yaml.Node, want []string) bool {
	have := scalars(use)
	if slices.Equal(have, want) || !slices.Equal(have, defaultUse) {
		return false
	}
	use.Content = nil
	addScalars(use, want...)
	return true
}
