package gorules

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"
)

// handler is a method that serves one or more RPCs.
type handler struct {
	decl *ast.FuncDecl
	rpcs []rpc
}

// contracts are the contracts of the packages a pass can see, by package
// path.
type contracts map[string]contract

// findHandlers returns the methods of the package, outside tests, that
// serve a unary RPC of a dedo service: a method named after the RPC that
// takes a context and the RPC's request message and returns its response
// message and an error, as connect-go's generated handler interfaces
// declare them.
func findHandlers(pass *analysis.Pass, cs contracts) []handler {
	var out []handler
	for _, f := range pass.Files {
		if isTestFile(pass, f) {
			continue
		}
		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Recv == nil || fd.Body == nil {
				continue
			}
			fn, ok := pass.TypesInfo.Defs[fd.Name].(*types.Func)
			if !ok {
				continue
			}
			if rpcs := servedRPCs(fn, cs); len(rpcs) > 0 {
				out = append(out, handler{decl: fd, rpcs: rpcs})
			}
		}
	}
	return out
}

// servedRPCs returns the RPCs a method's name and signature match.
func servedRPCs(fn *types.Func, cs contracts) []rpc {
	sig := fn.Type().(*types.Signature)
	if sig.Params().Len() != 2 || sig.Results().Len() != 2 ||
		!isNamed(sig.Params().At(0).Type(), "context", "Context") ||
		!isNamed(sig.Results().At(1).Type(), "", "error") {
		return nil
	}
	in, inOK := messageName(sig.Params().At(1).Type(), cs)
	out, outOK := messageName(sig.Results().At(0).Type(), cs)
	if !inOK || !outOK {
		return nil
	}
	var rpcs []rpc
	for _, c := range cs {
		for _, r := range c.Methods {
			if r.GoName == fn.Name() && r.Input == in && r.Output == out {
				rpcs = append(rpcs, r)
			}
		}
	}
	return rpcs
}

// messageName returns the full name of the message a *T stands for, from
// the contract of T's package.
func messageName(t types.Type, cs contracts) (string, bool) {
	ptr, ok := t.(*types.Pointer)
	if !ok {
		return "", false
	}
	named, ok := ptr.Elem().(*types.Named)
	if !ok || named.Obj().Pkg() == nil {
		return "", false
	}
	return cs[named.Obj().Pkg().Path()].messageNamed(named.Obj().Name())
}

// isNamed reports whether t is the named type pkg.name ("" for a
// predeclared one).
func isNamed(t types.Type, pkg, name string) bool {
	named, ok := types.Unalias(t).(*types.Named)
	if !ok || named.Obj().Name() != name {
		return false
	}
	if named.Obj().Pkg() == nil {
		return pkg == ""
	}
	return named.Obj().Pkg().Path() == pkg
}
