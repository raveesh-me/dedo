package gorules

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"slices"

	"golang.org/x/tools/go/analysis"
)

// A flow follows constant strings into a sink: an error reason into a
// google.rpc.ErrorInfo, an event type into events.Publisher.Publish. A
// handler hands the sink a constant directly, or through helpers that pass
// a parameter of theirs on unchanged, such as apiError(code, reason, msg);
// facts carry which parameters each helper passes on, across packages.
type flow struct {
	// direct returns the expressions a node hands the sink itself.
	direct func(pass *analysis.Pass, n ast.Node) []ast.Expr
	// fact reads the parameters a function passes on, from its fact.
	fact func(pass *analysis.Pass, fn *types.Func) []int
	// export records them as fn's fact.
	export func(pass *analysis.Pass, fn *types.Func, params []int)
}

// passOn maps the package's functions to the parameters they pass on.
type passOn map[*types.Func][]int

// params returns the parameters fn passes on to the sink.
func (fl flow) params(pass *analysis.Pass, local passOn, fn *types.Func) []int {
	if p, ok := local[fn]; ok {
		return p
	}
	return fl.fact(pass, fn)
}

// sinkArgs returns the expressions n hands the sink, directly or through a
// call to a function that passes a parameter on.
func (fl flow) sinkArgs(pass *analysis.Pass, local passOn, n ast.Node) []ast.Expr {
	out := fl.direct(pass, n)
	call, ok := n.(*ast.CallExpr)
	if !ok {
		return out
	}
	fn := staticCallee(pass, call)
	if fn == nil {
		return out
	}
	for _, i := range fl.params(pass, local, fn) {
		if arg := argument(pass, fn, call, i); arg != nil {
			out = append(out, arg)
		}
	}
	return out
}

// argument returns the argument of call for parameter i of fn, or nil when
// there is none to read: a variadic parameter, or a spread slice. A method
// expression, T.Method(recv, ...), takes the receiver first.
func argument(pass *analysis.Pass, fn *types.Func, call *ast.CallExpr, i int) ast.Expr {
	sig := fn.Type().(*types.Signature)
	if sig.Variadic() && i >= sig.Params().Len()-1 {
		return nil
	}
	if sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr); ok {
		if s, ok := pass.TypesInfo.Selections[sel]; ok && s.Kind() == types.MethodExpr {
			i++
		}
	}
	if i >= len(call.Args) {
		return nil
	}
	return call.Args[i]
}

// summarize finds the parameters each function of the package passes on,
// in as many rounds as helpers calling each other need, and exports them
// as facts.
func (fl flow) summarize(pass *analysis.Pass) passOn {
	local := passOn{}
	decls := funcDecls(pass)
	for changed := true; changed; {
		changed = false
		for fn, fd := range decls {
			// The parameters passed on only grow, round by round.
			if p := fl.passedOn(pass, local, fn, fd); len(p) > len(local[fn]) {
				local[fn] = p
				changed = true
			}
		}
	}
	for fn, p := range local {
		fl.export(pass, fn, p)
	}
	return local
}

// passedOn returns the parameters of fn that reach the sink unchanged.
func (fl flow) passedOn(pass *analysis.Pass, local passOn, fn *types.Func, fd *ast.FuncDecl) []int {
	sig := fn.Type().(*types.Signature)
	index := map[*types.Var]int{}
	for i := 0; i < sig.Params().Len(); i++ {
		if !(sig.Variadic() && i == sig.Params().Len()-1) {
			index[sig.Params().At(i)] = i
		}
	}
	changed := reassigned(pass, fd.Body)
	var out []int
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		for _, e := range fl.sinkArgs(pass, local, n) {
			id, ok := ast.Unparen(e).(*ast.Ident)
			if !ok {
				continue
			}
			v, ok := pass.TypesInfo.Uses[id].(*types.Var)
			if i, isParam := index[v]; ok && isParam && !changed[v] && !slices.Contains(out, i) {
				out = append(out, i)
			}
		}
		return true
	})
	return out
}

// reassigned returns the variables a body assigns, increments or takes the
// address of: a parameter it changes no longer holds what the caller passed.
func reassigned(pass *analysis.Pass, body *ast.BlockStmt) map[*types.Var]bool {
	out := map[*types.Var]bool{}
	mark := func(e ast.Expr) {
		if id, ok := ast.Unparen(e).(*ast.Ident); ok {
			if v, ok := pass.TypesInfo.Uses[id].(*types.Var); ok {
				out[v] = true
			}
		}
	}
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.AssignStmt:
			for _, lhs := range n.Lhs {
				mark(lhs)
			}
		case *ast.IncDecStmt:
			mark(n.X)
		case *ast.UnaryExpr:
			if n.Op == token.AND {
				mark(n.X)
			}
		}
		return true
	})
	return out
}

// funcDecls maps the package's functions and methods with bodies to their
// declarations.
func funcDecls(pass *analysis.Pass) map[*types.Func]*ast.FuncDecl {
	out := map[*types.Func]*ast.FuncDecl{}
	for _, f := range pass.Files {
		for _, decl := range f.Decls {
			if fd, ok := decl.(*ast.FuncDecl); ok && fd.Body != nil {
				if fn, ok := pass.TypesInfo.Defs[fd.Name].(*types.Func); ok {
					out[fn] = fd
				}
			}
		}
	}
	return out
}

// constString returns the value of a constant string expression.
func constString(pass *analysis.Pass, e ast.Expr) (string, bool) {
	tv, ok := pass.TypesInfo.Types[e]
	if !ok || tv.Value == nil || tv.Value.Kind() != constant.String {
		return "", false
	}
	return constant.StringVal(tv.Value), true
}
