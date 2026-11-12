package gorules

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/raveesh-me/dedo/lint/house/config"
)

// The rule sets go.use names.
const (
	SetDedo  = "DEDO"
	SetHouse = "HOUSE"
)

// DefaultUse runs when dedo.yaml's go section does not say use.
var DefaultUse = []string{SetDedo, SetHouse}

// Override is one rule overriding another.
type Override struct {
	By, Rule string
	// Source is the rules file of By, or "" for dedo's own rules.
	Source string
	// Lapsed is set when By is itself overridden, so Rule runs again.
	Lapsed bool
}

// Plan is the Go rules a run uses, consolidated from the go sections.
type Plan struct {
	// Use are the sets and rules dedo.yaml's go.use names.
	Use []string
	// DefaultUse is set when dedo.yaml does not say go.use.
	DefaultUse bool
	// Builtin are the IDs of dedo's rules that run.
	Builtin []string
	// House are the house rules that run.
	House []config.GoRule
	// Off says why each rule that does not run is off, by ID.
	Off       map[string]string
	Overrides []Override
	// Final lists the final rules, with the rules file of a house one.
	Final []Override
	// Disabled are dedo's rules that go.disable names.
	Disabled []string
	// Total is the number of rules, dedo's and the house ones.
	Total, HouseTotal int
}

// candidate is a rule as Consolidate ranks it.
type candidate struct {
	id        string
	overrides []string
	final     string
	house     *config.GoRule // nil for dedo's own rules
}

func (c candidate) source() string {
	if c.house == nil {
		return ""
	}
	return c.house.Source
}

// outranks reports whether a may override b: a house rule outranks dedo's,
// and a rule of a file that imports b's.
func outranks(a, b candidate) bool {
	if a.house == nil || b.house == nil {
		return a.house != nil && b.house == nil
	}
	return slices.Contains(b.house.Importers, a.house.File)
}

// Consolidate checks the go sections' use, disable and every override
// against the order of layers and the final rules, and returns what runs.
// It reports every mistake at once.
func Consolidate(sel config.GoSelection) (Plan, error) {
	var all []candidate
	byID := map[string]candidate{}
	for _, b := range Builtins() {
		c := candidate{id: b.ID, final: b.Final}
		all = append(all, c)
		byID[c.id] = c
	}
	for i := range sel.Rules {
		r := &sel.Rules[i]
		c := candidate{id: r.ID, overrides: r.Overrides, house: r}
		if r.Final {
			c.final = "marked final in " + r.Source
		}
		all = append(all, c)
		byID[c.id] = c
	}
	p := Plan{Use: sel.Use, Total: len(all), HouseTotal: len(sel.Rules)}
	if p.Use == nil {
		p.Use, p.DefaultUse = DefaultUse, true
	}
	off, err := selectRules(all, byID, p.Use, sel.Disabled)
	if err != nil {
		return Plan{}, err
	}
	if err := checkOverrides(all, byID, sel.Disabled); err != nil {
		return Plan{}, err
	}
	p.Off = off
	p.decide(all, sel.Disabled)
	return p, nil
}

// decide fills in what runs, the overrides and the final rules.
func (p *Plan) decide(all []candidate, disabled []string) {
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
		r := p.Off[id] == ""
		for _, o := range overriders[id] {
			r = r && !running(o.id) // overriders rank higher, so this ends
		}
		runs[id] = r
		return r
	}
	for _, c := range all {
		var by []string
		for _, o := range overriders[c.id] {
			if running(o.id) {
				by = append(by, o.id)
			}
		}
		if p.Off[c.id] == "" && len(by) > 0 {
			p.Off[c.id] = "Overridden by " + strings.Join(by, ", ") + "."
		}
		switch {
		case !running(c.id):
		case c.house != nil:
			p.House = append(p.House, *c.house)
		default:
			p.Builtin = append(p.Builtin, c.id)
		}
		for _, id := range c.overrides {
			p.Overrides = append(p.Overrides, Override{By: c.id, Rule: id, Source: c.source(), Lapsed: !running(c.id)})
		}
		if c.final != "" {
			p.Final = append(p.Final, Override{Rule: c.id, Source: c.source()})
		}
	}
	sort.SliceStable(p.Overrides, func(i, j int) bool { return p.Overrides[i].By < p.Overrides[j].By })
	for _, id := range disabled {
		if !config.IsHouse(id) {
			p.Disabled = append(p.Disabled, id)
		}
	}
}

// selectRules checks use and disable, and returns why each rule they turn
// off is off.
func selectRules(all []candidate, byID map[string]candidate, use, disabled []string) (map[string]string, error) {
	var errs []error
	for _, u := range use {
		if _, ok := byID[u]; !ok && u != SetDedo && u != SetHouse {
			errs = append(errs, fmt.Errorf("dedo.yaml: go.use names %s, which is neither a rule set (DEDO or HOUSE) nor a go rule", u))
		}
	}
	for _, id := range disabled {
		switch c, ok := byID[id]; {
		case config.IsHouse(id):
			// The rules files are checked already.
		case !ok:
			errs = append(errs, fmt.Errorf("dedo.yaml: go.disable names %s, which no go rule defines", id))
		case c.final != "":
			errs = append(errs, fmt.Errorf("dedo.yaml: go.disable names %s, which is final: %s. Keep it", id, c.final))
		}
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	off := map[string]string{}
	for _, c := range all {
		set := SetDedo
		if c.house != nil {
			set = SetHouse
		}
		switch {
		case c.final != "":
			// Always runs.
		case slices.Contains(disabled, c.id):
			off[c.id] = "Disabled in dedo.yaml's go section."
		case !slices.Contains(use, c.id) && !slices.Contains(use, set):
			off[c.id] = "Not in dedo.yaml's go.use."
		}
	}
	return off, nil
}

// checkOverrides checks that each override names a go rule that ranks
// below its rule and is not final.
func checkOverrides(all []candidate, byID map[string]candidate, disabled []string) error {
	var errs []error
	for _, c := range all {
		at := c.id
		if src := c.source(); src != "" {
			at = src + ": " + at
		}
		for _, id := range c.overrides {
			target, ok := byID[id]
			switch {
			case !ok && slices.Contains(disabled, id):
				// Disabled already; the override changes nothing.
			case !ok:
				errs = append(errs, fmt.Errorf("%s overrides %s, which no go rule defines; a go rule overrides only go rules", at, id))
			case target.final != "":
				errs = append(errs, fmt.Errorf("%s overrides %s, which is final (%s). Keep %s and add your rule beside it", at, id, target.final, id))
			case !outranks(c, target):
				errs = append(errs, fmt.Errorf("%s cannot override %s: a house go rule overrides only dedo's go rules and the go rules of the files its rules file imports", at, id))
			}
		}
	}
	return errors.Join(errs...)
}

// Report describes the plan for dedo tools: the rules that run, every
// override and the final rules.
func (p Plan) Report() string {
	var b strings.Builder
	running := len(p.Builtin) + len(p.House)
	overridden := 0
	for _, why := range p.Off {
		if strings.HasPrefix(why, "Overridden by") {
			overridden++
		}
	}
	fmt.Fprintf(&b, "go: %d rules, %d of them house rules; %d run, %d overridden, %d not used or disabled\n",
		p.Total, p.HouseTotal, running, overridden, p.Total-running-overridden)
	use := strings.Join(p.Use, ", ")
	if p.DefaultUse {
		use += " (the default; dedo.yaml does not say go.use)"
	}
	fmt.Fprintf(&b, "  use: %s\n", use)
	for _, id := range p.Disabled {
		fmt.Fprintf(&b, "  %s is disabled\n", id)
	}
	for _, r := range p.House {
		fmt.Fprintf(&b, "  %s (%s) forbids importing %s\n", r.ID, r.Source, r.ForbidImport)
	}
	for _, o := range p.Overrides {
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
	for _, f := range p.Final {
		rule := f.Rule
		if f.Source != "" {
			rule += " (" + f.Source + ")"
		}
		fmt.Fprintf(&b, "  %s is final\n", rule)
	}
	return b.String()
}
