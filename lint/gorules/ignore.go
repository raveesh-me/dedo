package gorules

import (
	"fmt"

	"golang.org/x/tools/go/analysis"
)

// IgnoreReason reports //dedo:ignore directives that name no rule or give no
// reason. Such a directive suppresses nothing.
var IgnoreReason = &analysis.Analyzer{
	Name: analyzerName(IDIgnoreReason),
	Doc:  "reports //dedo:ignore directives without a rule ID and a reason (" + IDIgnoreReason + ")",
	URL:  DocsURL(IDIgnoreReason),
	Run:  runIgnoreReason,
}

func runIgnoreReason(pass *analysis.Pass) (any, error) {
	for _, f := range pass.Files {
		for _, d := range directives(pass.Fset, f) {
			switch {
			case d.rule == "":
				reportIgnore(pass, d, "//dedo:ignore names no rule: write //dedo:ignore <RULE_ID> <reason>")
			case !ruleIDPattern.MatchString(d.rule):
				reportIgnore(pass, d, "//dedo:ignore names %q, which is not a rule ID such as "+IDErrorReasonDeclared, d.rule)
			case d.reason == "":
				reportIgnore(pass, d, "//dedo:ignore %s gives no reason, so it suppresses nothing: say why the finding does not apply", d.rule)
			}
		}
	}
	return nil, nil
}

// reportIgnore reports a directive. These findings cannot be suppressed.
func reportIgnore(pass *analysis.Pass, d directive, format string, args ...any) {
	pass.Report(analysis.Diagnostic{
		Pos:      d.pos,
		Category: IDIgnoreReason,
		Message:  IDIgnoreReason + ": " + fmt.Sprintf(format, args...) + " (" + DocsURL(IDIgnoreReason) + ")",
	})
}
