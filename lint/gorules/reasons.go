package gorules

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"slices"
	"strings"

	"golang.org/x/tools/go/analysis"
)

const errdetailsPkg = "google.golang.org/genproto/googleapis/rpc/errdetails"

// ErrorReasonDeclared checks that a constant reason a handler puts in a
// google.rpc.ErrorInfo is one its RPC declares in dedo.v1.method's errors.
var ErrorReasonDeclared = &analysis.Analyzer{
	Name:      analyzerName(IDErrorReasonDeclared),
	Doc:       "checks that the constant ErrorInfo reasons a handler returns are declared on its RPC (" + IDErrorReasonDeclared + ")",
	URL:       DocsURL(IDErrorReasonDeclared),
	Run:       runErrorReasonDeclared,
	FactTypes: []analysis.Fact{new(reasonContract), new(reasonParams), new(alwaysReason)},
}

// reasonContract is a generated package's contract, for this analyzer.
type reasonContract struct{ C contract }

func (*reasonContract) AFact()         {}
func (*reasonContract) String() string { return "contract" }

// reasonParams marks a function that puts parameters of its own into an
// ErrorInfo's reason unchanged, such as apiError(code, reason, message).
type reasonParams struct{ Params []int }

func (*reasonParams) AFact()           {}
func (f *reasonParams) String() string { return "reason params " + ints(f.Params) }

// alwaysReason marks a function every return of which ends in an error with
// one constant reason, such as notFound(name).
type alwaysReason struct{ Reason string }

func (*alwaysReason) AFact()           {}
func (f *alwaysReason) String() string { return "always reason " + f.Reason }

var reasonFlow = flow{
	direct: reasonSinks,
	fact: func(pass *analysis.Pass, fn *types.Func) []int {
		var f reasonParams
		pass.ImportObjectFact(fn, &f)
		return f.Params
	},
	export: func(pass *analysis.Pass, fn *types.Func, params []int) {
		pass.ExportObjectFact(fn, &reasonParams{Params: params})
	},
}

// reasonSinks returns the reasons a node sets on an ErrorInfo: the Reason
// of a composite literal, or the value assigned to a Reason field.
func reasonSinks(pass *analysis.Pass, n ast.Node) []ast.Expr {
	switch n := n.(type) {
	case *ast.CompositeLit:
		if !isErrorInfo(pass.TypesInfo.TypeOf(n)) {
			return nil
		}
		for _, elt := range n.Elts {
			if kv, ok := elt.(*ast.KeyValueExpr); ok {
				if key, ok := kv.Key.(*ast.Ident); ok && key.Name == "Reason" {
					return []ast.Expr{kv.Value}
				}
			}
		}
	case *ast.AssignStmt:
		var out []ast.Expr
		for i, lhs := range n.Lhs {
			sel, ok := ast.Unparen(lhs).(*ast.SelectorExpr)
			if ok && sel.Sel.Name == "Reason" && len(n.Rhs) == len(n.Lhs) && isErrorInfo(pass.TypesInfo.TypeOf(sel.X)) {
				out = append(out, n.Rhs[i])
			}
		}
		return out
	}
	return nil
}

// isErrorInfo reports whether t is errdetails.ErrorInfo or a pointer to it.
func isErrorInfo(t types.Type) bool {
	if ptr, ok := t.(*types.Pointer); ok {
		t = ptr.Elem()
	}
	return isNamed(types.Unalias(t), errdetailsPkg, "ErrorInfo")
}

func runErrorReasonDeclared(pass *analysis.Pass) (any, error) {
	if c, ok := readContract(pass); ok {
		pass.ExportPackageFact(&reasonContract{C: c})
	}
	local := reasonFlow.summarize(pass)
	always := summarizeAlways(pass, local)
	cs := contracts{}
	for _, f := range pass.AllPackageFacts() {
		if c, ok := f.Fact.(*reasonContract); ok {
			cs[f.Package.Path()] = c.C
		}
	}
	r := newReporter(pass)
	for _, h := range findHandlers(pass, cs) {
		checkReasons(pass, r, local, always, h)
	}
	return nil, nil
}

// checkReasons reports each constant reason in a handler's body that one
// of its RPCs does not declare.
func checkReasons(pass *analysis.Pass, r *reporter, local passOn, always map[*types.Func]string, h handler) {
	ast.Inspect(h.decl.Body, func(n ast.Node) bool {
		for _, e := range reasonFlow.sinkArgs(pass, local, n) {
			if reason, ok := constString(pass, e); ok {
				reportReason(r, h, e.Pos(), reason)
			}
		}
		if call, ok := n.(*ast.CallExpr); ok {
			if reason, ok := alwaysOf(pass, always, call); ok {
				reportReason(r, h, call.Pos(), reason)
			}
		}
		return true
	})
}

// reportReason reports a reason that one of the handler's RPCs does not
// declare.
func reportReason(r *reporter, h handler, pos token.Pos, reason string) {
	var missing []string
	for _, rpc := range h.rpcs {
		if slices.Contains(rpc.Reasons, reason) {
			return // one of the RPCs the method may serve declares it
		}
		missing = append(missing, rpc.Procedure)
	}
	r.report(pos, IDErrorReasonDeclared, DocsURL(IDErrorReasonDeclared),
		"%s returns the error reason %s, which %s does not declare: add it to the RPC's dedo.v1.method errors, or return a reason it declares",
		h.decl.Name.Name, reason, strings.Join(missing, " or "))
}

// alwaysOf returns the reason a call to a function that always returns one
// gives.
func alwaysOf(pass *analysis.Pass, local map[*types.Func]string, call *ast.CallExpr) (string, bool) {
	fn := staticCallee(pass, call)
	if fn == nil {
		return "", false
	}
	if reason, ok := local[fn]; ok {
		return reason, true
	}
	var f alwaysReason
	if pass.ImportObjectFact(fn, &f) {
		return f.Reason, true
	}
	return "", false
}

// summarizeAlways finds the package's functions every return of which ends
// in a call that gives one constant reason, and exports them as facts.
func summarizeAlways(pass *analysis.Pass, local passOn) map[*types.Func]string {
	always := map[*types.Func]string{}
	decls := funcDecls(pass)
	for changed := true; changed; {
		changed = false
		for fn, fd := range decls {
			if _, done := always[fn]; done {
				continue
			}
			if reason, ok := returnedReason(pass, local, always, fd); ok {
				always[fn] = reason
				changed = true
			}
		}
	}
	for fn, reason := range always {
		pass.ExportObjectFact(fn, &alwaysReason{Reason: reason})
	}
	return always
}

// returnedReason returns the one constant reason every return of fd ends
// in, if there is one.
func returnedReason(pass *analysis.Pass, local passOn, always map[*types.Func]string, fd *ast.FuncDecl) (string, bool) {
	var got []string
	ok := true
	returns(fd.Body, func(ret *ast.ReturnStmt) {
		reason, found := "", false
		if len(ret.Results) > 0 {
			reason, found = callReason(pass, local, always, ret.Results[len(ret.Results)-1])
		}
		ok = ok && found
		got = append(got, reason)
	})
	if !ok || len(got) == 0 {
		return "", false
	}
	for _, g := range got[1:] {
		if g != got[0] {
			return "", false
		}
	}
	return got[0], true
}

// callReason returns the one constant reason a call expression gives.
func callReason(pass *analysis.Pass, local passOn, always map[*types.Func]string, e ast.Expr) (string, bool) {
	call, ok := ast.Unparen(e).(*ast.CallExpr)
	if !ok {
		return "", false
	}
	if reason, ok := alwaysOf(pass, always, call); ok {
		return reason, true
	}
	var reasons []string
	for _, arg := range reasonFlow.sinkArgs(pass, local, call) {
		reason, ok := constString(pass, arg)
		if !ok {
			return "", false
		}
		reasons = append(reasons, reason)
	}
	if len(reasons) == 0 || slices.ContainsFunc(reasons, func(r string) bool { return r != reasons[0] }) {
		return "", false
	}
	return reasons[0], true
}

func ints(xs []int) string {
	return strings.Trim(fmt.Sprint(xs), "[]")
}
