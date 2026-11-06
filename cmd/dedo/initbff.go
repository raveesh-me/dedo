package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/semver"
	"gopkg.in/yaml.v3"
)

// bffDir is where dedo init -bff puts the backend for frontend: a Go module
// nested in the repository, whose buf module is bffDir/proto.
const bffDir = "bff"

// frontendDir is where the frontend the BFF serves conventionally lives.
const frontendDir = "frontend"

// bffNames are the names dedo init -bff fills its templates with, taken
// from the backend: a BFF package <context>.web.v1 in the backend's
// context, its owner team, REST root, read scope and resource domain.
type bffNames struct {
	Module     string // the backend's Go module: example.com/shop
	Go         string // the backend's go directive
	DedoModule string // github.com/raveesh-me/dedo
	Dedo       string // the dedo release bff/go.mod requires
	Connect    string // the connect-go release bff/go.mod requires

	// Starter is set when bff/proto has no protos yet, so dedo init -bff
	// writes the starter service, Package's Service in ProtoDir.
	Starter  bool
	Package  string // shop.web.v1
	Service  string // WebService
	ProtoDir string // shop/web/v1, below bff/proto
	Team     string // the owner team
	Root     string // the REST root before /web/v1
	Scope    string // a scope the backend's reads declare
	Domain   string // the resource type domain

	// Services are the BFF's services, which main.go registers: the
	// starter, or those bff/proto declares already.
	Services []bffService
	// ProtoDirs are the directories of the BFF's protos below bff/proto,
	// which managed mode gives the BFF's Go prefix when the backend's
	// module has no name.
	ProtoDirs []string

	// Backend is the backend's first service, whose client the starter
	// handler builds, and BackendField the handler's field holding it.
	Backend      *service
	BackendField string
	// BackendName is the BSR name of the backend's buf module, if it has
	// one, and BackendGo the Go prefix of its generated code.
	BackendName string
	BackendGo   string
}

var (
	ownerTeamLit = regexp.MustCompile(`owner_team\s*:\s*"([^"]+)"`)
	resourceLit  = regexp.MustCompile(`type\s*:\s*"([a-z0-9.-]+\.[a-z]+)/[A-Z]\w*"`)
	restRootLit  = regexp.MustCompile(`(?:get|post|put|patch|delete)\s*:\s*"((?:/[A-Za-z0-9_-]+)*)/v[0-9]+/`)
	goLine       = regexp.MustCompile(`(?m)^go\s+(\S+)`)
)

// initBFF adds a backend for frontend to a Go service repository that dedo
// init has set up: bff/ with its own go.mod, an unnamed buf module in
// bff/proto with no breaking check, its buf.gen.yaml, a starter service and
// its server. Running it again changes nothing that is already in place.
func initBFF(dir, release string, offline bool, out io.Writer) error {
	p, err := analyze(dir)
	if err != nil {
		return err
	}
	if p.schemaOnly {
		return fmt.Errorf("%s has no go.mod: a backend for frontend sits beside a Go service; run go mod init and dedo init first", dir)
	}
	if _, err := os.Stat(filepath.Join(dir, "buf.yaml")); err != nil {
		return fmt.Errorf("%s has no buf.yaml: run dedo init first, then dedo init -bff", dir)
	}
	in := &initer{p: p, release: release, out: out, offline: offline}
	names, err := in.bffNames()
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "dedo %s: adding a backend for frontend to %s in %s/ (protos in %s/proto/)\n", release, p.module, bffDir, bffDir)
	for _, s := range []struct {
		title string
		run   func(bffNames) error
	}{
		{"adding the BFF's Go module", in.bffModule},
		{"adding the BFF's proto module", in.bffProto},
		{"adding the BFF's code generators", in.bffGenerators},
		{"checking that the backend's code generators leave the BFF out", in.bffRootInputs},
		{"adding the BFF's server", in.bffServer},
		{"leaving the frontend out of the backend's Go module", in.bffFrontend},
	} {
		fmt.Fprintf(out, "\n%s...\n", s.title)
		if err := s.run(names); err != nil {
			return fmt.Errorf("%s: %w", s.title, err)
		}
	}
	failed := in.bffGenerate(offline)
	in.bffNext(names, len(failed) == 0 && !offline)
	if len(failed) > 0 {
		return fmt.Errorf("%d commands failed: %s; fix the cause and run dedo init -bff again", len(failed), strings.Join(failed, "; "))
	}
	return nil
}

// bffNames reads the names of the BFF from the backend.
func (in *initer) bffNames() (bffNames, error) {
	p := in.p
	starter := namesFor(p.module)
	context, _, _, _ := protoModule(starter.Package)
	n := bffNames{
		Module:     p.module,
		Go:         "1.26",
		DedoModule: strings.TrimSuffix(cliPkg, "/cmd/dedo"),
		Dedo:       in.release,
		Connect:    connectRelease,
		Service:    "WebService",
		Team:       starter.Team,
		Root:       starter.Root,
		Scope:      starter.App + ".item.read",
		Domain:     starter.Domain,
		BackendGo:  p.goPrefix,
	}
	if n.BackendGo == "" {
		n.BackendGo = p.module + "/gen/go"
	}
	gomod, err := os.ReadFile(in.path("go.mod"))
	if err != nil {
		return n, err
	}
	if m := goLine.FindSubmatch(gomod); m != nil {
		n.Go = string(m[1])
	}
	if f, err := modfile.Parse("go.mod", gomod, nil); err == nil {
		for _, r := range f.Require {
			switch {
			case r.Mod.Path == n.DedoModule && semver.Compare(r.Mod.Version, n.Dedo) > 0:
				// MVS would choose the backend's newer release anyway.
				n.Dedo = r.Mod.Version
			case r.Mod.Path == "connectrpc.com/connect/v2":
				n.Connect = r.Mod.Version
			}
		}
	}

	all, err := findProtos(p.dir)
	if err != nil {
		return n, err
	}
	// The BFF's own protos, also found while bff/ has no go.mod yet, are
	// never the backend's.
	var protos []string
	for _, rel := range all {
		if r := filepath.ToSlash(rel); r != bffDir && !strings.HasPrefix(r, bffDir+"/") {
			protos = append(protos, rel)
		}
	}
	// The first package names the context; the first of each declaration
	// the others.
	first := map[*regexp.Regexp]*string{ownerTeamLit: &n.Team, resourceLit: &n.Domain, restRootLit: &n.Root}
	var pkgs []string
	for _, rel := range protos {
		src, err := os.ReadFile(in.path(rel))
		if err != nil {
			return n, err
		}
		if pkg := protoPkg.FindSubmatch(src); pkg != nil {
			pkgs = append(pkgs, string(pkg[1]))
		}
		for re, field := range first {
			if m := re.FindSubmatch(src); m != nil {
				*field = string(m[1])
				delete(first, re)
			}
		}
	}
	var modules []string
	contextFound := false
	for _, pkg := range pkgs {
		c, m, _, ok := protoModule(pkg)
		if !ok {
			continue
		}
		if !contextFound {
			context, contextFound = c, true
		}
		if c == context {
			modules = append(modules, m)
		}
	}
	for _, s := range p.scopes {
		if strings.HasSuffix(s, ".read") {
			n.Scope = s
			break
		}
	}
	n.Package = strings.TrimPrefix(context+".web.v1", ".")
	n.ProtoDir = strings.ReplaceAll(n.Package, ".", "/")
	existing, err := bffProtos(in.path(path.Join(bffDir, "proto")))
	if err != nil {
		return n, err
	}
	if len(existing) == 0 {
		// Only the starter takes the package <context>.web.v1, so only
		// it can collide with the backend's.
		for _, m := range modules {
			if m == "web" {
				return n, fmt.Errorf("the backend has a module web in %s already, the package dedo init -bff would give the BFF: write the BFF's proto into %s/proto yourself, then run dedo init -bff again", context, bffDir)
			}
		}
		n.Starter = true
		existing = []bffProto{{Dir: n.ProtoDir, Package: n.Package, Services: []string{n.Service}}}
	}
	n.Services, n.ProtoDirs = bffServices(existing)
	// The backend's first service, never one of the BFF's, which analyze
	// counts while bff/ has no go.mod yet.
	for i, svc := range p.services {
		if !strings.HasPrefix(svc.File, bffDir+"/") {
			n.Backend = &p.services[i]
			n.BackendField = strings.ToLower(svc.Name[:1]) + svc.Name[1:]
			break
		}
	}
	mods, err := bufModules(p.dir)
	if err != nil {
		return n, fmt.Errorf("buf.yaml: %w", err)
	}
	for _, m := range mods {
		if m.Path == p.protoRoot {
			n.BackendName = m.Name
		}
	}
	return n, nil
}

// bffModule writes bff/go.mod: a Go module of its own that requires the
// backend's, from the working tree, for the backend's generated clients.
func (in *initer) bffModule(n bffNames) error {
	if err := os.MkdirAll(in.path(bffDir), 0o755); err != nil {
		return err
	}
	src, err := fillTemplate("templates/bff/go.mod.tmpl", n)
	if err != nil {
		return err
	}
	return in.writeFile(path.Join(bffDir, "go.mod"), src, false)
}

// bffProto lists bff/proto in buf.yaml, with no name and no breaking check,
// and writes a starter service there when it has no protos.
func (in *initer) bffProto(n bffNames) error {
	doc, _, err := loadYAML(in.path("buf.yaml"))
	if err != nil {
		return fmt.Errorf("buf.yaml: %w", err)
	}
	if v := lookup(root(doc), "version"); v == nil || v.Value != "v2" {
		return errors.New("buf.yaml is not version v2: run buf config migrate first")
	}
	if in.bffBufModule(child(root(doc), "modules", yaml.SequenceNode)) {
		if err := saveYAML(in.path("buf.yaml"), doc); err != nil {
			return err
		}
	}
	protoDir := path.Join(bffDir, "proto")
	if !n.Starter {
		var names []string
		for _, s := range n.Services {
			names = append(names, s.Name)
		}
		served := "no service yet"
		if len(names) > 0 {
			served = strings.Join(names, ", ")
		}
		in.say("%s/: has protos already; no starter service (it declares %s)", protoDir, served)
		return nil
	}
	src, err := fillTemplate("templates/bff/web.proto.tmpl", n)
	if err != nil {
		return err
	}
	rel := path.Join(protoDir, n.ProtoDir, "web.proto")
	if err := os.MkdirAll(in.path(path.Dir(rel)), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(in.path(rel), src, 0o644); err != nil {
		return err
	}
	in.say("%s: created %s, a starter AUDIENCE_COMPONENT_INTERNAL service that passes lint; shape its RPCs after the frontend's pages", rel, n.Service)
	return nil
}

// bffBufModule lists bff/proto among buf.yaml's modules, unnamed and with
// a breaking check that ignores it, and reports whether it changed them.
func (in *initer) bffBufModule(modules *yaml.Node) bool {
	protoDir := path.Join(bffDir, "proto")
	entry := findItem(modules, func(n *yaml.Node) bool {
		p := lookup(n, "path")
		return p != nil && path.Clean(p.Value) == protoDir
	})
	changed := entry == nil
	if entry == nil {
		entry = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		set(entry, "path", scalar(protoDir))
		entry.HeadComment = "The backend for frontend's API. It has no name, so buf push\n" +
			"--exclude-unnamed never publishes it, and no breaking check: the\n" +
			"frontend and the BFF build from one commit and change together."
		modules.Content = append(modules.Content, entry)
		in.say("buf.yaml: added the module %s, unnamed", protoDir)
	} else {
		in.say("buf.yaml: lists %s already", protoDir)
	}
	if name := lookup(entry, "name"); name != nil {
		in.say("buf.yaml: WARNING %s is named %s, so buf push publishes it: remove the name (dedo modules reports it)", protoDir, name.Value)
	}
	ignore := child(child(entry, "breaking", yaml.MappingNode), "ignore", yaml.SequenceNode)
	if added := addScalars(ignore, protoDir); len(added) > 0 {
		in.say("buf.yaml: %s has no breaking check", protoDir)
		changed = true
	}
	return changed
}

// bffGenerators writes bff/buf.gen.yaml, which generates the BFF's Go code
// and, once enabled, the frontend's client: nothing published.
func (in *initer) bffGenerators(n bffNames) error {
	src, err := fillTemplate("templates/bff/buf.gen.yaml.tmpl", n)
	if err != nil {
		return err
	}
	return in.writeFile(path.Join(bffDir, "buf.gen.yaml"), src, false)
}

// bffServer writes the BFF's main.go, which registers each of its
// services, and a handler for each. A main.go that exists is the BFF's
// own, so it writes neither; nor does it while bff/proto declares no
// service.
func (in *initer) bffServer(n bffNames) error {
	main := path.Join(bffDir, "main.go")
	if _, err := os.Stat(in.path(main)); err == nil {
		in.say("%s: exists; left as it is, and no handlers written beside it", main)
		return nil
	}
	if len(n.Services) == 0 {
		in.say("%s/proto/ declares no service, so there is no server to write: once it does, run dedo init -bff again", bffDir)
		return nil
	}
	src, err := in.renderGo("templates/bff/main.go.tmpl", n)
	if err != nil {
		return err
	}
	if err := in.writeFile(main, src, false); err != nil {
		return err
	}
	for _, s := range n.Services {
		src, err := in.renderGo("templates/bff/service.go.tmpl", bffHandler{bffService: s, Module: n.Module, Backend: n.Backend, BackendField: n.BackendField})
		if err != nil {
			return err
		}
		if err := in.writeFile(path.Join(bffDir, s.File), src, false); err != nil {
			return err
		}
	}
	return nil
}

// bffFrontend keeps the frontend out of the backend's Go module: go's ./...
// would otherwise walk its node_modules.
func (in *initer) bffFrontend(bffNames) error {
	if info, err := os.Stat(in.path(frontendDir)); err != nil || !info.IsDir() {
		in.say("%s/: none; once the frontend lives there, add `ignore ./%s` to go.mod, or run dedo init -bff again", frontendDir, frontendDir)
		return nil
	}
	b, err := os.ReadFile(in.path("go.mod"))
	if err != nil {
		return err
	}
	f, err := modfile.Parse("go.mod", b, nil)
	if err != nil {
		return err
	}
	for _, i := range f.Ignore {
		if path.Clean(i.Path) == frontendDir {
			in.say("go.mod: ignores ./%s already", frontendDir)
			return nil
		}
	}
	if err := f.AddIgnore("./" + frontendDir); err != nil {
		return err
	}
	f.Cleanup()
	out, err := f.Format()
	if err != nil {
		return err
	}
	if err := os.WriteFile(in.path("go.mod"), out, 0o644); err != nil {
		return err
	}
	in.say("go.mod: added ignore ./%s, so ./... leaves the frontend out", frontendDir)
	return nil
}

// bffGenerate generates the BFF's code, then tidies bff/go.mod, which
// needs it; offline, it prints both. It returns the failed commands.
func (in *initer) bffGenerate(offline bool) []string {
	if offline {
		fmt.Fprintln(in.out, "\ngenerating (skipped: -offline)...")
	} else {
		fmt.Fprintln(in.out, "\ngenerating...")
	}
	if failed := in.run([][]string{{"buf", "generate", "--template", path.Join(bffDir, "buf.gen.yaml")}}, offline); len(failed) > 0 {
		return failed
	}
	sub := &initer{p: &project{dir: in.path(bffDir)}, out: in.out}
	return sub.run([][]string{{"go", "mod", "tidy"}}, offline)
}

// bffNext says what to do next.
func (in *initer) bffNext(n bffNames, generated bool) {
	fmt.Fprintln(in.out, "\nnext:")
	var steps []string
	if !generated {
		steps = append(steps, fmt.Sprintf("once the backend's code is generated (make generate): buf generate --template %s/buf.gen.yaml, then go mod tidy in %s/", bffDir, bffDir))
	}
	for _, s := range n.Services {
		steps = append(steps, fmt.Sprintf("implement %s's RPCs in %s/%s, reading the backend through its clients; until then they answer Unimplemented", s.Name, bffDir, s.File))
	}
	steps = append(steps,
		fmt.Sprintf("build it with its frontend's commit: cd %s && go build -ldflags \"-X main.buildSHA=$(git rev-parse HEAD | cut -c1-12)\" .", bffDir),
		"run it with BACKEND_URL set to the backend's address; the frontend calls /bff/<sha>/ and reloads on BFF_VERSION_RETIRED",
		fmt.Sprintf("for the frontend's client, uncomment protoc-gen-es in %s/buf.gen.yaml", bffDir),
		"dedo modules checks that the BFF stays apart: unnamed, AUDIENCE_COMPONENT_INTERNAL, no database driver and none of the backend's own code",
	)
	for i, s := range steps {
		in.say("%d. %s", i+1, s)
	}
}

// fillTemplate fills a template that is not Go source.
func fillTemplate(name string, data any) ([]byte, error) {
	t, err := template.New(name).Parse(string(asset(name)))
	if err != nil {
		return nil, err
	}
	var b strings.Builder
	if err := t.Execute(&b, data); err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return []byte(b.String()), nil
}
