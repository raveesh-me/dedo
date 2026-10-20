package main

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"
)

// starterNames are the names a starter service takes from the Go module
// path: github.com/acme/time-counter gives package acme.time_counter.v1,
// TimeCounterService, scopes time_counter.item.read and .write, the REST
// root /timeCounter and resource type time-counter.acme.com/Item.
type starterNames struct {
	Package, Service, Team, Root, App, Domain string
	dir, file                                 string
}

var (
	majorVersion = regexp.MustCompile(`^v[0-9]+$`)
	nonWord      = regexp.MustCompile(`[^a-z0-9]+`)
)

func namesFor(module string) starterNames {
	parts := strings.Split(strings.ToLower(module), "/")
	if len(parts) > 1 && majorVersion.MatchString(parts[len(parts)-1]) {
		parts = parts[:len(parts)-1]
	}
	product := words(parts[len(parts)-1])
	org := []string{"example"}
	switch {
	case len(parts) >= 3:
		org = words(parts[1])
	case len(parts) == 2 && strings.Contains(parts[0], "."):
		org = words(strings.Split(parts[0], ".")[0])
	}
	if len(product) == 0 {
		product = []string{"api"}
	}
	if len(org) == 0 {
		org = []string{"example"}
	}
	snake, orgSnake := strings.Join(product, "_"), strings.Join(org, "_")
	camel := ""
	for i, w := range product {
		if i == 0 {
			camel += w
		} else {
			camel += strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return starterNames{
		Package: orgSnake + "." + snake + ".v1",
		Service: strings.ToUpper(camel[:1]) + camel[1:] + "Service",
		Team:    strings.Join(product, "-") + "-team",
		Root:    "/" + camel,
		App:     snake,
		Domain:  strings.Join(product, "-") + "." + strings.Join(org, "-") + ".com",
		dir:     path.Join(orgSnake, snake, "v1"),
		file:    snake + ".proto",
	}
}

// words splits a path element into lowercase words: time-counter → time,
// counter. A word may not start with a digit, so proto names stay valid.
func words(s string) []string {
	var out []string
	for _, w := range nonWord.Split(strings.ToLower(s), -1) {
		w = strings.TrimLeft(w, "0123456789")
		if w != "" {
			out = append(out, w)
		}
	}
	return out
}

// starter writes a service that passes every lint rule, for a repository
// without protos, so the first lint, generate and build all succeed and the
// service shows what a complete contract declares.
func (in *initer) starter() error {
	names := namesFor(in.p.module)
	rel := filepath.Join(in.p.protoRoot, filepath.FromSlash(names.dir), names.file)
	t, err := template.New("starter").Parse(string(asset("templates/starter.proto.tmpl")))
	if err != nil {
		return err
	}
	var b strings.Builder
	if err := t.Execute(&b, names); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(in.path(rel)), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(in.path(rel), []byte(b.String()), 0o644); err != nil {
		return err
	}
	in.say("%s: created %s, a starter service that passes lint; rename Item to your resource", rel, names.Service)
	p, err := analyze(in.p.dir)
	if err != nil {
		return fmt.Errorf("reading the starter service: %w", err)
	}
	in.p = p
	return nil
}
