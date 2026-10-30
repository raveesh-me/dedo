# Install dedo

Each numbered section below adds one part of dedo to a repository. `dedo init` does all of them, editing what is already there:

```sh
go install github.com/raveesh-me/dedo/cmd/dedo@v0.19.0
dedo init
```

It never removes your own deps, plugins or generators, and running it again changes nothing that is in place. In a module with no protos yet, it writes a starter service that passes lint, with a `server.go` stub, so the first lint, generate and build all succeed. If one of its `go`, `buf` or `make` commands fails, it runs the rest and lists what to fix. It ends by writing a `main.go` that serves your service, or, when you have one, printing dedo's to compare. `dedo init -offline` edits the files only and prints the `go`, `buf` and `make` commands instead of running them.

New to dedo? [tutorial.md](tutorial.md) builds a whole service from an empty directory. Already on Buf? Read [Adding dedo to an existing Buf repository](#adding-dedo-to-an-existing-buf-repository) first.

You need Go 1.25 or later and the [Buf CLI](https://buf.build/docs/installation).

## 1. Annotations: the options module

Add dedo's options module, and the two modules it builds on, to `buf.yaml`:

```yaml
version: v2
deps:
  - buf.build/googleapis/googleapis
  - buf.build/bufbuild/protovalidate
  - buf.build/r-a/dedo:v0.19.0   # the same release as the lint plugin and runtime
```

```sh
buf dep update
```

Pin the label to the dedo release you install, so the options, the lint rules that check them and the runtime that enforces them always agree. `dedo upgrade` moves all of them together.

Then import it in your `.proto` files and declare each service's and RPC's contract:

```protobuf
import "dedo/v1/options.proto";
```

[guideline.md](guideline.md#3-write-the-contract) lists every option.

## 2. Linting: buf-plugin-dedo-lint

The plugin lints `.proto` files, wherever they are, so it is not pinned in any Go module: the options module's label in `buf.yaml` (section 1) names its release. `dedo tools` installs it at that release into `$GOBIN` (`dedo install` does only that) and copies it into `.tools/`, next to `buf.yaml`:

```sh
dedo tools          # installs the plugin if needed, copies it into .tools/, and lists the rule overrides
printf '.tools/\n' >> .gitignore
```

Without the `dedo` command, `go install github.com/raveesh-me/dedo/lint/cmd/buf-plugin-dedo-lint@v0.19.0` and a copy into `.tools/` do the same. It runs natively because Buf starts the plugin about a dozen times per lint; through `go tool` each lint would take 2 s instead of 0.3 s. Run `dedo tools` again after a fresh clone, a version change, or any change to `rules.yml` or its imports.

List the plugin and its one category, `DEDO`, in `buf.yaml`, and choose its rules in `rules.yml` next to it ([templates/rules.yml](../templates/rules.yml); `dedo templates rules.yml` prints it). `buf.yaml` names no other rule of the plugin, in `use` or `except`: dedo reads only `rules.yml` ([RULES.md](RULES.md#choosing-the-rules) says how it chooses):

```yaml
# buf.yaml
lint:
  use:
    - STANDARD
    - COMMENTS
    - DEDO
plugins:
  - plugin: .tools/buf-plugin-dedo-lint
```

```yaml
# rules.yml
use:
  - AIP_NOLANG
  - ZALANDO
  - DEDO
  - HOUSE
```

```sh
dedo templates Makefile.api > Makefile.api && printf 'include Makefile.api\n' >> Makefile
make check          # buf format, buf lint, buf breaking against main
```

The breaking check compares with the `main` branch, and is skipped while `main` has no `.proto` files. Compare with another branch with `make check API_BASE=develop`. The targets of `Makefile.api` are `run`, `generate`, `check`, `test` and `tools`; where your `Makefile` already has one of those names, `dedo init` names dedo's `api-<name>` instead, and `api-check`, `api-generate` and `api-test` always exist, for CI.

Your organisation's own rules go in `rules.yml` next to `buf.yaml`, as CEL checks that can import shared rules from git repositories; `HOUSE` in `use` runs them. [templates/rules.example.yml](../templates/rules.example.yml) shows every key. A house rule can override a DEDO, AIP or ZALANDO rule by naming it in `overrides`, or lock itself with `final: true`; `dedo tools` lists every override and final rule in force. [RULES.md](RULES.md#house-rules) has the format, and [Precedence](RULES.md#precedence) the order.

If your services partition their data between tenants, declare `tenancy` on each and add `DEDO_MULTI_TENANT` to `rules.yml`'s `use` (`dedo init` does when a service declares it); then pass `Tenants` to the interceptor in section 4. [RULES.md](RULES.md#multi-tenant-rules) explains the model.

Repositories without a Go module, such as a TypeScript service or a schema repository, set the plugin up the same way. They need Go on the machine and in CI, to install it, but no `go.mod`. In CI:

```sh
go install github.com/raveesh-me/dedo/cmd/dedo@v0.19.0
dedo tools          # installs the plugin at buf.yaml's release, copies it into .tools/, fetches the house rules
buf lint
```

The Wasm plugin on the BSR (`buf.build/r-a/dedo-lint`) runs without Go but cannot read `rules.yml`, so it runs dedo's default rule sets and no house rules; `dedo init` replaces it. A platform team that must lint without Go can build a plugin with `rules.yml` built in, `dedo tools -wasm house-lint.wasm`, push it to the BSR (`buf plugin push buf.build/<org>/house-lint --binary=house-lint.wasm`), and list that plugin instead.

## 3. Code generators

Pin the generators as Go tools:

```sh
go get -tool google.golang.org/protobuf/cmd/protoc-gen-go
go get -tool connectrpc.com/connect/v2/cmd/protoc-gen-connect-go@v2.0.1
go get -tool github.com/raveesh-me/dedo/mcp/cmd/protoc-gen-dedo-mcp@v0.19.0
go get -tool github.com/raveesh-me/dedo/openapi/cmd/protoc-gen-dedo-openapi@v0.19.0
```

Add them to `buf.gen.yaml` ([templates/buf.gen.yaml](../templates/buf.gen.yaml) is a complete one):

```yaml
plugins:
  - local: ["go", "tool", "protoc-gen-go"]
    out: gen/go
    opt: paths=source_relative
  - local: ["go", "tool", "protoc-gen-connect-go"]
    out: gen/go
    opt: paths=source_relative
  # The MCP tools: <Service>MCPTools() with their schemas, text and typed calls.
  - local: ["go", "tool", "protoc-gen-dedo-mcp"]
    out: gen/go
    opt: paths=source_relative
  # The OpenAPI 3.1 document for the REST reference.
  - local: ["go", "tool", "protoc-gen-dedo-openapi"]
    out: docs
    strategy: all
    opt:
      - title=Your API
      - server=https://api.example.com
```

If `buf.gen.yaml` uses managed mode, keep dedo's Go package and the modules that ship their own Go code out of it:

```yaml
managed:
  enabled: true
  override:
    - file_option: go_package_prefix
      value: github.com/you/your-service/gen/go
    # After your own override: the last match wins.
    - file_option: go_package_prefix
      module: buf.build/r-a/dedo
      value: github.com/raveesh-me/dedo/gen/go
  disable:
    - module: buf.build/googleapis/googleapis
    - module: buf.build/bufbuild/protovalidate
```

```sh
make generate       # buf generate, then go mod tidy
```

## 4. The server

Add the runtime libraries. dedo is built on connect-go v2, where one `connect.Server` holds the interceptors and every transport mounts on it:

```sh
go get github.com/raveesh-me/dedo@v0.19.0 github.com/raveesh-me/dedo/mcp@v0.19.0
go get connectrpc.com/connect/v2@v2.0.1 connectrpc.com/vanguard@v0.5.0 connectrpc.com/validate@v0.9.0
```

Build against dedo's build of the MCP Go SDK. The SDK release decodes every message through a JSON decoder that allocates at least 32 KiB per call, several times per tool call; the fix is proposed upstream ([go-sdk#1454](https://github.com/modelcontextprotocol/go-sdk/pull/1454)), and until a release has it, dedo's build is that release plus the fix. The replace names the release, so a service that needs a later SDK gets the later upstream release instead. `dedo init` and `dedo upgrade` write and maintain the line, with a comment saying why, and a later dedo release removes it.

```sh
go mod edit -replace=github.com/modelcontextprotocol/go-sdk@v1.8.0=github.com/raveesh-me/go-sdk@v1.8.0-dedo.1
```

Add the REST reference page; it serves the generated `openapi.json` at `/openapi.json` and a reference page at `/docs`:

```sh
mkdir -p docs
dedo templates docs/docs.go.tmpl > docs/docs.go
dedo templates docs/index.html > docs/index.html   # then set its <title>
```

Then wire everything in `main.go`. `dedo init` writes it with your names filled in (or prints it, when you have a `main.go` already); here `CounterService` from `acme.time_counter.v1`:

```go
func newHandler(log *slog.Logger) (http.Handler, error) {
	// The API: scopes and policy come from the .proto, not from handlers.
	table, err := policy.Build(time_counterv1connect.CounterServiceName)
	if err != nil {
		return nil, err
	}
	guard, err := interceptor.New(interceptor.Config{
		Table:         table,
		Scopes:        scopesFromContext, // your auth interceptor puts the caller's scopes in the context
		EnforceScopes: true,
		Log:           log,
	})
	if err != nil {
		return nil, err
	}
	// One server, one interceptor chain, for every transport below.
	server := connect.NewServer(flowid.Interceptor(), auth, guard, validate.NewServerInterceptor())
	time_counterv1connect.RegisterCounterServiceHandler(server, newServer())

	mux := http.NewServeMux()
	// Connect, gRPC and gRPC-Web, then REST from the google.api.http bindings.
	connecthttp.Mount(mux, server)
	if err := vanguard.Mount(mux, server); err != nil {
		return nil, err
	}

	// MCP: every RPC is a tool, as protoc-gen-dedo-mcp generated it, called
	// in-process on the same server with the caller's token.
	tools := mcp.NewServer(&mcp.Implementation{Name: "time-counter", Version: "v1"}, nil)
	if err := mcpserver.AddTools(tools, connect.NewClient(connectinprocess.New(server)), time_counterv1.CounterServiceMCPTools()...); err != nil {
		return nil, err
	}

	// MCP_ACCESS=authenticated answers only callers whose token you accept,
	// tool discovery included; public, the default, lets anyone list the
	// tools. Tool calls are checked by the interceptors above either way.
	endpoint, err := mcpserver.Guard(mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return tools },
		&mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true}),
		mcpserver.Access(os.Getenv("MCP_ACCESS")), authenticated)
	if err != nil {
		return nil, err
	}

	mux.Handle("GET /openapi.json", docs.Spec())
	mux.Handle("GET /docs", docs.Page())
	// The MCP transport requires an Origin check.
	mux.Handle("/mcp", flowid.Middleware(http.NewCrossOriginProtection().Handler(endpoint)))
	return mux, nil
}
```

`MCP_ACCESS` says who may reach `/mcp`: `public` (the default) lets anyone list the tools, and every call is still scope-checked; `authenticated` answers 401, before any MCP handling, to requests without a token your `authenticated` function accepts (the template's checks `devTokens`, like `auth`). An unknown value stops the service at startup. [guideline.md](guideline.md#6-wire-the-service) says more.

In a multi-tenant service, `interceptor.Config` also takes `Tenants`, a function that says whether the caller may act in a tenant ID; the server will not start without it. `dedo init` writes a development one, next to `devTokens`, when a service declares `tenancy`.

Auth is a server interceptor (`auth` in the template), not HTTP middleware, so it covers MCP calls, which reach the server in-process, as well as Connect, gRPC and REST. The complete file, with `main`, imports and a development `auth`, is [templates/main.go.tmpl](../templates/main.go.tmpl) (`dedo init` fills it in). Add a policy-table test ([templates/policy_table_test.go.tmpl](../templates/policy_table_test.go.tmpl)) so a missing declaration fails `go test` before deploy.

```sh
make generate
make run
```

## 5. Guidance for coding agents

Append [claude.md](claude.md) to your `AGENTS.md` or `CLAUDE.md` (`dedo templates claude.md >> AGENTS.md`). `dedo init` does this where the file lacks it, and creates `AGENTS.md` when neither exists.

## Adding dedo to an existing Buf repository

`dedo init` adds to what is there and changes nothing you rely on. What it does depends on the repository:

| The repository | What `dedo init` does |
| --- | --- |
| A Go module with protos and `buf.gen.yaml` | Keeps your generators, including Buf's remote `protocolbuffers/go` and `connectrpc/go` plugins, and adds only dedo's two, writing to the same `out` with the same `paths=` option. Reads each file's `go_package`, or managed mode's `go_package_prefix`, for the Go names in `main.go`. Leaves managed mode as it is. Warns when `connectrpc/go` is pinned to a release that generates connect-go v1 code, which dedo's server cannot register. |
| A Go module with protos, no `buf.gen.yaml` | Writes `buf.gen.yaml` with managed mode and four generators run through `go tool`. |
| Protos and `buf.yaml`, but no Go module (a schema repository) | Adds the options module, pinned to the release, and the lint plugin, as in any repository. It replaces the BSR plugin (`buf.build/r-a/dedo-lint`) that earlier releases listed here, which cannot read `rules.yml`. Nothing that needs Go is added; instead it appends [generators.md](generators.md) to `AGENTS.md`, which tells coding agents how to add dedo's generators and runtime to the repository's own tooling and server. For a new service, run `go mod init` and `dedo init` again. |
| A Go module without protos | Writes a starter service that passes lint. |

In `buf.yaml` it adds the deps, the `DEDO` category and the plugin, and in `rules.yml` the rule sets that run. A `buf.yaml` from a release before v0.15.0 chose the plugin's rules itself: `dedo init` and `dedo upgrade` move that choice to `rules.yml`, its sets to `use` and its exceptions to `disable`, and drop the ones dedo now skips by itself. It keeps your own deps, Buf's categories and exceptions, plugins, and comments, in both files. It needs `buf.yaml` version v2: run `buf config migrate` first if yours is v1.

Your existing services declare nothing for dedo yet, and `dedo init` names them. Until they do, `buf lint` lists what each RPC needs, and the policy-table test and the server's startup fail on purpose, naming the first missing declaration. Declare one service at a time; [guideline.md](guideline.md#3-write-the-contract) shows a complete one.

`main.go` and `server.go` are written only if you have none, and only in a `package main` root. A library repository keeps its package.

## Upgrading

```sh
dedo update     # installs the latest dedo
dedo upgrade    # moves this repository to it
```

`dedo update` replaces the `dedo` command and changes no repository. `dedo upgrade`, run in each repository, moves everything to the release of the `dedo` you run, as one version:

- the generators and the runtime in `go.mod`; the lint plugin installed into `$GOBIN` at the release and copied into `.tools/`;
- the options module's label in `buf.yaml`, then `buf.lock` (`buf dep update`); in a schema repository, the lint plugin installed into `$GOBIN` and copied into `.tools/` at that release;
- `Makefile.api`, and plugin entries and generated files that earlier releases named differently;
- the generated code (`buf generate`) and `go.mod` (`go mod tidy`);
- from connect-go v1 (dedo before v0.11.0): connect-go's own `connect-go-v2-migrate`, before and after regenerating, rewrites handlers and clients. It lists what it cannot rewrite; the wiring in `main.go` (one `connect.Server`, `connecthttp.Mount`, `vanguard.Mount`, auth as an interceptor) is printed beside yours to copy.

It keeps your `main.go` and `server.go`, and prints dedo's current `main.go` beside yours to compare. `dedo update -version vX.Y.Z` installs a given release instead of the latest.

## Uninstalling

```sh
dedo uninstall  # removes dedo and buf-plugin-dedo-lint from $GOBIN
```

It changes no repository. Each keeps its copy of the plugin and the fetched house rules in `.tools/`, ignored by git; `rm -rf .tools/` removes them. To take dedo out of a repository, revert what `dedo init` wrote: `buf.yaml`, `rules.yml`, the Go tools in `go.mod`, `Makefile.api` and the guidance in `AGENTS.md`.
