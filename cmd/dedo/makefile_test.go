package main

import (
	"os"
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
	for _, want := range []string{"\napi-run:\n\tgo run .", "\napi-tools:\n\tgo tool dedo tools", "\ncheck: api-tools modules\n", "\ngenerate:\n", "\napi-check: check\n", "\napi-generate: generate\n", "## api-run: serve the API"} {
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
	if strings.Count(s, "\napi-check:") != 1 || strings.Contains(s, "api-check: check") || !strings.Contains(s, "\napi-check: tools modules\n") {
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

// A service that already shares its docs keeps its own docs-tunnel, and
// dedo's becomes api-docs-tunnel; docs keeps its short name.
func TestMakefileAPIRenamesOwnDocsTunnel(t *testing.T) {
	got, names := makefileAPI(asset("templates/Makefile.api"), []byte("docs-tunnel:\n\tbash share.sh\n"))
	s := string(got)
	if names["docs-tunnel"] != "api-docs-tunnel" || names["docs"] != "docs" {
		t.Errorf("names = %v", names)
	}
	for _, want := range []string{"\napi-docs-tunnel:\n\tgo tool dedo docs -tunnel", "\ndocs:\n\tgo tool dedo docs -addr", "## api-docs-tunnel: share"} {
		if !strings.Contains(s, want) {
			t.Errorf("Makefile.api lacks %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, "\ndocs-tunnel:") {
		t.Errorf("Makefile.api still defines docs-tunnel:\n%s", s)
	}
}

// A rule that only adds prerequisites or sets a target-specific variable
// extends dedo's target of that name, so it keeps its short name.
func TestMakefileAPIKeepsExtendedTargets(t *testing.T) {
	mk := []byte("run: export DEV_AUTH ?= true\n\n" +
		"## sqlc-check: diff\nsqlc-check:\n\tgo tool sqlc diff\n\n" +
		"check: sqlc-check\n\n" +
		"test: test-db\ntest: export GOFLAGS += -race\n\n" +
		"tools: ; echo mine\n")
	got, names := makefileAPI(asset("templates/Makefile.api"), mk)
	for _, t2 := range []string{"run", "check", "test"} {
		if names[t2] != t2 {
			t.Errorf("%s renamed to %s", t2, names[t2])
		}
	}
	if names["tools"] != "api-tools" {
		t.Errorf("tools with an inline recipe kept its name: %v", names)
	}
	if s := string(got); !strings.Contains(s, "\ncheck: api-tools modules\n") || !strings.Contains(s, "\ntest:\n\tgo test") {
		t.Errorf("Makefile.api:\n%s", s)
	}
}

// docs-static writes the one-file reference into .tools/, which git ignores.
func TestMakefileAPIDocsStatic(t *testing.T) {
	got, names := makefileAPI(asset("templates/Makefile.api"), nil)
	s := string(got)
	if names["docs-static"] != "docs-static" ||
		!strings.Contains(s, "\nDOCS_STATIC ?= .tools/docs.html\n") ||
		!strings.Contains(s, "\ndocs-static:\n\tgo tool dedo docs -static $(DOCS_STATIC)\n") {
		t.Errorf("names = %v, Makefile.api:\n%s", names, s)
	}
}

// make check runs dedo modules, under the name the service's Makefile
// leaves it.
func TestMakefileAPIChecksModules(t *testing.T) {
	got, names := makefileAPI(asset("templates/Makefile.api"), nil)
	if s := string(got); names["modules"] != "modules" || !strings.Contains(s, "\ncheck: tools modules\n") ||
		!strings.Contains(s, "\nmodules:\n\tgo tool dedo modules\n") {
		t.Errorf("names = %v, Makefile.api:\n%s", names, s)
	}
	got, names = makefileAPI(asset("templates/Makefile.api"), []byte("modules:\n\tgo run ./scripts/modulecheck\n"))
	if s := string(got); names["modules"] != "api-modules" || !strings.Contains(s, "\ncheck: tools api-modules\n") ||
		!strings.Contains(s, "\napi-modules:\n\tgo tool dedo modules\n") {
		t.Errorf("names = %v, Makefile.api:\n%s", names, s)
	}
}

// CI runs every check make check does: dedo modules, buf breaking per
// module, and, in a repository with a backend for frontend, the BFF's tests
// and a staleness check of bff/buf.gen.yaml's output. guideline.md's CI
// table lists each step of the Jenkins stage.
func TestCITemplatesRunEveryCheck(t *testing.T) {
	mk := string(asset("templates/Makefile.api"))
	jenkins := string(asset("templates/Jenkinsfile.api-stage"))
	guideline, err := os.ReadFile("../../guidance/guideline.md")
	if err != nil {
		t.Fatal(err)
	}
	contains(t, "Makefile.api", mk,
		"\ncheck: tools modules\n",
		"go tool dedo breaking -against $(API_BASE)",
		"buf generate --template bff/buf.gen.yaml",
		"git status --porcelain -- $(BFF_GEN)",
		"cd bff && go vet ./... && go test ./...",
	)
	contains(t, "Jenkinsfile.api-stage", jenkins,
		"sh 'go tool dedo modules'",
		"sh 'go tool dedo breaking -against main'",
		"buf generate --template bff/buf.gen.yaml",
		"cd bff && go vet ./... && go test ./...",
	)
	for name, src := range map[string]string{"Makefile.api": mk, "Jenkinsfile.api-stage": jenkins} {
		if strings.Contains(src, "buf breaking --against") {
			t.Errorf("%s compares the whole workspace, which fails when a PR adds a module:\n%s", name, src)
		}
	}
	for _, line := range strings.Split(jenkins, "\n") {
		line = strings.TrimSpace(line)
		cmd, ok := strings.CutPrefix(line, "sh ")
		if !ok {
			continue
		}
		cmd = strings.Trim(cmd, `'"`)
		if cmd == "git fetch --no-tags origin +refs/heads/main:refs/heads/main" {
			continue
		}
		if i := strings.Index(cmd, " --error-format"); i > 0 {
			cmd = cmd[:i]
		}
		if !strings.Contains(string(guideline), "`"+cmd+"`") {
			t.Errorf("guideline.md's CI table does not list %q", cmd)
		}
	}
}
