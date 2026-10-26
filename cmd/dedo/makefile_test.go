package main

import (
	"strings"
	"testing"
)

// A target the service's Makefile already has is named api-<name> in
// Makefile.api, so neither shadows the other; the rest keep short names,
// and the earlier api-check and api-generate names still work.
func TestMakefileAPIRenamesCollisions(t *testing.T) {
	tmpl := asset("templates/Makefile.api")
	got, names := makefileAPI(tmpl, nil)
	if string(got) != string(tmpl) {
		t.Errorf("without a Makefile, Makefile.api changed:\n%s", got)
	}
	if names["check"] != "check" || names["run"] != "run" {
		t.Errorf("names = %v", names)
	}

	mk := []byte("VERSION := 1\nrun: build\n\t./bin/app\n\ntools:\n\techo mine\n")
	got, names = makefileAPI(tmpl, mk)
	s := string(got)
	for _, want := range []string{"\napi-run:\n\tgo run .", "\napi-tools:\n\tgo tool dedo tools", "\ncheck: api-tools\n", "\ngenerate:\n", "\napi-check: check\n", "\napi-generate: generate\n", "## api-run: serve the API"} {
		if !strings.Contains(s, want) {
			t.Errorf("Makefile.api lacks %q:\n%s", want, s)
		}
	}
	for _, unwanted := range []string{"\nrun:", "\ntools:", "api-run: run", "api-tools: tools"} {
		if strings.Contains(s, unwanted) {
			t.Errorf("Makefile.api has %q:\n%s", unwanted, s)
		}
	}
	if names["run"] != "api-run" || names["tools"] != "api-tools" || names["check"] != "check" {
		t.Errorf("names = %v", names)
	}
	// A variable named like a target is not a target.
	if _, names := makefileAPI(tmpl, []byte("run := yes\n")); names["run"] != "run" {
		t.Errorf("a variable counted as a target: %v", names)
	}
}

// When the service already has check, the renamed api-check is the target,
// and the alias of that name goes.
func TestMakefileAPIRenamedCheckReplacesAlias(t *testing.T) {
	got, _ := makefileAPI(asset("templates/Makefile.api"), []byte("check:\n\tgo vet ./...\n"))
	s := string(got)
	if strings.Count(s, "\napi-check:") != 1 || strings.Contains(s, "api-check: check") || !strings.Contains(s, "\napi-check: tools\n") {
		t.Errorf("Makefile.api:\n%s", s)
	}
	if !strings.Contains(s, ".PHONY: api-generate") || strings.Contains(s, ".PHONY: api-check api-generate") {
		t.Errorf("alias .PHONY line:\n%s", s)
	}
}

// dedo init ends with make commands only: offline, it lists generation for
// later and says make generate, then make run; with a Makefile that already
// has run, it says make api-run.
func TestInitNextStepsUseMake(t *testing.T) {
	dir := repo(t, map[string]string{"go.mod": "module example.com/shop\n"})
	out := initOffline(t, dir)
	for _, want := range []string{"\n    buf generate\n", "\n    go mod tidy\n", "1. make generate", "2. make run", "after changing a .proto: make generate; before committing: make check"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "go run .") || strings.Contains(out, "make api-check api-generate") {
		t.Errorf("output still names commands other than dedo and make:\n%s", out)
	}

	dir = repo(t, map[string]string{"go.mod": "module example.com/shop\n", "Makefile": "run:\n\t./serve\n"})
	out = initOffline(t, dir)
	if !strings.Contains(out, "2. make api-run") || !strings.Contains(out, "Makefile: already has run, so Makefile.api names dedo's api-run") {
		t.Errorf("output:\n%s", out)
	}
	if mk := read(t, dir, "Makefile.api"); !strings.Contains(mk, "\napi-run:\n\tgo run .") {
		t.Errorf("Makefile.api:\n%s", mk)
	}
}
