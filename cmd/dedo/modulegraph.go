package main

import (
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"
)

// Package is a Go package of the repository and what it imports, outside
// its tests.
type Package struct {
	Path    string
	Imports []string
}

// Graph is the modules' dependencies: Edges[a][b] lists the imports that
// make module a depend on module b, as "importer -> imported".
type Graph struct {
	Modules []string
	Edges   map[string]map[string][]string
}

// Build makes the module graph of pkgs, and returns the findings about
// shared packages that import a module. Only non-test imports count: tests
// may use the composition root's helpers and other modules' fakes; the
// rules are about what ships.
func (l Layout) Build(pkgs []Package) (Graph, []string) {
	g := Graph{Modules: slices.Clone(l.Modules), Edges: map[string]map[string][]string{}}
	var findings []string
	for _, p := range pkgs {
		kind, from := l.Classify(p.Path)
		for _, imp := range p.Imports {
			ikind, to := l.Classify(imp)
			if ikind != KindModule {
				continue
			}
			switch kind {
			case KindShared:
				findings = append(findings, fmt.Sprintf("%s is shared but imports module %s (%s): shared code imports no module", l.rel(p.Path), to, l.rel(imp)))
			case KindModule:
				if from != to {
					g.add(from, to, l.rel(p.Path)+" -> "+l.rel(imp))
				}
			}
		}
	}
	return g, findings
}

// rel is an import path relative to the Go module.
func (l Layout) rel(p string) string {
	if r, ok := strings.CutPrefix(p, l.Module+"/"); ok {
		return r
	}
	return p
}

func (g Graph) add(from, to, why string) {
	if g.Edges[from] == nil {
		g.Edges[from] = map[string][]string{}
	}
	if !slices.Contains(g.Edges[from][to], why) {
		g.Edges[from][to] = append(g.Edges[from][to], why)
	}
}

// Cycles returns every strongly connected component of more than one
// module, each with the imports that close it.
func (g Graph) Cycles() []string {
	var out []string
	for _, scc := range g.sccs() {
		if len(scc) < 2 {
			continue
		}
		sort.Strings(scc)
		var why []string
		for _, a := range scc {
			for _, b := range scc {
				for _, w := range g.Edges[a][b] {
					why = append(why, fmt.Sprintf("    %s -> %s: %s", a, b, w))
				}
			}
		}
		sort.Strings(why)
		out = append(out, fmt.Sprintf("modules %s depend on each other: make them one module, or move what they share to a third\n%s",
			strings.Join(scc, ", "), strings.Join(why, "\n")))
	}
	sort.Strings(out)
	return out
}

// sccs returns the graph's strongly connected components (Tarjan).
func (g Graph) sccs() [][]string {
	index, low := map[string]int{}, map[string]int{}
	onStack := map[string]bool{}
	var stack []string
	var out [][]string
	next := 0
	var visit func(v string)
	visit = func(v string) {
		index[v], low[v] = next, next
		next++
		stack = append(stack, v)
		onStack[v] = true
		for _, w := range slices.Sorted(maps.Keys(g.Edges[v])) {
			if _, seen := index[w]; !seen {
				visit(w)
				low[v] = min(low[v], low[w])
			} else if onStack[w] {
				low[v] = min(low[v], index[w])
			}
		}
		if low[v] == index[v] {
			var scc []string
			for {
				w := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				onStack[w] = false
				scc = append(scc, w)
				if w == v {
					break
				}
			}
			out = append(out, scc)
		}
	}
	for _, v := range g.Modules {
		if _, seen := index[v]; !seen {
			visit(v)
		}
	}
	return out
}

// String lists each module and the modules it depends on, the modules
// with fewest dependencies first.
func (g Graph) String() string {
	order := slices.Clone(g.Modules)
	depth := map[string]int{}
	var deep func(m string, seen map[string]bool) int
	deep = func(m string, seen map[string]bool) int {
		if d, ok := depth[m]; ok {
			return d
		}
		if seen[m] {
			return 0
		}
		seen[m] = true
		d := 0
		for to := range g.Edges[m] {
			d = max(d, deep(to, seen)+1)
		}
		depth[m] = d
		return d
	}
	width := 0
	for _, m := range order {
		deep(m, map[string]bool{})
		width = max(width, len(m))
	}
	sort.SliceStable(order, func(i, j int) bool {
		if depth[order[i]] != depth[order[j]] {
			return depth[order[i]] < depth[order[j]]
		}
		return order[i] < order[j]
	})
	var b strings.Builder
	for _, m := range order {
		deps := slices.Sorted(maps.Keys(g.Edges[m]))
		if len(deps) == 0 {
			fmt.Fprintf(&b, "  %-*s  (nothing)\n", width, m)
			continue
		}
		fmt.Fprintf(&b, "  %-*s  -> %s\n", width, m, strings.Join(deps, ", "))
	}
	return b.String()
}
