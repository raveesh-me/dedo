package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// zarfFiles are the files dedo init scaffolds into zarf/.
var zarfFiles = []string{
	"zarf/docker/dockerfile.service",
	"zarf/k8s/base/service/kustomization.yaml",
	"zarf/k8s/base/service/base-service.yaml",
	"zarf/k8s/dev/kustomization.yaml",
	"zarf/k8s/dev/kind-config.yaml",
	"zarf/k8s/dev/dev-namespace-limits.yaml",
	"zarf/k8s/dev/service/kustomization.yaml",
	"zarf/k8s/dev/service/dev-service-patch-deploy.yaml",
	"zarf/k8s/dev/service/dev-service-patch-service.yaml",
}

func TestInitScaffoldsKind(t *testing.T) {
	dir := repo(t, map[string]string{
		"go.mod": "module github.com/acme/Time_Counter\n\ngo 1.25.3\n",
		"proto/acme/time_counter/v1/time_counter.proto": counterProto,
	})
	initOffline(t, dir)

	for _, f := range zarfFiles {
		got := read(t, dir, f)
		if strings.Contains(got, "{{") {
			t.Errorf("%s is not filled in:\n%s", f, got)
		}
		if strings.HasSuffix(f, ".yaml") {
			dec := yaml.NewDecoder(strings.NewReader(got))
			for {
				var v any
				if err := dec.Decode(&v); err != nil {
					if err.Error() != "EOF" {
						t.Errorf("%s: %v", f, err)
					}
					break
				}
			}
		}
	}
	if got := read(t, dir, "zarf/k8s/base/service/base-service.yaml"); !strings.Contains(got, "namespace: time-counter\n") {
		t.Errorf("base-service.yaml does not use the namespace time-counter:\n%s", got)
	}
	if got := read(t, dir, "zarf/docker/dockerfile.service"); !strings.Contains(got, "golang:1.25-alpine") {
		t.Errorf("the image does not build with go.mod's Go:\n%s", got)
	}
	if port, found, err := kindHostPort(dir); err != nil || !found || port != kindPortFor("time-counter") {
		t.Errorf("kindHostPort = %d, %v, %v; want %d", port, found, err, kindPortFor("time-counter"))
	}
	mk := read(t, dir, "Makefile.kind")
	for _, want := range []string{"KIND_CLUSTER   ?= time-counter", "deployment/time-counter", "go tool dedo local-mcp-claude", kindMarker} {
		if !strings.Contains(mk, want) {
			t.Errorf("Makefile.kind lacks %q", want)
		}
	}
	if !strings.Contains(read(t, dir, ".gitignore"), ".build/") {
		t.Error(".gitignore lacks .build/")
	}

	// Run again: nothing changes.
	before := snapshot(t, dir)
	initOffline(t, dir)
	for f, b := range snapshot(t, dir) {
		if before[f] != b && strings.Contains(f, "zarf") {
			t.Errorf("%s changed on a second run", f)
		}
	}
}

// A repository that has zarf/ or a Makefile.kind of its own keeps them.
func TestInitKeepsTheRepositorysKind(t *testing.T) {
	const ownMakefile = "# ours\ndev-up:\n\tkind create cluster\n"
	dir := repo(t, map[string]string{
		"go.mod": "module github.com/acme/time-counter\n\ngo 1.26\n",
		"proto/acme/time_counter/v1/time_counter.proto": counterProto,
		"zarf/k8s/local/kind-config.yaml":               "kind: Cluster\n",
		"Makefile.kind":                                 ownMakefile,
		"Makefile":                                      "include Makefile.kind\n",
	})
	out := initOffline(t, dir)

	if _, err := os.Stat(filepath.Join(dir, "zarf/docker")); err == nil {
		t.Error("zarf/docker was added to the repository's own zarf/")
	}
	if got := read(t, dir, "Makefile.kind"); got != ownMakefile {
		t.Errorf("Makefile.kind = %q", got)
	}
	if got := read(t, dir, "Makefile"); strings.Count(got, "include Makefile.kind") != 1 {
		t.Errorf("Makefile = %q", got)
	}
	for _, want := range []string{"zarf/: exists; left as it is", "Makefile.kind: the repository's own"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

// A Makefile with a kind-* target of its own keeps it, without Makefile.kind.
func TestInitSkipsMakefileKindOnClash(t *testing.T) {
	dir := repo(t, map[string]string{
		"go.mod": "module github.com/acme/time-counter\n\ngo 1.26\n",
		"proto/acme/time_counter/v1/time_counter.proto": counterProto,
		"Makefile": "kind-up:\n\t./up.sh\n",
	})
	out := initOffline(t, dir)
	if _, err := os.Stat(filepath.Join(dir, "Makefile.kind")); err == nil {
		t.Error("Makefile.kind was written")
	}
	if strings.Contains(read(t, dir, "Makefile"), "include Makefile.kind") {
		t.Error("the Makefile includes Makefile.kind")
	}
	if !strings.Contains(out, "Makefile.kind: skipped; the Makefile already has kind-up") {
		t.Errorf("output does not say why:\n%s", out)
	}
}

func TestKindPortFor(t *testing.T) {
	seen := map[int]string{}
	for _, name := range []string{"time-counter", "commerce-bounty", "commerce-taxes", "commerce-pim", "shop", "todo"} {
		p := kindPortFor(name)
		if p < kindPortBase || p >= kindPortBase+kindPorts {
			t.Errorf("kindPortFor(%q) = %d, outside %d-%d", name, p, kindPortBase, kindPortBase+kindPorts-1)
		}
		if p != kindPortFor(name) {
			t.Errorf("kindPortFor(%q) is not stable", name)
		}
		if other, ok := seen[p]; ok {
			t.Errorf("%s and %s both get %d", name, other, p)
		}
		seen[p] = name
	}
}

func TestK8sName(t *testing.T) {
	tests := map[string]string{
		"time-counter":          "time-counter",
		"Time_Counter":          "time-counter",
		"commerce.bounty":       "commerce-bounty",
		"--x--":                 "x",
		"___":                   "service",
		strings.Repeat("a", 70): strings.Repeat("a", 63),
	}
	for in, want := range tests {
		if got := k8sName(in); got != want {
			t.Errorf("k8sName(%q) = %q, want %q", in, got, want)
		}
	}
}

// A zarf/ of another layout gets no Makefile.kind, whose recipes need files
// it lacks.
func TestInitSkipsMakefileKindWithoutItsFiles(t *testing.T) {
	dir := repo(t, map[string]string{
		"go.mod": "module github.com/acme/time-counter\n\ngo 1.26\n",
		"proto/acme/time_counter/v1/time_counter.proto": counterProto,
		"zarf/k8s/local/kind-config.yaml":               "kind: Cluster\n",
	})
	out := initOffline(t, dir)
	if _, err := os.Stat(filepath.Join(dir, "Makefile.kind")); err == nil {
		t.Error("Makefile.kind was written")
	}
	if !strings.Contains(out, "Makefile.kind: skipped; it needs zarf/docker/dockerfile.service") {
		t.Errorf("output does not say why:\n%s", out)
	}
}
