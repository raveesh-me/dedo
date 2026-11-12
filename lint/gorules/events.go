package gorules

import (
	"go/ast"
	"go/token"
	"go/types"
	"slices"
	"strings"

	"golang.org/x/tools/go/analysis"
)

const eventsPkg = "github.com/raveesh-me/dedo/events"

// EventDeclared checks that a constant event type a handler publishes
// through events.Publisher is one its RPC declares in dedo.v1.method's
// emits.
var EventDeclared = &analysis.Analyzer{
	Name:      analyzerName(IDEventDeclared),
	Doc:       "checks that the constant event types a handler publishes are in its RPC's emits (" + IDEventDeclared + ")",
	URL:       DocsURL(IDEventDeclared),
	Run:       runEventDeclared,
	FactTypes: []analysis.Fact{new(eventContract), new(eventParams)},
}

// eventContract is a generated package's contract, for this analyzer.
type eventContract struct{ C contract }

func (*eventContract) AFact()         {}
func (*eventContract) String() string { return "contract" }

// eventParams marks a function that publishes parameters of its own as the
// event type unchanged, such as publishCreated(ctx, eventType, name).
type eventParams struct{ Params []int }

func (*eventParams) AFact()           {}
func (f *eventParams) String() string { return "event params " + ints(f.Params) }

var eventFlow = flow{
	direct: publishedTypes,
	fact: func(pass *analysis.Pass, fn *types.Func) []int {
		var f eventParams
		pass.ImportObjectFact(fn, &f)
		return f.Params
	},
	export: func(pass *analysis.Pass, fn *types.Func, params []int) {
		pass.ExportObjectFact(fn, &eventParams{Params: params})
	},
}

// publishedTypes returns the event type of a call to
// (*events.Publisher).Publish.
func publishedTypes(pass *analysis.Pass, n ast.Node) []ast.Expr {
	call, ok := n.(*ast.CallExpr)
	if !ok || len(call.Args) < 2 {
		return nil
	}
	fn := staticCallee(pass, call)
	if fn == nil || fn.Name() != "Publish" || fn.Pkg() == nil || fn.Pkg().Path() != eventsPkg {
		return nil
	}
	recv := fn.Type().(*types.Signature).Recv()
	if recv == nil {
		return nil
	}
	t := recv.Type()
	if ptr, ok := t.(*types.Pointer); ok {
		t = ptr.Elem()
	}
	if !isNamed(t, eventsPkg, "Publisher") {
		return nil
	}
	return []ast.Expr{call.Args[1]}
}

func runEventDeclared(pass *analysis.Pass) (any, error) {
	if c, ok := readContract(pass); ok {
		pass.ExportPackageFact(&eventContract{C: c})
	}
	local := eventFlow.summarize(pass)
	cs := contracts{}
	for _, f := range pass.AllPackageFacts() {
		if c, ok := f.Fact.(*eventContract); ok {
			cs[f.Package.Path()] = c.C
		}
	}
	r := newReporter(pass)
	for _, h := range findHandlers(pass, cs) {
		ast.Inspect(h.decl.Body, func(n ast.Node) bool {
			for _, e := range eventFlow.sinkArgs(pass, local, n) {
				if eventType, ok := constString(pass, e); ok {
					reportEvent(r, h, e.Pos(), eventType)
				}
			}
			return true
		})
	}
	return nil, nil
}

// reportEvent reports an event type that one of the handler's RPCs does
// not declare in emits.
func reportEvent(r *reporter, h handler, pos token.Pos, eventType string) {
	var missing []string
	for _, rpc := range h.rpcs {
		if slices.Contains(rpc.Emits, eventType) {
			return // one of the RPCs the method may serve declares it
		}
		missing = append(missing, rpc.Procedure)
	}
	r.report(pos, IDEventDeclared, DocsURL(IDEventDeclared),
		"%s publishes the event type %s, which %s does not declare in emits, so events.Publisher refuses it: add it to the RPC's dedo.v1.method emits",
		h.decl.Name.Name, eventType, strings.Join(missing, " or "))
}
