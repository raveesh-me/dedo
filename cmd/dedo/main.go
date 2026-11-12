// Command dedo sets up a Go repository to build its API with dedo.
//
//	go install github.com/raveesh-me/dedo/cmd/dedo@latest
//	dedo init
//
// dedo init adds the options module, the lint plugin, the code generators,
// the server libraries and the guidance for coding agents, each where it is
// missing, then prints a main.go to adapt. Running it again changes nothing
// that is already in place.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
)

// version is the dedo release this command installs. The release job checks
// that it names the release being tagged.
const version = "v0.37.0"

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "dedo:", err)
		os.Exit(1)
	}
}

const usage = `usage:
  dedo init [-dir DIR] [-offline]   set up the repository in DIR (default .)
  dedo init -bff [-dir DIR] [-offline]
                                    add a backend for frontend to the repository dedo init set up in DIR:
                                    bff/ with its own go.mod, an unnamed buf module in bff/proto with no
                                    breaking check, its buf.gen.yaml, a starter service and its server
  dedo upgrade [-dir DIR]           move the repository to this dedo's release: pins, templates, buf.lock, generated code
  dedo upgrade -lint [-dir DIR]     move only the options module, whose label is the lint plugin's release, then dedo tools
  dedo update [-version V]          install the latest dedo (or V); then run dedo upgrade
  dedo install [-version V]         install the lint plugin at this release (or V) into $GOBIN
  dedo uninstall                    remove dedo and the lint plugin from $GOBIN; repositories keep .tools/
  dedo tools [-dir DIR] [-wasm F]   copy the lint plugin at buf.yaml's release into .tools/ (installing it
                                    into $GOBIN first if needed), fetch the house rules and list every rule
                                    override, failing on one the order forbids; -wasm also builds it as
                                    WebAssembly into F with the house rules built in
  dedo docs [-dir DIR] [-addr A] [-tunnel]
                                    serve the REST reference (docs/index.html and openapi.json only) on A
                                    (default 127.0.0.1:8099) until Ctrl-C; -tunnel also shares it at a public
                                    Cloudflare quick-tunnel URL (needs cloudflared)
  dedo docs [-dir DIR] -static FILE  write the REST reference as one HTML file that opens from disk with
                                    no network: the OpenAPI document and the page's scripts inline, each
                                    script checked against its integrity hash and cached for offline builds
  dedo modules [-dir DIR]           check that the modules (proto packages <context>.<m>.v<n>) stay apart:
                                    no import cycle between them, shared code imports none, module code
                                    only in internal/<m>, SQL naming only its own schema; then print
                                    the module graph
  dedo breaking [-dir DIR] [-against BRANCH]
                                    run buf breaking for each module of buf.yaml against its own directory
                                    on BRANCH (default main), skipping a module that has no protos there yet
  dedo local-mcp-claude [-dir DIR] [-token T] [-url U] [-name N] [-runtime R] [-config F] [-print] [-remove]
                                    add the service's MCP endpoint to Claude Desktop: an mcpServers entry that
                                    runs mcp-remote in a podman (or docker) container, bridging stdio to the
                                    running service's stateless /mcp with Authorization: Bearer T (default
                                    reader). The URL defaults to the kind cluster's port from
                                    zarf/k8s/dev/kind-config.yaml, or make run's 8080; the name to the Go
                                    module's last element. -print prints the entry; -remove removes it
  dedo slo [-dir DIR] [-out OUT] [-datasource NAME]
                                    write the service level objectives the protos declare as OpenSLO v1
                                    documents, one file per service in OUT (default slo): a Service, an SLI
                                    and an SLO per objective on rpc.server.call.duration, and burn-rate
                                    AlertPolicies notifying the owner team. -datasource names an OpenSLO
                                    DataSource for every indicator
  dedo templates [NAME]             list the bundled templates, or print one
  dedo version                      print the dedo release this command installs
`

func run(args []string, out io.Writer) error {
	if len(args) == 0 {
		return errors.New("no command\n" + usage)
	}
	switch args[0] {
	case "init":
		flags := flag.NewFlagSet("init", flag.ContinueOnError)
		flags.SetOutput(out)
		dir := flags.String("dir", ".", "the repository to set up")
		offline := flags.Bool("offline", false, "edit files only; print the go, buf and make commands instead of running them")
		release := flags.String("version", version, "the dedo release to pin")
		bff := flags.Bool("bff", false, "add a backend for frontend in bff/, a Go module of its own, to a repository dedo init set up")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if *bff {
			return initBFF(*dir, *release, *offline, out)
		}
		return initRepo(*dir, *release, *offline, out)
	case "upgrade":
		flags := flag.NewFlagSet("upgrade", flag.ContinueOnError)
		flags.SetOutput(out)
		dir := flags.String("dir", ".", "the repository to upgrade")
		offline := flags.Bool("offline", false, "edit files only; print the go and buf commands instead of running them")
		lint := flags.Bool("lint", false, "move only the lint plugin to this release, rebuild it and consolidate the house rules")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if *lint {
			return toolsLint(*dir, version, *offline, out)
		}
		return upgrade(*dir, *offline, out)
	case "update":
		flags := flag.NewFlagSet("update", flag.ContinueOnError)
		flags.SetOutput(out)
		release := flags.String("version", "", "the dedo release to install (default: the latest)")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		return update(*release, out)
	case "install":
		flags := flag.NewFlagSet("install", flag.ContinueOnError)
		flags.SetOutput(out)
		release := flags.String("version", version, "the lint plugin release to install")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		return install(*release, out)
	case "uninstall":
		return uninstall(out)
	case "tools":
		flags := flag.NewFlagSet("tools", flag.ContinueOnError)
		flags.SetOutput(out)
		dir := flags.String("dir", ".", "the repository")
		wasm := flags.String("wasm", "", "also build the lint plugin as WebAssembly into this file, with the house rules built in")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if err := tools(*dir, out); err != nil {
			return err
		}
		if *wasm != "" {
			return wasmPlugin(*dir, *wasm, out)
		}
		return nil
	case "docs":
		flags := flag.NewFlagSet("docs", flag.ContinueOnError)
		flags.SetOutput(out)
		dir := flags.String("dir", ".", "the repository whose docs/ to serve")
		addr := flags.String("addr", defaultDocsAddr, "the local address to serve on")
		tunnel := flags.Bool("tunnel", false, "also share the reference at a public Cloudflare quick-tunnel URL")
		static := flags.String("static", "", "write the reference as one self-contained HTML `file` instead of serving it")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if *static != "" {
			if *tunnel || flagSet(flags, "addr") {
				return errors.New("dedo docs -static writes a file and serves nothing: it takes no -addr or -tunnel")
			}
			return docsStatic(*dir, *static, out)
		}
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		return docs(ctx, *dir, *addr, *tunnel, out)
	case "modules":
		flags := flag.NewFlagSet("modules", flag.ContinueOnError)
		flags.SetOutput(out)
		dir := flags.String("dir", ".", "the service repository")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		return modules(*dir, out)
	case "breaking":
		flags := flag.NewFlagSet("breaking", flag.ContinueOnError)
		flags.SetOutput(out)
		dir := flags.String("dir", ".", "the directory that holds buf.yaml, at or below the root of its git working tree")
		against := flags.String("against", "main", "the branch to compare with")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		return breaking(*dir, *against, out)
	case "slo":
		flags := flag.NewFlagSet("slo", flag.ContinueOnError)
		flags.SetOutput(out)
		dir := flags.String("dir", ".", "the directory that holds buf.yaml")
		outDir := flags.String("out", "slo", "the directory to write the documents into, relative to -dir")
		dataSource := flags.String("datasource", "", "the OpenSLO DataSource every indicator reads from (default: type OpenTelemetry)")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		return slo(*dir, *outDir, sloOptions{DataSource: *dataSource}, out)
	case "local-mcp-claude":
		return localMCPClaude(args[1:], out)
	case "templates":
		return templates(args[1:], out)
	case "version":
		fmt.Fprintln(out, version)
		return nil
	case "help", "-h", "--help":
		fmt.Fprint(out, usage)
		return nil
	}
	return fmt.Errorf("unknown command %q\n%s", args[0], usage)
}

// flagSet reports whether name was given on the command line.
func flagSet(flags *flag.FlagSet, name string) bool {
	set := false
	flags.Visit(func(f *flag.Flag) { set = set || f.Name == name })
	return set
}
