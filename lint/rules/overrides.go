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

// Consolidated is the rule set a lint runs: every rule, in the category
// DEDO, with each rule that does not run reporting nothing.
type Consolidated struct {
	Rules     []*check.RuleSpec
	Overrides []Override
	// Use are the rule sets and rules of dedo's that rules.yml uses.
	Use []string
	// DefaultUse is set when rules.yml does not say use.
	DefaultUse bool
	// Disabled are the rules of dedo's that rules.yml disables.
	Disabled []string
	// House is the number of house rules.
	House int
	// Overridden is the number of rules a running rule overrides.
	Overridden int
	// Off is the number of rules of dedo's that do not run: not in use,
	// skipped, or disabled.
	Off int
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

// Consolidate checks rules.yml's use and disable and every override
// against the order of layers, the final rules and the rules that exist,
// and returns the rules to run. It reports every mistake at once. A rule
// runs when use names it or a set it is in (HOUSE for house rules), unless
// disable names it; a final rule always runs. disabled may also name house rules, which
// houseRules no longer holds: an override of one has nothing left to
// override.
func Consolidate(builtin []*check.RuleSpec, houseRules []config.Rule, houseSpecs []*check.RuleSpec, use, disabled []string) (*Consolidated, error) {
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
	out := &Consolidated{House: len(houseSpecs), Use: use}
	if use == nil {
		out.Use, out.DefaultUse = DefaultUse, true
	}
	off, err := selection(all, byID, out.Use, disabled)
	if err != nil {
		return nil, err
	}
	for _, id := range disabled {
		if !config.IsHouse(id) {
			out.Disabled = append(out.Disabled, id)
		}
	}
	if err := checkOverrides(all, byID, disabled); err != nil {
		return nil, err
	}
	// A rule runs unless it is off or a rule that runs overrides it.
	// Overriders rank strictly higher, so this ends.
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
		r := off[id] == ""
		for _, o := range overriders[id] {
			r = r && !running(o.spec.ID)
		}
		runs[id] = r
		return r
	}
	for _, c := range all {
		spec := c.spec
		var by []string
		for _, o := range overriders[spec.ID] {
			if running(o.spec.ID) {
				by = append(by, o.spec.ID)
			}
		}
		switch {
		case off[spec.ID] != "":
			spec = silenced(spec, off[spec.ID])
			out.Off++
		case len(by) > 0:
			spec = silenced(spec, fmt.Sprintf("Overridden by %s.", strings.Join(by, ", ")))
			out.Overridden++
		default:
			copied := *spec
			spec = &copied
		}
		spec.CategoryIDs = []string{CategoryDedo}
		spec.Default = true
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

// selection checks rules.yml's use and disable against the rules, and
// returns why each rule that does not run is off.
func selection(all []candidate, byID map[string]candidate, use, disabled []string) (map[string]string, error) {
	sets := map[string]bool{house.Category: true}
	for _, c := range all {
		for _, set := range c.spec.CategoryIDs {
			sets[set] = true
		}
	}
	var errs []error
	for _, u := range use {
		if _, ok := byID[u]; !ok && !sets[u] {
			errs = append(errs, fmt.Errorf("rules.yml: use names %s, which is neither a rule set such as AIP_NOLANG, ZALANDO, DEDO, HOUSE or DEDO_MULTI_TENANT nor a rule", u))
		}
	}
	for _, id := range disabled {
		switch {
		case config.IsHouse(id):
			// Checked against the rules files already.
		case byID[id].spec == nil:
			errs = append(errs, fmt.Errorf("rules.yml: disable names %s, which no rule defines", id))
		case Final[id] != "":
			errs = append(errs, fmt.Errorf("rules.yml: disable names %s, which is final: %s. Keep it", id, Final[id]))
		}
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	off := map[string]string{}
	for _, c := range all {
		id := c.spec.ID
		switch {
		case Final[id] != "" || c.house != nil && c.house.Final:
			// Always runs.
		case slices.Contains(disabled, id):
			off[id] = "Disabled in rules.yml."
		case slices.Contains(use, id):
			// Named by ID, skipped or not.
		case Skipped[id] != "":
			off[id] = "Skipped by dedo: " + Skipped[id]
		case !slices.ContainsFunc(c.spec.CategoryIDs, func(set string) bool { return slices.Contains(use, set) }):
			off[id] = "Not in rules.yml's use."
		}
	}
	return off, nil
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

// silenced returns a copy of spec that reports nothing, its purpose
// prefixed with why. It stays a rule, so buf config ls-lint-rules lists it.
func silenced(spec *check.RuleSpec, why string) *check.RuleSpec {
	out := *spec
	out.Purpose = why + " " + spec.Purpose
	out.Handler = check.RuleHandlerFunc(func(context.Context, check.ResponseWriter, check.Request) error { return nil })
	return &out
}

// Report describes a consolidated rule set for dedo tools: the house
// rules, every override and the rules that run.
func (c *Consolidated) Report() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d rules, %d of them house rules; %d run, %d overridden, %d not used, skipped or disabled\n", len(c.Rules), c.House, len(c.Rules)-c.Overridden-c.Off, c.Overridden, c.Off)
	use := strings.Join(c.Use, ", ")
	if c.DefaultUse {
		use += " (the default; rules.yml does not say use)"
	}
	fmt.Fprintf(&b, "  use: %s\n", use)
	for _, id := range c.Disabled {
		fmt.Fprintf(&b, "  %s is disabled\n", id)
	}
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

// consolidateHouse compiles a rules.yml's house rules and consolidates
// them with dedo's own.
func consolidateHouse(cfg config.Config, builtin []*check.RuleSpec) (*Consolidated, error) {
	specs, err := house.Specs(cfg.Rules)
	if err != nil {
		return nil, fmt.Errorf("house rules: %w", err)
	}
	c, err := Consolidate(builtin, cfg.Rules, specs, cfg.Use, cfg.Disabled)
	if err != nil {
		return nil, fmt.Errorf("lint rules:\n%w", err)
	}
	return c, nil
}
