package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"text/template"

	"gopkg.in/yaml.v3"

	"github.com/raveesh-me/dedo"
)

const (
	lintPlugin     = ".tools/buf-plugin-dedo-lint"
	optionsModule  = "buf.build/r-a/dedo"
	googleapis     = "buf.build/googleapis/googleapis"
	protovalidate  = "buf.build/bufbuild/protovalidate"
	guidanceMarker = "# Building an API with dedo: guidance for coding agents"
)

// oldPlugins are plugin paths earlier dedo releases told services to list.
var oldPlugins = []string{".tools/buf-plugin-dedo", ".tools/buf-plugin-aip"}

// generators are the code generators buf.gen.yaml runs through go tool.
var generators = []string{"protoc-gen-go", "protoc-gen-connect-go", "protoc-gen-dedo-mcp", "protoc-gen-dedo-openapi"}

// initer runs dedo init: each step reports what it changed.
type initer struct {
	p          *project
	release    string
	out        io.Writer
	cmds       [][]string // network steps, run at the end or printed
	createdBuf bool       // buf.yaml was written from the template this run
}

func initRepo(dir, release string, offline bool, out io.Writer) error {
	p, err := analyze(dir)
	if err != nil {
		return err
	}
	in := &initer{p: p, release: release, out: out}
	if p.schemaOnly {
		fmt.Fprintf(out, "dedo %s: setting up a schema repository (protos in %s/, no go.mod): the options module and the lint plugin, no Go parts\n", release, p.protoRoot)
		return in.initSchema(offline)
	}
	fmt.Fprintf(out, "dedo %s: setting up %s (protos in %s/)\n", release, p.module, p.protoRoot)
	type step struct {
		title string
		run   func() error
	}
	var steps []step
	if p.protos == 0 {
		steps = append(steps, step{"starter service", in.starter})
	}
	steps = append(steps, []step{
		{"options package", in.options},
		{"linter", in.linter},
		{"code generators", in.generators},
		{"server package", in.server},
		{"agent guidance", in.guidance},
	}...)
	for _, s := range steps {
		fmt.Fprintf(out, "\nadding %s...\n", s.title)
		if err := s.run(); err != nil {
			return fmt.Errorf("%s: %w", s.title, err)
		}
	}
	failed := in.runCommands(offline)
	if err := in.printMain(); err != nil {
		return err
	}
	if len(failed) > 0 {
		return fmt.Errorf("%d commands failed: %s; fix the cause and run dedo init again", len(failed), strings.Join(failed, "; "))
	}
	return nil
}

// remotePlugin is the lint plugin on the BSR, for repositories without Go.
const remotePlugin = "buf.build/r-a/dedo-lint"

// pinRemotePlugin lists the BSR lint plugin at this release, replacing any
// other label of it. It reports whether buf.yaml changed.
func (in *initer) pinRemotePlugin(plugins *yaml.Node) bool {
	want := remotePlugin + ":" + in.release
	for _, item := range plugins.Content {
		p := lookup(item, "plugin")
		if p == nil || (p.Value != remotePlugin && !strings.HasPrefix(p.Value, remotePlugin+":")) {
			continue
		}
		if p.Value == want {
			return false
		}
		p.Value = want
		in.say("buf.yaml: lint plugin pinned to %s", want)
		return true
	}
	item := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	set(item, "plugin", scalar(want))
	plugins.Content = append(plugins.Content, item)
	in.say("buf.yaml: added the plugin %s (from the BSR; it runs without Go, at about 5 s per lint)", want)
	return true
}

// initSchema sets up a schema repository: options, lint, agent guidance.
func (in *initer) initSchema(offline bool) error {
	for _, s := range []struct {
		title string
		run   func() error
	}{{"options package", in.options}, {"linter", in.linter}, {"agent guidance", in.guidance}} {
		fmt.Fprintf(in.out, "\nadding %s...\n", s.title)
		if err := s.run(); err != nil {
			return fmt.Errorf("%s: %w", s.title, err)
		}
	}
	failed := in.runCommands(offline)
	fmt.Fprintln(in.out, "\nnext:")
	in.say("1. buf lint: it lists the declarations each service still needs")
	in.say("2. for generated code and a server, add a go.mod (go mod init) and run dedo init again")
	if len(failed) > 0 {
		return fmt.Errorf("%d commands failed: %s; fix the cause and run dedo init again", len(failed), strings.Join(failed, "; "))
	}
	return nil
}

// optionsPin matches the labelled options module in the buf.yaml template,
// such as buf.build/r-a/dedo:v0.10.0 (not the dedo-lint plugin beside it).
var optionsPin = regexp.MustCompile(regexp.QuoteMeta(optionsModule) + `:[A-Za-z0-9._]+`)

// pinOptions pins the options module to this release, so the options and
// the lint plugin, generators and runtime in go.mod are one version. It
// returns the dep it wrote, or "" when the pin was already right.
func (in *initer) pinOptions(deps *yaml.Node) string {
	want := optionsModule + ":" + in.release
	for _, n := range deps.Content {
		if n.Value == want {
			return ""
		}
		if n.Value == optionsModule || strings.HasPrefix(n.Value, optionsModule+":") {
			n.Value = want
			return want
		}
	}
	deps.Content = append(deps.Content, scalar(want))
	return want
}

// depUpdate queues buf dep update once.
func (in *initer) depUpdate() {
	for _, c := range in.cmds {
		if strings.Join(c, " ") == "buf dep update" {
			return
		}
	}
	in.cmds = append(in.cmds, []string{"buf", "dep", "update"})
}

func (in *initer) say(format string, args ...any) {
	fmt.Fprintf(in.out, "  "+format+"\n", args...)
}

func (in *initer) path(rel string) string { return filepath.Join(in.p.dir, rel) }

// asset reads an embedded file from the dedo module.
func asset(name string) []byte {
	b, err := fs.ReadFile(dedo.Files, name)
	if err != nil {
		panic(err) // embedded at build time
	}
	return b
}

// options adds the options module and the modules it builds on to buf.yaml,
// creating buf.yaml from the template when it is missing.
func (in *initer) options() error {
	doc, ok, err := loadYAML(in.path("buf.yaml"))
	if err != nil {
		return fmt.Errorf("buf.yaml: %w", err)
	}
	if !ok {
		src := string(asset("templates/buf.yaml"))
		src = strings.Replace(src, "# Copy into a service repository.\n", "", 1)
		src = strings.Replace(src, "  - path: proto\n", "  - path: "+in.p.protoRoot+"\n", 1)
		src = optionsPin.ReplaceAllString(src, optionsModule+":"+in.release)
		if err := os.WriteFile(in.path("buf.yaml"), []byte(src), 0o644); err != nil {
			return err
		}
		in.say("buf.yaml: created, with %s, %s and %s", optionsModule, googleapis, protovalidate)
		in.createdBuf = true
		in.depUpdate()
		return nil
	}
	if v := lookup(root(doc), "version"); v == nil || v.Value != "v2" {
		return errors.New("buf.yaml is not version v2: run buf config migrate first")
	}
	deps := child(root(doc), "deps", yaml.SequenceNode)
	added := addScalars(deps, googleapis, protovalidate)
	pinned := in.pinOptions(deps)
	if _, err := os.Stat(in.path("buf.lock")); err != nil {
		in.say("buf.lock: missing; buf dep update will write it")
		in.depUpdate()
	}
	if len(added) == 0 && pinned == "" {
		in.say("buf.yaml: deps already in place")
		return nil
	}
	if len(added) > 0 {
		in.say("buf.yaml: added deps %s", strings.Join(added, ", "))
	}
	if pinned != "" {
		in.say("buf.yaml: options module pinned to %s, the release of the lint plugin and runtime", pinned)
	}
	in.depUpdate()
	return saveYAML(in.path("buf.yaml"), doc)
}

// linter lists the lint plugin and its categories in buf.yaml, and adds the
// Makefile targets that download it.
func (in *initer) linter() error {
	doc, _, err := loadYAML(in.path("buf.yaml"))
	if err != nil {
		return fmt.Errorf("buf.yaml: %w", err)
	}
	tmpl, err := parseYAML(asset("templates/buf.yaml"))
	if err != nil {
		return err
	}
	changed := false
	lint := lookup(root(doc), "lint")
	if lint == nil {
		set(root(doc), "lint", lookup(root(tmpl), "lint"))
		in.say("buf.yaml: added the lint section")
		changed = true
	} else {
		if added := addScalars(child(lint, "use", yaml.SequenceNode), "AIP_NOLANG", "DEDO", "ZALANDO"); len(added) > 0 {
			in.say("buf.yaml: lint.use gained %s", strings.Join(added, ", "))
			changed = true
		}
		skips := scalars(lookup(lookup(root(tmpl), "lint"), "except"))
		if added := addScalars(child(lint, "except", yaml.SequenceNode), skips...); len(added) > 0 {
			in.say("buf.yaml: lint.except gained the %d AIP rules dedo skips (see RULES.md)", len(added))
			changed = true
		}
	}
	plugins := child(root(doc), "plugins", yaml.SequenceNode)
	kept := plugins.Content[:0]
	for _, item := range plugins.Content {
		if p := lookup(item, "plugin"); p != nil && (isOldPlugin(p.Value) || in.p.schemaOnly && p.Value == lintPlugin) {
			in.say("buf.yaml: removed %s, which buf-plugin-dedo-lint replaces", p.Value)
			changed = true
			continue
		}
		kept = append(kept, item)
	}
	plugins.Content = kept
	if in.p.schemaOnly {
		changed = in.pinRemotePlugin(plugins) || changed
	} else if findItem(plugins, func(n *yaml.Node) bool { return lookup(n, "plugin") != nil && lookup(n, "plugin").Value == lintPlugin }) == nil {
		item := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		set(item, "plugin", scalar(lintPlugin))
		plugins.Content = append(plugins.Content, item)
		in.say("buf.yaml: added the plugin %s", lintPlugin)
		changed = true
	}
	if changed {
		if err := saveYAML(in.path("buf.yaml"), doc); err != nil {
			return err
		}
	} else if in.createdBuf {
		in.say("buf.yaml: lint plugin and categories added from the template")
	} else {
		in.say("buf.yaml: lint plugin and categories already in place")
	}
	if in.p.schemaOnly {
		// No Go: the plugin is the BSR's, which buf.lock pins.
		in.cmds = append(in.cmds, []string{"buf", "plugin", "update"})
		return nil
	}
	if err := in.writeFile("Makefile.api", asset("templates/Makefile.api"), true); err != nil {
		return err
	}
	if err := in.ensureLine("Makefile", "include Makefile.api"); err != nil {
		return err
	}
	// Releases before v0.11.0 downloaded the plugin and left a stamp file
	// named after their version in .tools/.
	if stamps, _ := filepath.Glob(in.path(filepath.Join(".tools", "v[0-9]*"))); len(stamps) > 0 {
		for _, s := range stamps {
			_ = os.Remove(s)
		}
		in.say(".tools/: removed the download stamp of an earlier release")
	}
	if err := in.ensureLine(".gitignore", ".tools/"); err != nil {
		return err
	}
	// The plugin is pinned in go.mod like the generators, then built into
	// .tools/: buf starts it a dozen times per lint, so it runs natively
	// rather than through go tool.
	in.cmds = append(in.cmds,
		[]string{"go", "get", "-tool", lintPluginPkg + "@" + in.release},
		buildPluginCmd(),
	)
	in.say("go.mod: will pin buf-plugin-dedo-lint as a Go tool and build it into %s", pluginPath())
	return nil
}

func isOldPlugin(p string) bool {
	for _, old := range oldPlugins {
		if p == old || strings.HasSuffix(p, "/"+path.Base(old)) {
			return true
		}
	}
	return false
}

// generators pins the code generators as Go tools and lists them in
// buf.gen.yaml, creating it from the template when it is missing.
func (in *initer) generators() error {
	if err := in.removeStaleSchemas(); err != nil {
		return err
	}
	src := in.genTemplate()
	tmpl, err := parseYAML(src)
	if err != nil {
		return err
	}
	doc, ok, err := loadYAML(in.path("buf.gen.yaml"))
	if err != nil {
		return fmt.Errorf("buf.gen.yaml: %w", err)
	}
	if !ok {
		if err := os.WriteFile(in.path("buf.gen.yaml"), src, 0o644); err != nil {
			return err
		}
		in.say("buf.gen.yaml: created; Go code goes to gen/go, the OpenAPI document to docs/")
		in.pinGenerators(generators)
		return nil
	}
	changed := in.mergeManaged(doc, tmpl)
	plugins := child(root(doc), "plugins", yaml.SequenceNode)
	// dedo's generators write into the same Go packages as the repository's
	// own protoc-gen-go, so they go where it goes, with its paths= option.
	goEntry := findItem(plugins, func(n *yaml.Node) bool { return runsGenerator(n, "protoc-gen-go") })
	var pinned []string
	for _, gen := range generators {
		if existing := findItem(plugins, func(n *yaml.Node) bool { return runsGenerator(n, gen) }); existing != nil {
			if ref := strings.Join(allStrings(pluginRef(existing)), " "); connectV1Remote.MatchString(ref) {
				in.say("buf.gen.yaml: WARNING %s generates connect-go v1 code; dedo needs v2: use buf.build/connectrpc/go:%s", ref, connectRelease)
			}
			if !isGoTool(existing, gen) {
				in.say("buf.gen.yaml: %s already runs as %s; left as it is", gen, strings.Join(allStrings(pluginRef(existing)), " "))
			}
			continue
		}
		entry := cloneNode(findItem(lookup(root(tmpl), "plugins"), func(n *yaml.Node) bool { return runsGenerator(n, gen) }))
		if goEntry != nil && gen != "protoc-gen-dedo-openapi" {
			alignWith(entry, goEntry)
		}
		plugins.Content = append(plugins.Content, entry)
		in.say("buf.gen.yaml: added %s, writing to %s", gen, lookup(entry, "out").Value)
		pinned = append(pinned, gen)
		changed = true
	}
	in.pinGenerators(pinned)
	if !changed {
		in.say("buf.gen.yaml: generators already in place")
		return nil
	}
	return saveYAML(in.path("buf.gen.yaml"), doc)
}

// generatorTools are the Go tools buf.gen.yaml runs each generator as.
func (in *initer) generatorTools() map[string]string {
	return map[string]string{
		"protoc-gen-go":           "google.golang.org/protobuf/cmd/protoc-gen-go",
		"protoc-gen-connect-go":   "connectrpc.com/connect/v2/cmd/protoc-gen-connect-go@" + connectRelease,
		"protoc-gen-dedo-mcp":     "github.com/raveesh-me/dedo/mcp/cmd/protoc-gen-dedo-mcp@" + in.release,
		"protoc-gen-dedo-openapi": "github.com/raveesh-me/dedo/openapi/cmd/protoc-gen-dedo-openapi@" + in.release,
	}
}

// pinGenerators pins the given generators, and dedo's own two always, so an
// upgrade moves them to this release.
func (in *initer) pinGenerators(gens []string) {
	tools := in.generatorTools()
	want := map[string]bool{"protoc-gen-dedo-mcp": true, "protoc-gen-dedo-openapi": true}
	for _, g := range gens {
		want[g] = true
	}
	var names []string
	for _, gen := range generators {
		if want[gen] {
			in.cmds = append(in.cmds, []string{"go", "get", "-tool", tools[gen]})
			names = append(names, gen)
		}
	}
	in.say("go.mod: will pin %s as Go tools", strings.Join(names, ", "))
}

// The connect-go v2 releases dedo is built and tested with.
const (
	connectRelease  = "v2.0.1"
	vanguardRelease = "v0.5.0"
	validateRelease = "v0.9.0"
)

// connectV1Remote matches Buf's remote Connect plugin pinned to a v1 release,
// which generates code dedo's connect-go v2 runtime cannot serve.
var connectV1Remote = regexp.MustCompile(`^buf\.build/connectrpc/go(simple)?:v1\.`)

// generatorRefs are the Buf remote plugins that produce what a generator
// does, so a repository using them gets no second copy of that code.
var generatorRefs = map[string][]string{
	"protoc-gen-go":         {"buf.build/protocolbuffers/go"},
	"protoc-gen-connect-go": {"buf.build/connectrpc/go", "buf.build/connectrpc/gosimple"},
}

// runsGenerator reports whether a buf.gen.yaml plugin entry runs gen: as a
// local binary or go tool, or as an equivalent remote plugin. It matches
// whole names, so protoc-gen-go-grpc is not protoc-gen-go.
func runsGenerator(n *yaml.Node, gen string) bool {
	for _, s := range allStrings(pluginRef(n)) {
		ref, _, _ := strings.Cut(s, ":")
		if s == gen || path.Base(s) == gen || slices.Contains(generatorRefs[gen], ref) {
			return true
		}
	}
	return false
}

// pluginRef is what a plugin entry runs: its local, remote or
// protoc_builtin value.
func pluginRef(n *yaml.Node) *yaml.Node {
	for _, key := range []string{"local", "remote", "protoc_builtin"} {
		if v := lookup(n, key); v != nil {
			return v
		}
	}
	return nil
}

// isGoTool reports whether an entry runs gen through go tool, as dedo
// writes it.
func isGoTool(n *yaml.Node, gen string) bool {
	return strings.Join(allStrings(pluginRef(n)), " ") == "go tool "+gen
}

// alignWith points a new Go generator entry at the repository's existing
// Go output: the same out, and the same paths= option.
func alignWith(entry, goEntry *yaml.Node) {
	if out := lookup(goEntry, "out"); out != nil {
		set(entry, "out", scalar(out.Value))
	}
	pathsOpt := ""
	for _, o := range allStrings(lookup(goEntry, "opt")) {
		for _, part := range strings.Split(o, ",") {
			if strings.HasPrefix(part, "paths=") {
				pathsOpt = part
			}
		}
	}
	if pathsOpt == "" {
		remove(entry, "opt")
		return
	}
	set(entry, "opt", scalar(pathsOpt))
}

// removeStaleSchemas deletes the *.mcp_schema.pb.go files that
// protoc-gen-dedo-mcp wrote before v0.9.0. It now writes *.mcp.pb.go with the
// same declarations, and buf generate does not remove the old files, so
// keeping them breaks the build.
func (in *initer) removeStaleSchemas() error {
	return filepath.WalkDir(in.p.dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && path != in.p.dir && (d.Name() == ".git" || d.Name() == "node_modules" || d.Name() == "vendor") {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, ".mcp_schema.pb.go") {
			return nil
		}
		if err := os.Remove(path); err != nil {
			return err
		}
		rel, _ := filepath.Rel(in.p.dir, path)
		in.say("%s: removed; buf generate now writes %s", rel, strings.TrimSuffix(filepath.Base(path), ".mcp_schema.pb.go")+".mcp.pb.go")
		return nil
	})
}

// genTemplate is templates/buf.gen.yaml for this repository.
func (in *initer) genTemplate() []byte {
	src := string(asset("templates/buf.gen.yaml"))
	if i := strings.Index(src, "version: v2"); i > 0 {
		src = "# Generators run through `go tool`, pinned in go.mod. Set up by dedo init.\n" + src[i:]
	}
	r := strings.NewReplacer(
		"github.com/YOUR_ORG/YOUR_REPO/gen/go", in.p.module+"/gen/go",
		"YOUR_API_TITLE", apiTitle(in.p.module),
		"https://YOUR_HOST", "http://localhost:8080",
		"  - directory: proto\n", "  - directory: "+in.p.protoRoot+"\n",
	)
	return []byte(r.Replace(src))
}

// mergeManaged adds what managed mode needs for dedo: the options module's
// Go package and the modules that ship their own Go code.
func (in *initer) mergeManaged(doc, tmpl *yaml.Node) bool {
	managed := lookup(root(doc), "managed")
	if managed == nil {
		// Turning managed mode on would move existing generated code; the
		// options module sets its own go_package, so dedo needs none.
		in.say("buf.gen.yaml: managed mode is off; left off")
		return false
	}
	changed := false
	tm := lookup(root(tmpl), "managed")
	byModule := func(module string) func(*yaml.Node) bool {
		return func(n *yaml.Node) bool { return lookup(n, "module") != nil && lookup(n, "module").Value == module }
	}
	override := child(managed, "override", yaml.SequenceNode)
	if findItem(override, byModule(optionsModule)) == nil {
		override.Content = append(override.Content, findItem(lookup(tm, "override"), byModule(optionsModule)))
		in.say("buf.gen.yaml: added the go_package_prefix override for %s", optionsModule)
		changed = true
	}
	disable := child(managed, "disable", yaml.SequenceNode)
	for _, module := range []string{googleapis, protovalidate} {
		if findItem(disable, byModule(module)) == nil {
			disable.Content = append(disable.Content, findItem(lookup(tm, "disable"), byModule(module)))
			in.say("buf.gen.yaml: managed mode leaves %s alone", module)
			changed = true
		}
	}
	return changed
}

// server adds the runtime modules and the REST reference page, and the
// policy-table test when the repository already has a service.
func (in *initer) server() error {
	in.cmds = append(in.cmds, []string{"go", "get",
		"github.com/raveesh-me/dedo@" + in.release,
		"github.com/raveesh-me/dedo/mcp@" + in.release,
		"connectrpc.com/connect/v2@" + connectRelease,
		"connectrpc.com/vanguard@" + vanguardRelease,
		"connectrpc.com/validate@" + validateRelease,
	})
	in.say("go.mod: will add github.com/raveesh-me/dedo and its mcp module at %s", in.release)
	if err := os.MkdirAll(in.path("docs"), 0o755); err != nil {
		return err
	}
	if err := in.writeFile("docs/docs.go", asset("templates/docs/docs.go.tmpl"), false); err != nil {
		return err
	}
	page := bytes.Replace(asset("templates/docs/index.html"), []byte("YOUR_API_TITLE"), []byte(apiTitle(in.p.module)), 1)
	if err := in.writeFile("docs/index.html", page, false); err != nil {
		return err
	}
	if in.p.service == nil {
		in.say("policy_table_test.go, server.go: skipped until the repository has a service")
		return nil
	}
	if in.p.rootPkg != "" && in.p.rootPkg != "main" {
		in.say("policy_table_test.go, server.go: skipped; the repository root is package %s, not main", in.p.rootPkg)
		return nil
	}
	test, err := in.render("templates/policy_table_test.go.tmpl")
	if err != nil {
		return err
	}
	if err := in.writeFile("policy_table_test.go", test, false); err != nil {
		return err
	}
	if hasNewServer(in.p.dir) {
		in.say("server.go: newServer already exists; left as it is")
		return nil
	}
	stub, err := in.render("templates/server.go.tmpl")
	if err != nil {
		return err
	}
	return in.writeFile("server.go", stub, false)
}

// hasNewServer reports whether a Go file in the repository root already
// declares newServer, which the printed main.go calls.
func hasNewServer(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		src, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err == nil && strings.Contains(string(src), "func newServer(") {
			return true
		}
	}
	return false
}

// guidance appends the agent guidance to CLAUDE.md and AGENTS.md where they
// lack it, and creates AGENTS.md when neither file exists.
func (in *initer) guidance() error {
	text := asset("guidance/claude.md")
	found := false
	for _, name := range []string{"CLAUDE.md", "AGENTS.md"} {
		b, err := os.ReadFile(in.path(name))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		found = true
		if bytes.Contains(b, []byte(guidanceMarker)) {
			in.say("%s: already has the dedo guidance", name)
			continue
		}
		var add []byte
		if len(b) > 0 && !bytes.HasSuffix(b, []byte("\n\n")) {
			add = []byte("\n")
			if !bytes.HasSuffix(b, []byte("\n")) {
				add = []byte("\n\n")
			}
		}
		if err := appendFile(in.path(name), append(add, text...)); err != nil {
			return err
		}
		in.say("%s: appended the dedo guidance", name)
	}
	if found {
		return nil
	}
	if err := os.WriteFile(in.path("AGENTS.md"), text, 0o644); err != nil {
		return err
	}
	in.say("AGENTS.md: created with the dedo guidance")
	return nil
}

// runCommands runs the network steps, or prints them when offline or when a
// tool is missing.
func (in *initer) runCommands(offline bool) []string {
	if offline {
		fmt.Fprintln(in.out, "\nfetching (skipped: -offline)...")
	} else {
		fmt.Fprintln(in.out, "\nfetching (a first run downloads Go modules and can take a couple of minutes)...")
	}
	var todo [][]string
	var failed []string
	for _, c := range in.cmds {
		if offline {
			todo = append(todo, c)
			continue
		}
		if _, err := exec.LookPath(c[0]); err != nil {
			in.say("%s is not installed; run it yourself later", c[0])
			todo = append(todo, c)
			continue
		}
		in.say("$ %s", strings.Join(c, " "))
		cmd := exec.Command(c[0], c[1:]...)
		cmd.Dir = in.p.dir
		cmd.Stdout, cmd.Stderr = in.out, in.out
		if err := cmd.Run(); err != nil {
			// Later commands do not depend on this one, so carry on and
			// report every failure at the end.
			in.say("failed: %v", err)
			failed = append(failed, strings.Join(c, " "))
		}
	}
	if len(todo) > 0 {
		in.say("run these in %s:", in.p.dir)
		for _, c := range todo {
			in.say("  %s", strings.Join(c, " "))
		}
	}
	return failed
}

// printMain prints a main.go that serves the first service on every
// transport, for the user to adapt.
func (in *initer) printMain() error {
	src, err := in.render("templates/main.go.tmpl")
	if err != nil {
		return err
	}
	// main.go is written only where it can be built: a service to serve, a
	// package main root, and no main.go of the user's. Its imports also keep
	// the pinned Vanguard in go.mod, which go mod tidy would otherwise drop.
	_, statErr := os.Stat(in.path("main.go"))
	write := in.p.service != nil && (in.p.rootPkg == "" || in.p.rootPkg == "main") && errors.Is(statErr, fs.ErrNotExist)
	if write {
		if err := os.WriteFile(in.path("main.go"), src, 0o644); err != nil {
			return err
		}
		fmt.Fprintf(in.out, "\nmain.go: created; it serves %s on Connect, gRPC, REST, /docs and /mcp\n", in.p.service.Name)
	}
	fmt.Fprintln(in.out, "\nnext:")
	if len(in.p.undeclared) > 0 {
		in.say("0. %s declare nothing for dedo yet: buf lint lists what each needs, and until it is declared the policy test and the server's startup fail on purpose", strings.Join(in.p.undeclared, ", "))
	}
	if in.p.service == nil {
		in.say("1. write your service in %s/ (see guidance/tutorial.md)", in.p.protoRoot)
		in.say("2. run dedo init again: it writes server.go and main.go for it")
	} else {
		in.say("1. make api-check api-generate")
		in.say("2. go mod tidy && go run .")
		in.say("3. implement the RPCs in server.go; until then they answer Unimplemented")
		in.say("4. before shipping, replace authMiddleware and devTokens in main.go with your token verification")
		if in.p.others > 0 {
			in.say("   main.go serves %s; %d more services need the same three lines each", in.p.service.Name, in.p.others)
		}
	}
	if write {
		return nil
	}
	if statErr == nil {
		fmt.Fprintf(in.out, "\n// ---- main.go: dedo's template, to compare with yours ----\n%s", src)
		return nil
	}
	fmt.Fprintf(in.out, "\n// ---- main.go ----\n%s", src)
	return nil
}

// render fills a Go template with this repository's names, or with
// placeholders when it has no service yet.
func (in *initer) render(name string) ([]byte, error) {
	svc := service{Name: "YourService", GoPkg: "yourv1", GoImport: in.p.module + "/gen/go/your/package/v1"}
	if in.p.service != nil {
		svc = *in.p.service
	}
	var read []string
	for _, s := range in.p.scopes {
		if strings.HasSuffix(s, ".read") {
			read = append(read, s)
		}
	}
	data := struct {
		service
		Module, Service, Name string
		ReadScopes, AllScopes []string
	}{svc, in.p.module, svc.Name, path.Base(in.p.module), read, in.p.scopes}
	t, err := template.New(name).Parse(string(asset(name)))
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// writeFile writes a file. An existing file is kept unless replace is set,
// which updates it when its content differs.
func (in *initer) writeFile(rel string, content []byte, replace bool) error {
	old, err := os.ReadFile(in.path(rel))
	switch {
	case err == nil && bytes.Equal(old, content):
		in.say("%s: already in place", rel)
		return nil
	case err == nil && !replace:
		in.say("%s: exists; left as it is", rel)
		return nil
	case err != nil && !errors.Is(err, fs.ErrNotExist):
		return err
	}
	if err := os.WriteFile(in.path(rel), content, 0o644); err != nil {
		return err
	}
	if old != nil {
		in.say("%s: updated", rel)
	} else {
		in.say("%s: created", rel)
	}
	return nil
}

// ensureLine appends a line to a file that lacks it, creating the file.
func (in *initer) ensureLine(rel, line string) error {
	b, err := os.ReadFile(in.path(rel))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	for _, l := range strings.Split(string(b), "\n") {
		if strings.TrimSpace(l) == line {
			in.say("%s: already has %q", rel, line)
			return nil
		}
	}
	add := line + "\n"
	if len(b) > 0 && !bytes.HasSuffix(b, []byte("\n")) {
		add = "\n" + add
	}
	if err := appendFile(in.path(rel), []byte(add)); err != nil {
		return err
	}
	in.say("%s: added %q", rel, line)
	return nil
}

func appendFile(path string, b []byte) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func saveYAML(path string, doc *yaml.Node) error {
	b, err := encodeYAML(doc)
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

// apiTitle turns a module path's last element into a title:
// github.com/acme/time-counter becomes "Time Counter API".
func apiTitle(module string) string {
	words := strings.FieldsFunc(path.Base(module), func(r rune) bool { return r == '-' || r == '_' || r == '.' })
	for i, w := range words {
		words[i] = strings.ToUpper(w[:1]) + w[1:]
	}
	return strings.Join(words, " ") + " API"
}
