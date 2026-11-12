// Package gorules holds dedo's Go rules: analyzers on
// golang.org/x/tools/go/analysis that check the Go code serving a dedo
// contract, which dedo vet runs through go vet -vettool.
//
// dedo's own rules are DEDO_GO_*; an organisation's are HOUSE_GO_*, from the
// go sections of dedo.yaml and the rules files it imports (package config),
// ranked, overridden and made final as lint rules are (Consolidate). Every
// finding names its rule and links to its section of guidance/RULES.md, or to
// a house rule's doc. A finding is suppressed by a directive with a reason,
// on its line or the line above it, or above the statement it is in:
//
//	//dedo:ignore DEDO_GO_ERROR_REASON_DECLARED the reason is declared by the gateway
//
// A directive without a reason suppresses nothing and is itself a finding,
// DEDO_GO_IGNORE_REASON.
package gorules

import (
	"strings"

	"golang.org/x/tools/go/analysis"
)

// The DEDO_GO_* rule IDs. Each has a heading of the same name in
// guidance/RULES.md.
const (
	IDServerInterceptors  = "DEDO_GO_SERVER_INTERCEPTORS"
	IDErrorReasonDeclared = "DEDO_GO_ERROR_REASON_DECLARED"
	IDEventDeclared       = "DEDO_GO_EVENT_DECLARED"
	IDIgnoreReason        = "DEDO_GO_IGNORE_REASON"
)

// DocsRef is the git ref that rule documentation links point at. dedo vet
// sets it to its release, so a finding links to the rules as they were then.
var DocsRef = "main"

const docsBase = "https://github.com/raveesh-me/dedo/blob/"

// DocsURL returns the link to a rule's section in guidance/RULES.md.
func DocsURL(ruleID string) string {
	return docsBase + DocsRef + "/guidance/RULES.md#" + strings.ToLower(ruleID)
}

// Builtin is one of dedo's own Go rules.
type Builtin struct {
	ID       string
	Analyzer *analysis.Analyzer
	// Final says why the rule cannot be disabled or overridden, or "".
	Final string
}

// Builtins are dedo's Go rules, in the order RULES.md documents them.
func Builtins() []Builtin {
	return []Builtin{
		{ID: IDServerInterceptors, Analyzer: ServerInterceptors},
		{ID: IDErrorReasonDeclared, Analyzer: ErrorReasonDeclared},
		{ID: IDEventDeclared, Analyzer: EventDeclared},
		{ID: IDIgnoreReason, Analyzer: IgnoreReason, Final: "a suppression without a reason is a decision nobody can review"},
	}
}

// Analyzers are every analyzer dedo vet runs, the house rules' among them:
// what go vet -vettool runs.
func Analyzers() []*analysis.Analyzer {
	var out []*analysis.Analyzer
	for _, b := range Builtins() {
		out = append(out, b.Analyzer)
	}
	return append(out, House)
}

// analyzerName is the analyzer name of a rule: its ID in lower case, which
// is also the flag go vet turns it off with.
func analyzerName(id string) string {
	return strings.ToLower(id)
}
