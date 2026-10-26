package rules

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	"buf.build/go/bufplugin/check"

	"github.com/raveesh-me/dedo/lint/aip"
	"github.com/raveesh-me/dedo/lint/house"
	"github.com/raveesh-me/dedo/lint/house/config"
)

// Rules come in layers, and a rule may override only rules of a lower one:
//
//	ZALANDO < AIP < DEDO < HOUSE
//
// Among house rules, a rules file outranks the files it imports, so a
// repository's own rules.yml outranks the shared rules it imports. No
// conflict is detected for you: a rule names the rules it overrides, and
// Consolidate checks each against this order.
const (
	layerZalando = iota
	layerAIP
	layerDedo
	layerHouse
)

var layerNames = []string{"ZALANDO", "AIP", "DEDO", "HOUSE"}

// layer returns the layer of one of dedo's own rules from its ID prefix.
func layer(id string) int {
	switch category(id) {
	case CategoryZalando:
		return layerZalando
	case aip.CategoryAll:
		return layerAIP
	case CategoryDedo, CategoryMultiTenant:
		return layerDedo
	}
	panic("rules: " + id + " is in no layer") // TestSpec loads every rule
}

// builtinOverrides are the AIP and ZALANDO rules that dedo's own rules
// override, by the rule that overrides them.
var builtinOverrides = map[string][]string{
	// Every RPC returns its own <Rpc>Response, which REST unwraps with
	// response_body. The wrapping also hides the resource from AIP's
	// standard-method detection, which then reports false positives.
	httpResponseBody.ID: {
		"AIP_0131_RESPONSE_MESSAGE_NAME",
		"AIP_0132_RESPONSE_MESSAGE_NAME",
		"AIP_0133_RESPONSE_MESSAGE_NAME",
		"AIP_0134_RESPONSE_MESSAGE_NAME",
		"AIP_0135_RESPONSE_MESSAGE_NAME",
		"AIP_0121_RESOURCE_MUST_SUPPORT_GET",
		"AIP_0134_REQUEST_REQUIRED_FIELDS",
	},
}

// Final are dedo's own final rules, by why they are final: they check what
// the runtime itself requires, and overriding one would only move the
// failure from lint to the service's startup, so no rule may. A house rule
// is final when it says final: true.
var Final = map[string]string{
	serviceRulesRequired.ID:     "policy.Build refuses a service without the dedo.v1.service option",
	methodRulesRequired.ID:      "policy.Build refuses a method without scopes",
	multiTenantRequestTenant.ID: "policy.Build refuses a multi-tenant method whose tenant field it cannot find",
}

// Override is one rule overriding another.
type Override struct {
	By, Rule string
	// Source is the rules file of By, or "" for dedo's own rules.
	Source string
	// Lapsed is set when By is itself overridden: it does not run, so
	// neither do its overrides, and Rule runs again.
	Lapsed bool
}

// Consolidated is the rule set a lint runs: every rule, with each
// overridden one reporting nothing.
type Consolidated struct {
	Rules     []*check.RuleSpec
	Overrides []Override
	// House is the number of house rules.
	House int
	// Overridden is the number of rules that report nothing.
	Overridden int
	// Final lists the final rules, with the rules file of a house one.
	Final []Override
}

// candidate is a rule as Consolidate ranks it.
type candidate struct {
	spec      *check.RuleSpec
	overrides []string
	house     *config.Rule // nil for dedo's own rules
}

func (c candidate) source() string {
	if c.house == nil {
		return ""
	}
	return c.house.Source
}

func (c candidate) layer() int {
	if c.house != nil {
		return layerHouse
	}
	return layer(c.spec.ID)
}

// outranks reports whether a may override b.
func outranks(a, b candidate) bool {
	if a.house == nil || b.house == nil {
		return a.layer() > b.layer()
	}
	return slices.Contains(b.house.Importers, a.house.File)
}

// Consolidate checks every override against the order of layers, the
// final rules and the rules that exist, and returns the rules to run. It
// reports every mistake at once. disabled are the house rules rules.yml
// disables: an override of one has nothing left to override.
func Consolidate(builtin []*check.RuleSpec, houseRules []config.Rule, houseSpecs []*check.RuleSpec, disabled []string) (*Consolidated, error) {
	var all []candidate
	byID := map[string]candidate{}
	for _, s := range builtin {
		c := candidate{spec: s, overrides: builtinOverrides[s.ID]}
		all = append(all, c)
		byID[s.ID] = c
	}
	for i, s := range houseSpecs {
		c := candidate{spec: s, overrides: houseRules[i].Overrides, house: &houseRules[i]}
		all = append(all, c)
		byID[s.ID] = c
	}
	if err := checkOverrides(all, byID, disabled); err != nil {
		return nil, err
	}
	// A rule runs unless a rule that runs overrides it. Overriders rank
	// strictly higher, so this ends.
	overriders := map[string][]candidate{}
	for _, c := range all {
		for _, id := range c.overrides {
			overriders[id] = append(overriders[id], c)
		}
	}
	runs := map[string]bool{}
	var running func(id string) bool
	running = func(id string) bool {
		if r, ok := runs[id]; ok {
			return r
		}
		r := true
		for _, o := range overriders[id] {
			r = r && !running(o.spec.ID)
		}
		runs[id] = r
		return r
	}
	out := &Consolidated{House: len(houseSpecs)}
	for _, c := range all {
		spec := c.spec
		var by []string
		for _, o := range overriders[spec.ID] {
			if running(o.spec.ID) {
				by = append(by, o.spec.ID)
			}
		}
		if len(by) > 0 {
			spec = overridden(spec, by)
			out.Overridden++
		}
		out.Rules = append(out.Rules, spec)
		for _, id := range c.overrides {
			out.Overrides = append(out.Overrides, Override{By: c.spec.ID, Rule: id, Source: c.source(), Lapsed: !running(c.spec.ID)})
		}
	}
	sort.SliceStable(out.Overrides, func(i, j int) bool { return out.Overrides[i].By < out.Overrides[j].By })
	for _, c := range all {
		if Final[c.spec.ID] != "" || c.house != nil && c.house.Final {
			out.Final = append(out.Final, Override{Rule: c.spec.ID, Source: c.source()})
		}
	}
	return out, nil
}

func checkOverrides(all []candidate, byID map[string]candidate, disabled []string) error {
	var errs []error
	for _, c := range all {
		at := c.spec.ID
		if src := c.source(); src != "" {
			at = src + ": " + at
		}
		for _, id := range c.overrides {
			target, ok := byID[id]
			switch {
			case !ok && slices.Contains(disabled, id):
				// Disabled already; the override changes nothing.
			case !ok:
				errs = append(errs, fmt.Errorf("%s overrides %s, which no rule defines", at, id))
			case Final[id] != "":
				errs = append(errs, fmt.Errorf("%s overrides %s, which is final: %s, so a contract without it would fail at startup instead of in lint. Keep %s and add your rule beside it", at, id, Final[id], id))
			case target.house != nil && target.house.Final:
				errs = append(errs, fmt.Errorf("%s overrides %s, which %s marks final. Keep %s and add your rule beside it, or ask its owners to drop final", at, id, target.house.Source, id))
			case !outranks(c, target):
				errs = append(errs, fmt.Errorf("%s cannot override %s%s: %s", at, id, from(target), rankRule(c)))
			}
		}
	}
	return errors.Join(errs...)
}

func from(c candidate) string {
	if c.house == nil {
		return ""
	}
	return " (" + c.house.Source + ")"
}

// rankRule explains what a rule may override.
func rankRule(c candidate) string {
	l := c.layer()
	if l == layerHouse {
		return "a house rule overrides only DEDO, AIP and ZALANDO rules and the rules of the files its rules file imports, not rules of its own file or of a file imported beside it"
	}
	if l == layerZalando {
		return "ZALANDO rules override nothing"
	}
	return fmt.Sprintf("%s rules override only %s rules", layerNames[l], strings.Join(layerNames[:l], " and "))
}

// overridden returns a copy of spec that reports nothing. It stays a rule,
// so buf.yaml can still name it in use and except.
func overridden(spec *check.RuleSpec, by []string) *check.RuleSpec {
	out := *spec
	out.Purpose = fmt.Sprintf("Overridden by %s. %s", strings.Join(by, ", "), spec.Purpose)
	out.Handler = check.RuleHandlerFunc(func(context.Context, check.ResponseWriter, check.Request) error { return nil })
	return &out
}

// Report describes a consolidated rule set for dedo tools: the house
// rules, every override and the rules that run.
func (c *Consolidated) Report() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d rules, %d of them house rules; %d overridden\n", len(c.Rules), c.House, c.Overridden)
	for _, o := range c.Overrides {
		by := o.By
		if o.Source != "" {
			by += " (" + o.Source + ")"
		}
		lapsed := ""
		if o.Lapsed {
			lapsed = ", lapsed: " + o.By + " is overridden itself"
		}
		fmt.Fprintf(&b, "  %s overrides %s%s\n", by, o.Rule, lapsed)
	}
	for _, f := range c.Final {
		rule := f.Rule
		if f.Source != "" {
			rule += " (" + f.Source + ")"
		}
		fmt.Fprintf(&b, "  %s is final\n", rule)
	}
	return b.String()
}

// consolidateHouse compiles house rules and consolidates them with dedo's
// own.
func consolidateHouse(houseRules []config.Rule, disabled []string, builtin []*check.RuleSpec) (*Consolidated, error) {
	specs, err := house.Specs(houseRules)
	if err != nil {
		return nil, fmt.Errorf("house rules: %w", err)
	}
	c, err := Consolidate(builtin, houseRules, specs, disabled)
	if err != nil {
		return nil, fmt.Errorf("rule overrides:\n%w", err)
	}
	return c, nil
}
