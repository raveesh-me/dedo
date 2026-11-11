package main

import (
	"bytes"
	"errors"
	"hash/fnv"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"

	"github.com/raveesh-me/dedo"
)

const (
	// kindPortBase and kindPorts are the host ports a new service's kind
	// cluster may get: one picked from its name, so the clusters of
	// different services rarely clash, beside make run's 8080.
	kindPortBase = 8100
	kindPorts    = 900
	// kindGoVersion is the build image's Go when go.mod names none.
	kindGoVersion = "1.26"
	// kindMarker is in the Makefile.kind dedo writes, and in no other.
	kindMarker = "dedo init writes it and dedo upgrade keeps it current"
)

// kindTargets are the targets Makefile.kind defines.
var kindTargets = []string{"kind-up", "kind-down", "kind-cluster", "kind-build", "kind-load", "kind-apply",
	"kind-update", "kind-status", "kind-logs", "kind-smoke", "kind-claude"}

// kindUses are the files of zarf/ that Makefile.kind builds and deploys
// from.
var kindUses = []string{"zarf/docker/dockerfile.service", kindConfig, "zarf/k8s/dev/kustomization.yaml"}

// kindData fills the zarf/ and Makefile.kind templates.
type kindData struct {
	Name      string // the cluster, namespace, deployment and image
	Port      int    // the host port of the cluster's NodePort
	GoVersion string // the build image's Go, major.minor
}

// kind scaffolds zarf/, the image and manifests that run the service in a
// local kind cluster, and Makefile.kind, which builds and deploys it there.
// zarf/ is the repository's once written; Makefile.kind stays dedo's.
func (in *initer) kind() error {
	if in.p.service == nil || (in.p.rootPkg != "" && in.p.rootPkg != "main") {
		in.say("zarf/, Makefile.kind: skipped until the repository root serves a service (package main)")
		return nil
	}
	name := k8sName(serviceName(in.p.module))
	data := kindData{Name: name, Port: kindPortFor(name), GoVersion: goVersion(in.path("go.mod"))}
	if err := in.zarf(data); err != nil {
		return err
	}
	if err := in.makefileKind(data); err != nil {
		return err
	}
	return in.ensureLine(".gitignore", ".build/")
}

// zarf writes zarf/ from the templates, unless the repository has one.
func (in *initer) zarf(data kindData) error {
	if _, err := os.Stat(in.path("zarf")); err == nil {
		in.say("zarf/: exists; left as it is")
		return nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return fs.WalkDir(dedo.Files, "templates/zarf", func(name string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := renderText(name, data)
		if err != nil {
			return err
		}
		rel := strings.TrimSuffix(strings.TrimPrefix(name, "templates/"), ".tmpl")
		if err := os.MkdirAll(filepath.Dir(in.path(rel)), 0o755); err != nil {
			return err
		}
		return in.writeFile(rel, b, false)
	})
}

// makefileKind writes Makefile.kind and includes it from the Makefile,
// unless the repository has a Makefile.kind of its own or its Makefile
// defines a target Makefile.kind would.
func (in *initer) makefileKind(data kindData) error {
	if old, err := os.ReadFile(in.path("Makefile.kind")); err == nil && !bytes.Contains(old, []byte(kindMarker)) {
		in.say("Makefile.kind: the repository's own; left as it is")
		return nil
	}
	for _, f := range kindUses {
		if _, err := os.Stat(in.path(f)); err != nil {
			in.say("Makefile.kind: skipped; it needs %s, which zarf/ lacks", f)
			return nil
		}
	}
	mk, err := os.ReadFile(in.path("Makefile"))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	own := ownTargets(mk)
	for _, t := range kindTargets {
		if own[t] {
			in.say("Makefile.kind: skipped; the Makefile already has %s", t)
			return nil
		}
	}
	b, err := renderText("templates/Makefile.kind.tmpl", data)
	if err != nil {
		return err
	}
	if err := in.writeFile("Makefile.kind", b, true); err != nil {
		return err
	}
	return in.ensureLine("Makefile", "include Makefile.kind")
}

// renderText fills a bundled text template.
func renderText(name string, data any) ([]byte, error) {
	t, err := template.New(path.Base(name)).Option("missingkey=error").Parse(string(asset(name)))
	if err != nil {
		return nil, err
	}
	var b bytes.Buffer
	if err := t.Execute(&b, data); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

var notDNSLabel = regexp.MustCompile(`[^a-z0-9-]+`)

// k8sName makes name a Kubernetes namespace and kind cluster name: a DNS
// label of at most 63 characters.
func k8sName(name string) string {
	n := strings.Trim(notDNSLabel.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if len(n) > 63 {
		n = strings.TrimRight(n[:63], "-")
	}
	if n == "" {
		return "service"
	}
	return n
}

// kindPortFor is the host port dedo init gives the kind cluster of the
// service called name: the same every time, and zarf/k8s/dev/kind-config.yaml
// holds it from then on.
func kindPortFor(name string) int {
	h := fnv.New32a()
	h.Write([]byte(name))
	return kindPortBase + int(h.Sum32()%kindPorts)
}

var goDirective = regexp.MustCompile(`(?m)^go\s+(\d+\.\d+)`)

// goVersion is the major.minor Go of a go.mod, for the build image's tag.
func goVersion(gomod string) string {
	b, err := os.ReadFile(gomod)
	if err != nil {
		return kindGoVersion
	}
	if m := goDirective.FindSubmatch(b); m != nil {
		return string(m[1])
	}
	return kindGoVersion
}
