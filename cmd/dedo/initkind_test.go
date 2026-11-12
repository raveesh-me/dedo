package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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
	"zarf/k8s/dev/otel-collector/kustomization.yaml",
	"zarf/k8s/dev/otel-collector/otel-collector.yaml",
	"zarf/k8s/dev/otel-collector/config.yaml",
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
	for _, want := range []string{"KIND_CLUSTER   ?= time-counter", "deployment/time-counter", "go tool dedo local-mcp-claude", "logs deployment/otel-collector", kindMarker} {
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

// The dev overlay deploys an OpenTelemetry Collector that logs what it
// receives, and points the service's standard OTEL_* variables at it.
func TestInitScaffoldsDevTelemetrySink(t *testing.T) {
	dir := repo(t, map[string]string{
		"go.mod": "module github.com/acme/time-counter\n\ngo 1.26\n",
		"proto/acme/time_counter/v1/time_counter.proto": counterProto,
	})
	initOffline(t, dir)

	var config struct {
		Receivers struct {
			OTLP struct {
				Protocols map[string]struct{ Endpoint string }
			}
		}
		Exporters struct {
			Debug struct{ Verbosity string }
		}
		Service struct {
			Pipelines map[string]struct{ Receivers, Exporters []string }
		}
	}
	if err := yaml.Unmarshal([]byte(read(t, dir, "zarf/k8s/dev/otel-collector/config.yaml")), &config); err != nil {
		t.Fatal(err)
	}
	for protocol, port := range map[string]string{"grpc": ":4317", "http": ":4318"} {
		if got := config.Receivers.OTLP.Protocols[protocol].Endpoint; !strings.HasSuffix(got, port) {
			t.Errorf("otlp %s listens on %q, want port %s", protocol, got, port)
		}
	}
	if config.Exporters.Debug.Verbosity != "basic" {
		t.Errorf("debug verbosity = %q, want basic", config.Exporters.Debug.Verbosity)
	}
	for _, signal := range []string{"traces", "metrics", "logs"} {
		p := config.Service.Pipelines[signal]
		if strings.Join(p.Receivers, ",") != "otlp" || strings.Join(p.Exporters, ",") != "debug" {
			t.Errorf("%s pipeline = %+v, want otlp to debug", signal, p)
		}
	}

	pinned := regexp.MustCompile(`image: docker\.io/otel/opentelemetry-collector@sha256:[0-9a-f]{64} # \d+\.\d+\.\d+\n`)
	if got := read(t, dir, "zarf/k8s/dev/otel-collector/otel-collector.yaml"); !pinned.MatchString(got) {
		t.Errorf("the collector image is not pinned by digest with its version:\n%s", got)
	}
	if !strings.Contains(read(t, dir, "zarf/k8s/dev/kustomization.yaml"), "\n- otel-collector\n") {
		t.Error("the dev overlay does not deploy otel-collector")
	}

	want := map[string]string{
		"OTEL_EXPORTER_OTLP_ENDPOINT": "http://otel-collector:4318",
		"OTEL_SERVICE_NAME":           "time-counter",
		"OTEL_RESOURCE_ATTRIBUTES":    "deployment.environment.name=dev",
	}
	if got := serviceEnv(t, read(t, dir, "zarf/k8s/dev/service/dev-service-patch-deploy.yaml")); !equalEnv(got, want) {
		t.Errorf("the dev patch sets %v, want %v", got, want)
	}

	// Rendered as make kind-apply renders it, when kubectl is here.
	if _, err := exec.LookPath("kubectl"); err != nil {
		t.Skip("kubectl is not installed")
	}
	out, err := exec.Command("kubectl", "kustomize", filepath.Join(dir, "zarf/k8s/dev")).CombinedOutput()
	if err != nil {
		t.Fatalf("kubectl kustomize: %v\n%s", err, out)
	}
	found := map[string]bool{}
	dec := yaml.NewDecoder(strings.NewReader(string(out)))
	for {
		var doc struct {
			Kind     string
			Metadata struct{ Name, Namespace string }
		}
		var node yaml.Node
		if dec.Decode(&node) != nil {
			break
		}
		if err := node.Decode(&doc); err != nil {
			t.Fatal(err)
		}
		if doc.Kind == "Namespace" {
			continue
		}
		if doc.Metadata.Namespace != "time-counter" {
			t.Errorf("%s %s is in namespace %q", doc.Kind, doc.Metadata.Name, doc.Metadata.Namespace)
		}
		name := doc.Metadata.Name
		if doc.Kind == "ConfigMap" && strings.HasPrefix(name, "otel-collector-") {
			name = "otel-collector"
		}
		found[doc.Kind+"/"+name] = true
		if doc.Kind == "Deployment" && name == "time-counter" {
			var b strings.Builder
			if err := yaml.NewEncoder(&b).Encode(&node); err != nil {
				t.Fatal(err)
			}
			if got := serviceEnv(t, b.String()); !equalEnv(got, want) {
				t.Errorf("the rendered service gets %v, want %v", got, want)
			}
		}
	}
	for _, r := range []string{"Deployment/otel-collector", "Service/otel-collector", "ConfigMap/otel-collector", "Deployment/time-counter"} {
		if !found[r] {
			t.Errorf("the dev overlay renders no %s", r)
		}
	}
}

// serviceEnv is the OTEL_* environment of a Deployment's service container.
func serviceEnv(t *testing.T, deployment string) map[string]string {
	t.Helper()
	var d struct {
		Spec struct {
			Template struct {
				Spec struct {
					Containers []struct {
						Name string
						Env  []struct{ Name, Value string }
					}
				}
			}
		}
	}
	if err := yaml.Unmarshal([]byte(deployment), &d); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{}
	for _, c := range d.Spec.Template.Spec.Containers {
		if c.Name != "service" {
			continue
		}
		for _, e := range c.Env {
			if strings.HasPrefix(e.Name, "OTEL_") {
				env[e.Name] = e.Value
			}
		}
	}
	return env
}

func equalEnv(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// dedo upgrade leaves a zarf/ written before the collector as it is, and
// brings dedo's Makefile.kind, with kind-otel, up to date.
func TestUpgradeKeepsZarfWithoutTheCollector(t *testing.T) {
	dir := repo(t, map[string]string{
		"go.mod": "module github.com/acme/time-counter\n\ngo 1.26\n",
		"proto/acme/time_counter/v1/time_counter.proto": counterProto,
	})
	initOffline(t, dir)
	if err := os.RemoveAll(filepath.Join(dir, "zarf/k8s/dev/otel-collector")); err != nil {
		t.Fatal(err)
	}
	old := strings.ReplaceAll(read(t, dir, "Makefile.kind"), "kind-otel", "kind-gone")
	if err := os.WriteFile(filepath.Join(dir, "Makefile.kind"), []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}

	out := initOffline(t, dir)
	if _, err := os.Stat(filepath.Join(dir, "zarf/k8s/dev/otel-collector")); err == nil {
		t.Error("zarf/k8s/dev/otel-collector was added to an existing zarf/")
	}
	if !strings.Contains(out, "zarf/: exists; left as it is") {
		t.Errorf("output does not say zarf/ was left:\n%s", out)
	}
	if !strings.Contains(read(t, dir, "Makefile.kind"), "\nkind-otel:") {
		t.Error("Makefile.kind was not brought up to date with kind-otel")
	}
}
