package main

import (
	"fmt"
	"go/ast"
	"go/token"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// SQLFile is SQL a module or shared code owns: a migration, a query file,
// or a query written in Go.
type SQLFile struct {
	// Name is where the SQL is, for findings, such as
	// internal/orders/queries/orders.sql or internal/orders/store.go:12.
	Name string
	// Module is the module that owns it, or "" for shared SQL.
	Module string
	Text   string
}

var (
	lineComment  = regexp.MustCompile(`--[^\n]*`)
	blockComment = regexp.MustCompile(`(?s)/\*.*?\*/`)
	// qualified matches schema.name, the schema quoted or not, not preceded
	// by a word or a dot, so acme.orders.created (a topic name) is not
	// orders.created.
	qualified = regexp.MustCompile(`(?:^|[^\w."])"?([a-z_][a-z0-9_]*)"?\.[a-z_"]`)
)

// SQL reports SQL that names another module's schema. Each module has a
// Postgres schema of its name: a module's SQL may use its own schema and
// any that is not a module's, and shared SQL no module's.
func (l Layout) SQL(files []SQLFile) []string {
	var out []string
	for _, f := range files {
		text := blockComment.ReplaceAllString(lineComment.ReplaceAllString(f.Text, ""), "")
		seen := map[string]bool{}
		for _, m := range qualified.FindAllStringSubmatch(text, -1) {
			schema := m[1]
			if schema == f.Module || !slices.Contains(l.Modules, schema) || seen[schema] {
				continue
			}
			seen[schema] = true
			if f.Module == "" {
				out = append(out, fmt.Sprintf("%s: shared SQL names module %s's schema", f.Name, schema))
				continue
			}
			out = append(out, fmt.Sprintf("%s: module %s's SQL names module %s's schema: read it through %s.Port, or keep a copy from its events",
				f.Name, f.Module, schema, schema))
		}
	}
	return out
}

// sqlKeyword tells a Go string literal that holds SQL from one that does
// not, such as an error message or a subscriber name.
var sqlKeyword = regexp.MustCompile(`(?i)\b(select|insert\s+into|update|delete\s+from|from|join)\s`)

// LooksLikeSQL reports whether a Go string literal is a query that may name
// a schema.
func LooksLikeSQL(s string) bool { return sqlKeyword.MatchString(s) && strings.Contains(s, ".") }

// goSQL returns the string literals of a Go file that hold SQL.
func goSQL(fset *token.FileSet, name, module string, f *ast.File) []SQLFile {
	var out []SQLFile
	ast.Inspect(f, func(n ast.Node) bool {
		if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING {
			if s, err := strconv.Unquote(lit.Value); err == nil && LooksLikeSQL(s) {
				out = append(out, SQLFile{Name: fmt.Sprintf("%s:%d", name, fset.Position(lit.Pos()).Line), Module: module, Text: s})
			}
		}
		return true
	})
	return out
}
