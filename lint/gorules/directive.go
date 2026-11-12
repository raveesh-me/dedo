package gorules

import (
	"fmt"
	"go/ast"
	"go/token"
	"regexp"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/ast/astutil"
)

// ignorePrefix starts a suppression directive.
const ignorePrefix = "//dedo:ignore"

var ruleIDPattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)

// directive is one //dedo:ignore comment.
type directive struct {
	pos    token.Pos
	line   int
	rule   string
	reason string
}

// valid reports whether the directive suppresses anything: it names a rule
// and gives a reason.
func (d directive) valid() bool {
	return ruleIDPattern.MatchString(d.rule) && d.reason != ""
}

// parseDirective reads a comment's text as a directive:
// //dedo:ignore RULE_ID reason.
func parseDirective(text string) (rule, reason string, ok bool) {
	rest, ok := strings.CutPrefix(text, ignorePrefix)
	if !ok || rest != "" && rest[0] != ' ' && rest[0] != '\t' {
		return "", "", false
	}
	// A comment after the directive, from " //" on, is not its reason.
	rest, _, _ = strings.Cut(rest, " //")
	fields := strings.Fields(rest)
	if len(fields) == 0 {
		return "", "", true
	}
	return fields[0], strings.Join(fields[1:], " "), true
}

// directives returns the //dedo:ignore comments of a file.
func directives(fset *token.FileSet, f *ast.File) []directive {
	var out []directive
	for _, group := range f.Comments {
		for _, c := range group.List {
			rule, reason, ok := parseDirective(c.Text)
			if !ok {
				continue
			}
			out = append(out, directive{pos: c.Pos(), line: fset.Position(c.Pos()).Line, rule: rule, reason: reason})
		}
	}
	return out
}

// reporter reports a pass's findings, leaving out those a directive
// suppresses.
type reporter struct {
	pass  *analysis.Pass
	files map[*ast.File][]directive
}

func newReporter(pass *analysis.Pass) *reporter {
	return &reporter{pass: pass, files: map[*ast.File][]directive{}}
}

// fileOf returns the file of the pass that holds pos.
func (r *reporter) fileOf(pos token.Pos) *ast.File {
	for _, f := range r.pass.Files {
		if f.FileStart <= pos && pos <= f.FileEnd {
			return f
		}
	}
	return nil
}

// report reports a finding of rule at pos, unless a directive suppresses
// it. The message ends with the link to the rule's documentation.
func (r *reporter) report(pos token.Pos, rule, doc, format string, args ...any) {
	if r.suppressed(pos, rule) {
		return
	}
	msg := rule + ": " + fmt.Sprintf(format, args...)
	if doc != "" {
		msg += " (" + doc + ")"
	}
	r.pass.Report(analysis.Diagnostic{Pos: pos, Category: rule, Message: msg})
}

// suppressed reports whether a directive with a reason for rule sits on
// the line of pos, the line above it, or the line above the statement or
// declaration that holds pos.
func (r *reporter) suppressed(pos token.Pos, rule string) bool {
	f := r.fileOf(pos)
	if f == nil {
		return false
	}
	ds, ok := r.files[f]
	if !ok {
		ds = directives(r.pass.Fset, f)
		r.files[f] = ds
	}
	if len(ds) == 0 {
		return false
	}
	lines := r.targetLines(f, pos)
	for _, d := range ds {
		if d.rule == rule && d.valid() && (lines[d.line] || lines[d.line+1]) {
			return true
		}
	}
	return false
}

// targetLines are the lines a directive on, or directly above, suppresses
// a finding at pos: its own, and the first line of the statement, import
// or declaration that holds it.
func (r *reporter) targetLines(f *ast.File, pos token.Pos) map[int]bool {
	lines := map[int]bool{r.pass.Fset.Position(pos).Line: true}
	path, _ := astutil.PathEnclosingInterval(f, pos, pos)
	for _, n := range path {
		switch n.(type) {
		case ast.Stmt, ast.Spec, ast.Decl:
			lines[r.pass.Fset.Position(n.Pos()).Line] = true
			return lines
		}
	}
	return lines
}
