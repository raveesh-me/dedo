package gorules

import (
	"go/ast"
	"go/token"
	"go/types"
	"slices"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/types/typeutil"
)

// The packages and functions the interceptor rule looks for.
const (
	connectPkg     = "connectrpc.com/connect/v2"
	flowidPkg      = "github.com/raveesh-me/dedo/flowid"
	interceptorPkg = "github.com/raveesh-me/dedo/interceptor"
)

// ServerInterceptors checks that every connect.NewServer outside tests has
// dedo's flow ID interceptor and its policy interceptor, the flow ID one
// first. With -bff, for a backend for frontend, it wants only the flow ID
// one: a BFF serves no contract of its own to enforce.
var ServerInterceptors = &analysis.Analyzer{
	Name:      analyzerName(IDServerInterceptors),
	Doc:       "checks that connect.NewServer has dedo's flow ID and policy interceptors, in order (" + IDServerInterceptors + ")",
	URL:       DocsURL(IDServerInterceptors),
	Run:       runServerInterceptors,
	FactTypes: []analysis.Fact{new(interceptorFact)},
}

var bffMode bool

func init() {
	ServerInterceptors.Flags.BoolVar(&bffMode, "bff", false, "the code is a backend for frontend's: require only the flow ID interceptor")
}

// origin is what an interceptor argument is known to be.
type origin int

const (
	unknown     origin = iota // could be anything: no finding rests on it
	other                     // known, and not one of dedo's
	flowID                    // flowid.Interceptor()
	policyGuard               // the first result of interceptor.New
)

// interceptorFact marks a function whose every return gives one of dedo's
// interceptors as its first result, such as a helper that calls
// interceptor.New.
type interceptorFact struct{ Origin int }

func (*interceptorFact) AFact() {}

func (f *interceptorFact) String() string {
	return "returns interceptor " + [...]string{"?", "other", "flowid", "policy"}[f.Origin]
}

func runServerInterceptors(pass *analysis.Pass) (any, error) {
	rs := newResolver(pass)
	r := newReporter(pass)
	for _, f := range pass.Files {
		if isTestFile(pass, f) {
			continue
		}
		for _, decl := range f.Decls {
			if fd, ok := decl.(*ast.FuncDecl); ok && fd.Body != nil {
				rs.checkServers(r, fd.Body)
			}
		}
	}
	return nil, nil
}

// resolver says what interceptor expressions are, from the package's own
// functions and the facts of the packages it imports.
type resolver struct {
	pass *analysis.Pass
	// local is what each function of the package returning a
	// connect.ServerInterceptor gives.
	local map[*types.Func]origin
}

// newResolver summarizes the package's functions that return a
// connect.ServerInterceptor and exports a fact for each whose result is
// one of dedo's interceptors, or cannot be known: a function without a fact
// returns some other interceptor.
func newResolver(pass *analysis.Pass) *resolver {
	rs := &resolver{pass: pass, local: map[*types.Func]origin{}}
	decls := map[*types.Func]*ast.FuncDecl{}
	for fn, fd := range funcDecls(pass) {
		if sig := fn.Type().(*types.Signature); sig.Results().Len() > 0 && isNamed(sig.Results().At(0).Type(), connectPkg, "ServerInterceptor") {
			decls[fn] = fd
			rs.local[fn] = unknown
		}
	}
	// Each round settles helpers one call deeper; a cycle stays unknown.
	for range len(decls) {
		next := map[*types.Func]origin{}
		for fn, fd := range decls {
			next[fn] = rs.returnedOrigin(fd)
		}
		rs.local = next
	}
	for fn, o := range rs.local {
		if o != other {
			pass.ExportObjectFact(fn, &interceptorFact{Origin: int(o)})
		}
	}
	return rs
}

// checkServers checks every connect.NewServer call in a function body, its
// function literals included, each resolved against its own body.
func (rs *resolver) checkServers(r *reporter, body *ast.BlockStmt) {
	defs := definitions(rs.pass, body)
	ast.Inspect(body, func(n ast.Node) bool {
		if lit, ok := n.(*ast.FuncLit); ok {
			rs.checkServers(r, lit.Body)
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if ok && isFunc(staticCallee(rs.pass, call), connectPkg, "NewServer") {
			rs.checkServer(r, defs, call)
		}
		return true
	})
}

// checkServer reports a NewServer call that lacks one of dedo's
// interceptors, or has them out of order. When an argument cannot be
// resolved, it might be either, so only a wrong order is reported.
func (rs *resolver) checkServer(r *reporter, defs map[*types.Var]ast.Expr, call *ast.CallExpr) {
	origins, ok := rs.argOrigins(defs, call)
	flow, guard := slices.Index(origins, flowID), slices.Index(origins, policyGuard)
	doc := DocsURL(IDServerInterceptors)
	switch {
	case flow >= 0 && guard >= 0 && guard < flow:
		r.report(call.Pos(), IDServerInterceptors, doc, "connect.NewServer runs the policy interceptor before flowid.Interceptor(), so its audit logs carry no flow ID: put flowid.Interceptor() first")
	case !ok:
		// An argument could be anything; nothing more is certain.
	case flow < 0:
		r.report(call.Pos(), IDServerInterceptors, doc, "connect.NewServer has no flowid.Interceptor(): calls carry no X-Flow-ID, on any transport")
	case guard < 0 && !bffMode:
		r.report(call.Pos(), IDServerInterceptors, doc, "connect.NewServer has no policy interceptor (interceptor.New): the declared scopes, cache and deprecation headers and the error audit do not run")
	}
}

// argOrigins resolves each interceptor NewServer receives. ok is false when
// one cannot be resolved.
func (rs *resolver) argOrigins(defs map[*types.Var]ast.Expr, call *ast.CallExpr) ([]origin, bool) {
	args := call.Args
	if call.Ellipsis.IsValid() {
		lit, ok := rs.sliceLiteral(defs, args[len(args)-1])
		if !ok {
			return nil, false
		}
		args = append(args[:len(args)-1:len(args)-1], lit.Elts...)
	}
	var out []origin
	ok := true
	for _, a := range args {
		o := rs.resolve(defs, a, 0)
		out = append(out, o)
		ok = ok && o != unknown
	}
	return out, ok
}

// sliceLiteral resolves the slice NewServer's arguments are spread from,
// when it is a composite literal, directly or through a variable defined
// once.
func (rs *resolver) sliceLiteral(defs map[*types.Var]ast.Expr, e ast.Expr) (*ast.CompositeLit, bool) {
	e = ast.Unparen(e)
	if id, ok := e.(*ast.Ident); ok {
		v, ok := rs.pass.TypesInfo.Uses[id].(*types.Var)
		if !ok || defs[v] == nil {
			return nil, false
		}
		e = ast.Unparen(defs[v])
	}
	lit, ok := e.(*ast.CompositeLit)
	return lit, ok
}

// maxDepth bounds how many variables resolve follows.
const maxDepth = 8

// resolve says what an interceptor expression is.
func (rs *resolver) resolve(defs map[*types.Var]ast.Expr, e ast.Expr, depth int) origin {
	if depth > maxDepth {
		return unknown
	}
	switch e := ast.Unparen(e).(type) {
	case *ast.CallExpr:
		return rs.callOrigin(e)
	case *ast.FuncLit:
		if rs.mentionsInterceptor(e) {
			return unknown // it may wrap one of dedo's
		}
		return other
	case *ast.Ident:
		switch obj := rs.pass.TypesInfo.Uses[e].(type) {
		case *types.Func:
			return other // a function used as an interceptor
		case *types.Var:
			if def, ok := defs[obj]; ok {
				return rs.resolve(defs, def, depth+1)
			}
		}
		return unknown
	case *ast.SelectorExpr:
		if _, ok := rs.pass.TypesInfo.Uses[e.Sel].(*types.Func); ok {
			return other // a method value or a package's function
		}
	}
	return unknown
}

// callOrigin says what a call returns as its first result.
func (rs *resolver) callOrigin(call *ast.CallExpr) origin {
	fn := staticCallee(rs.pass, call)
	switch {
	case fn == nil:
		return unknown // a function value or an interface method
	case isFunc(fn, flowidPkg, "Interceptor"):
		return flowID
	case isFunc(fn, interceptorPkg, "New"):
		return policyGuard
	}
	if o, ok := rs.local[fn]; ok {
		return o
	}
	var fact interceptorFact
	if rs.pass.ImportObjectFact(fn, &fact) {
		return origin(fact.Origin)
	}
	// Without a fact, a function returning a connect.ServerInterceptor
	// returns another interceptor, unless it is handed one to wrap, such
	// as Chain(flowid.Interceptor(), guard).
	sig := fn.Type().(*types.Signature)
	if sig.Results().Len() == 0 || !isNamed(sig.Results().At(0).Type(), connectPkg, "ServerInterceptor") {
		return unknown
	}
	for _, arg := range call.Args {
		if isInterceptorType(rs.pass.TypesInfo.TypeOf(arg)) {
			return unknown
		}
	}
	return other
}

// mentionsInterceptor reports whether a function literal uses a variable
// of an interceptor type declared outside it, or calls a function that
// returns one: it may wrap one of dedo's.
func (rs *resolver) mentionsInterceptor(lit *ast.FuncLit) bool {
	found := false
	ast.Inspect(lit.Body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.Ident:
			v, ok := rs.pass.TypesInfo.Uses[n].(*types.Var)
			if ok && (v.Pos() < lit.Pos() || v.Pos() > lit.End()) && isInterceptorType(v.Type()) {
				found = true
			}
		case *ast.CallExpr:
			if isInterceptorType(rs.pass.TypesInfo.TypeOf(n)) {
				found = true
			}
		}
		return !found
	})
	return found
}

// isInterceptorType reports whether t is a connect.ServerInterceptor, a
// slice of them, or a function of the same shape.
func isInterceptorType(t types.Type) bool {
	if t == nil {
		return false
	}
	if tuple, ok := t.(*types.Tuple); ok {
		return tuple.Len() > 0 && isInterceptorType(tuple.At(0).Type())
	}
	if slice, ok := t.Underlying().(*types.Slice); ok {
		return isInterceptorType(slice.Elem())
	}
	if isNamed(t, connectPkg, "ServerInterceptor") {
		return true
	}
	sig, ok := t.Underlying().(*types.Signature)
	return ok && sig.Params().Len() == 1 && sig.Results().Len() == 1 &&
		isNamed(sig.Params().At(0).Type(), connectPkg, "ServerFunc") &&
		isNamed(sig.Results().At(0).Type(), connectPkg, "ServerFunc")
}

// staticCallee is the function a call statically calls, generic ones as
// declared rather than instantiated, so facts and summaries find them.
func staticCallee(pass *analysis.Pass, call *ast.CallExpr) *types.Func {
	fn := typeutil.StaticCallee(pass.TypesInfo, call)
	if fn == nil {
		return nil
	}
	return fn.Origin()
}

// returnedOrigin is what every return of fd gives as its first result,
// leaving out those that return nil, such as on an error, or unknown when
// they differ.
func (rs *resolver) returnedOrigin(fd *ast.FuncDecl) origin {
	defs := definitions(rs.pass, fd.Body)
	var got []origin
	returns(fd.Body, func(ret *ast.ReturnStmt) {
		switch {
		case len(ret.Results) == 0:
			got = append(got, unknown) // named results: not followed
		case !isNil(rs.pass, ret.Results[0]):
			got = append(got, rs.resolve(defs, ret.Results[0], 0))
		}
	})
	if len(got) == 0 {
		return unknown
	}
	for _, o := range got[1:] {
		if o != got[0] {
			return unknown
		}
	}
	return got[0]
}

// isNil reports whether e is the untyped nil.
func isNil(pass *analysis.Pass, e ast.Expr) bool {
	tv, ok := pass.TypesInfo.Types[e]
	return ok && tv.IsNil()
}

// returns calls fn with each return statement of a body, leaving out those
// of function literals.
func returns(body *ast.BlockStmt, fn func(*ast.ReturnStmt)) {
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.ReturnStmt:
			fn(n)
		}
		return true
	})
}

// definitions maps each variable a body declares with a value, and never
// assigns again, to that value: for v, err := f(), the call. The
// declaration scopes every use, so the value is the one each use sees.
// Parameters, named results and variables of enclosing functions are left
// out: their value comes from elsewhere. Writes in function literals count,
// as do range assignments, increments and taking the address.
func definitions(pass *analysis.Pass, body *ast.BlockStmt) map[*types.Var]ast.Expr {
	defs := map[*types.Var]ast.Expr{}
	writes := map[*types.Var]int{}
	record := func(e ast.Expr, value ast.Expr) {
		id, ok := ast.Unparen(e).(*ast.Ident)
		if !ok {
			return
		}
		v, ok := pass.TypesInfo.ObjectOf(id).(*types.Var)
		if !ok {
			return
		}
		writes[v]++
		if value != nil && pass.TypesInfo.Defs[id] == v && v.Pos() >= body.Pos() && v.Pos() <= body.End() {
			defs[v] = value
		}
	}
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.AssignStmt:
			for i, lhs := range n.Lhs {
				var value ast.Expr
				if n.Tok == token.DEFINE {
					value = valueOf(n.Rhs, i, len(n.Lhs))
				}
				record(lhs, value)
			}
		case *ast.ValueSpec:
			for i, name := range n.Names {
				record(name, valueOf(n.Values, i, len(n.Names)))
			}
		case *ast.RangeStmt:
			record(n.Key, nil)
			record(n.Value, nil)
		case *ast.IncDecStmt:
			record(n.X, nil)
		case *ast.UnaryExpr:
			if n.Op == token.AND {
				record(n.X, nil) // its address escapes: anything may change it
			}
		}
		return true
	})
	for v, n := range writes {
		if n != 1 {
			delete(defs, v)
		}
	}
	return defs
}

// valueOf is the expression the i-th of n names is assigned: its own value,
// or, when one call gives them all, the call for the first name.
func valueOf(values []ast.Expr, i, n int) ast.Expr {
	switch {
	case len(values) == n:
		return values[i]
	case len(values) == 1 && i == 0:
		return values[0]
	}
	return nil
}

// isFunc reports whether fn is the package-level function pkg.name.
func isFunc(fn *types.Func, pkg, name string) bool {
	return fn != nil && fn.Pkg() != nil && fn.Pkg().Path() == pkg && fn.Name() == name &&
		fn.Type().(*types.Signature).Recv() == nil
}

// isTestFile reports whether f is test code: a _test.go file, or a file of
// a package of test helpers or fakes, such as orderstest, which a file other
// than a _test.go file imports testing in; production code never does.
func isTestFile(pass *analysis.Pass, f *ast.File) bool {
	if isTestName(pass, f) {
		return true
	}
	for _, other := range pass.Files {
		if isTestName(pass, other) {
			continue
		}
		for _, imp := range other.Imports {
			if imp.Path.Value == `"testing"` {
				return true
			}
		}
	}
	return false
}

func isTestName(pass *analysis.Pass, f *ast.File) bool {
	return strings.HasSuffix(pass.Fset.File(f.Pos()).Name(), "_test.go")
}
