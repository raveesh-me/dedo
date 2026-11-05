package restdoc

import (
	"strings"
	"testing"

	"github.com/raveesh-me/dedo/aggregate"
)

// A service whose tag repeats its package's heading draws a warning only
// when it is not named after the one root it manages; the warning suggests
// the root.
func TestTitleWarning(t *testing.T) {
	quote := aggregate.Resource{Type: "acme.example.com/TaxQuote", Plural: "taxQuotes"}
	order := aggregate.Resource{Type: "acme.example.com/Order", Plural: "orders"}
	for _, c := range []struct {
		name, service, title, heading string
		roots                         []aggregate.Resource
		want                          []string // substrings; none for no warning
	}{
		{"different title", "CalculationService", "Tax quotes", "Calculation", []aggregate.Resource{quote}, nil},
		{"named after its root", "OrderService", "Orders", "Orders", []aggregate.Resource{order}, nil},
		{"no root", "AddressService", "Addresses", "Addresses", nil, nil},
		{"one other root", "CalculationService", "Calculation", "Calculation", []aggregate.Resource{quote},
			[]string{`acme.calculation.v1.CalculationService`, `"Calculation › Calculation"`, `acme.example.com/TaxQuote`, `TaxQuoteService`, `"Tax quotes"`}},
		{"case differs", "TaxProfileService", "Tax Profile", "Tax profile", []aggregate.Resource{quote},
			[]string{`"Tax profile › Tax Profile"`, `TaxQuoteService`}},
		{"no plural", "CalculationService", "Calculation", "Calculation", []aggregate.Resource{{Type: "acme.example.com/TaxQuote"}},
			[]string{`TaxQuoteService`}},
		{"two roots", "CheckoutService", "Checkout", "Checkout", []aggregate.Resource{quote, order},
			[]string{`2 roots`, `acme.example.com/TaxQuote, acme.example.com/Order`, `one service per root`}},
	} {
		got := titleWarning("acme.calculation.v1."+c.service, c.service, c.title, c.heading, c.roots)
		if len(c.want) == 0 {
			if got != "" {
				t.Errorf("%s: warning %q, want none", c.name, got)
			}
			continue
		}
		for _, w := range c.want {
			if !strings.Contains(got, w) {
				t.Errorf("%s: warning %q lacks %s", c.name, got, w)
			}
		}
	}
	if got := titleWarning("x.NoPluralService", "CalculationService", "Calculation", "Calculation", []aggregate.Resource{{Type: "acme.example.com/TaxQuote"}}); strings.Contains(got, "TaxQuoteService titled") {
		t.Errorf("warning %q suggests a title without a declared plural", got)
	}
}

// The generator reports the warning through Config.Warn and still writes
// the document. dedo's own layout, OrderService titled "Orders" in package
// orders, draws none.
func TestGenerateWarnsOnRepeatedHeading(t *testing.T) {
	for _, c := range []struct {
		protos []string
		want   int
	}{
		{[]string{"acme/calculation/v1/calculation.proto", "acme/catalog/v1/catalog.proto"}, 1},
		{shopProtos, 0},
		{nil, 0}, // the library: one package, so no package heading
	} {
		var warnings []string
		cfg := Config{Warn: func(msg string) { warnings = append(warnings, msg) }}
		if out := generateWith(t, cfg, c.protos...); out == "" {
			t.Errorf("%v: no document", c.protos)
		}
		if len(warnings) != c.want {
			t.Errorf("%v: warnings %q, want %d", c.protos, warnings, c.want)
		}
	}
}

// sentence turns a lowerCamelCase plural into a heading.
func TestSentence(t *testing.T) {
	for in, want := range map[string]string{"taxQuotes": "Tax quotes", "orders": "Orders", "": ""} {
		if got := sentence(in); got != want {
			t.Errorf("sentence(%q) = %q, want %q", in, got, want)
		}
	}
}
