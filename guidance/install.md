# Install dedo

Each numbered section below adds one part of dedo to a repository. `dedo init` does all of them, editing what is already there:

```sh
go install github.com/raveesh-me/dedo/cmd/dedo@v0.33.0
dedo init
```

It never removes your own deps, plugins or generators, and running it again changes nothing that is in place. In a module with no protos yet, it writes a starter service that passes lint, with a handler stub in its module's package, so the first lint, generate and build all succeed. If one of its `go`, `buf` or `make` commands fails, it runs the rest and lists what to fix. It ends by writing a `main.go` that serves every service, or, when you have one, printing dedo's to compare. `dedo init -offline` edits the files only and prints the `go`, `buf` and `make` commands instead of running them.

New to dedo? [tutorial.md](tutorial.md) builds a whole service from an empty directory. Already on Buf? Read [Adding dedo to an existing Buf repository](#adding-dedo-to-an-existing-buf-repository) first.

You need Go 1.26 or later and the [Buf CLI](https://buf.build/docs/installation).

## 1. Annotations: the options module

Add dedo's options module, and the two modules it builds on, to `buf.yaml`:

```yaml
version: v2
deps:
  - buf.build/googleapis/googleapis
  - buf.build/bufbuild/protovalidate
  - buf.build/r-a/dedo:v0.33.0   # the same release as the lint plugin and runtime
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

Without the `dedo` command, `go install github.com/raveesh-me/dedo/lint/cmd/buf-plugin-dedo-lint@v0.33.0` and a copy into `.tools/` do the same. It runs natively because Buf starts the plugin about a dozen times per lint; through `go tool` each lint would take 2 s instead of 0.3 s. Run `dedo tools` again after a fresh clone, a version change, or any change to `dedo.yaml`'s rules or its imports.

List the plugin and its two categories in `buf.yaml`, `DEDO` for its lint rules and `DEDO_BREAKING` for its breaking rules, and choose its rules in the `lint` and `breaking` sections of `dedo.yaml` next to it ([templates/dedo.yaml](../templates/dedo.yaml); `dedo templates dedo.yaml` prints it). `buf.yaml` names no other rule of the plugin, in `use` or `except`: dedo reads only `dedo.yaml` ([RULES.md](RULES.md#choosing-the-rules) says how it chooses):

```yaml
# buf.yaml
lint:
  use:
    - STANDARD
    - COMMENTS
    - DEDO
breaking:
  use:
    - FILE
    - DEDO_BREAKING
plugins:
  - plugin: .tools/buf-plugin-dedo-lint
```

```yaml
# dedo.yaml
lint:
  use:
    - AIP_NOLANG
    - ZALANDO
    - DEDO
    - HOUSE
# breaking: without use, DEDO and HOUSE run
```

```sh
dedo templates Makefile.api > Makefile.api && printf 'include Makefile.api\n' >> Makefile
make check          # buf format, buf lint, buf breaking against main, dedo modules
```

The breaking check compares with the `main` branch, and is skipped while `main` has no `.proto` files. It runs Buf's `FILE` rules and dedo's [breaking rules](RULES.md#breaking-rules), which catch what `FILE` cannot see: an RPC that drops a scope, a REST path or an event type, a field that becomes required, a resource pattern that goes. Compare with another branch with `make check API_BASE=develop`. `make check` also runs `make modules`, which is `dedo modules` (below). The targets of `Makefile.api` are `run`, `generate`, `check`, `test`, `tools`, `modules`, `docs`, `docs-tunnel` and `docs-static`; where your `Makefile` already has one of those names, `dedo init` names dedo's `api-<name>` instead, and `api-check`, `api-generate` and `api-test` always exist, for CI.

`make docs` serves the REST reference, only `docs/index.html` and `docs/openapi.json`, on `127.0.0.1:8099` (`DOCS_ADDR` to change it). `make docs-tunnel` also shares it at a public `trycloudflare.com` URL until Ctrl-C, to show the API to someone off your machine; it needs [cloudflared](https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/downloads/). It never shares the service itself: development tokens are often public.

`make docs-static` writes the reference as one HTML file, `.tools/docs.html` (`DOCS_STATIC` to change it), that opens from disk with no network: to read on a plane, or to send to someone. It is `dedo docs -static FILE`. The OpenAPI document goes in place of the page's `url: '/openapi.json'`. Each script the page loads goes in too, as a `data:` URL, but only an `https` script with an `integrity` hash, and only once its bytes match that hash. dedo keeps each script it fetches in your user cache directory, under `dedo/scripts`, so after the first run new builds need no network either. The file holds the whole API reference, so share it only where you would share the API.

Your organisation's own rules go in `dedo.yaml`'s `lint` section, and its breaking rules in its `breaking` section, as CEL checks that can import shared rules from git repositories, or from files published over HTTPS and pinned by their SHA-256, which CI fetches without access to a private repository ([RULES.md](RULES.md#house-rules), *Published rules*); `HOUSE` in a section's `use` runs them, and runs by default. [templates/dedo.example.yaml](../templates/dedo.example.yaml) shows every key. A house rule can override one of dedo's rules of its kind by naming it in `overrides`, or lock itself with `final: true`; `dedo tools` lists every override and final rule in force. [RULES.md](RULES.md#house-rules) has the format, and [Precedence](RULES.md#precedence) the order.

If your services partition their data between tenants, declare `tenancy` on each and add `DEDO_MULTI_TENANT` to `dedo.yaml`'s `lint.use` (`dedo init` does when a service declares it); then pass `Tenants` to the interceptor in section 4. [RULES.md](RULES.md#multi-tenant-rules) explains the model.

Repositories without a Go module, such as a TypeScript service or a schema repository, set the plugin up the same way. They need Go on the machine and in CI, to install it, but no `go.mod`. In CI:

```sh
go install github.com/raveesh-me/dedo/cmd/dedo@v0.33.0
dedo tools          # installs the plugin at buf.yaml's release, copies it into .tools/, fetches the house rules
buf lint
```

The Wasm plugin on the BSR (`buf.build/r-a/dedo-lint`) runs without Go but cannot read `dedo.yaml`, so it runs dedo's default rule sets and no house rules; `dedo init` replaces it. A platform team that must lint without Go can build a plugin with `dedo.yaml`'s rules built in, `dedo tools -wasm house-lint.wasm`, push it to the BSR (`buf plugin push buf.build/<org>/house-lint --binary=house-lint.wasm`), and list that plugin instead.

## 3. Code generators

Pin the generators as Go tools:

```sh
go get -tool google.golang.org/protobuf/cmd/protoc-gen-go
go get -tool connectrpc.com/connect/v2/cmd/protoc-gen-connect-go@v2.0.1
go get -tool github.com/raveesh-me/dedo/mcp/cmd/protoc-gen-dedo-mcp@v0.33.0
go get -tool github.com/raveesh-me/dedo/openapi/cmd/protoc-gen-dedo-openapi@v0.33.0
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
  # The OpenAPI 3.2 document for the REST reference.
  - local: ["go", "tool", "protoc-gen-dedo-openapi"]
    out: docs
    strategy: all
    opt:
      - title=Your API
      - server=https://api.example.com
```

Both dedo generators take `min_audience`: an `Audience` value, with or without its `AUDIENCE_` prefix, such as `EXTERNAL_PARTNER`; an unknown value fails the run. They leave out every service whose `audience` is narrower, and keep one that declares none, for lint to report. Without it, they leave out only `AUDIENCE_COMPONENT_INTERNAL` services, which only a [backend for frontend](domains.md#backends-for-frontends) declares. A reference published to everyone sets `EXTERNAL_PUBLIC`, so internal services, such as the reads a BFF makes, stay out of it:

```yaml
    opt:
      - title=Your API
      - server=https://api.example.com
      - min_audience=EXTERNAL_PUBLIC
```

A service `protoc-gen-dedo-mcp` leaves out still gets its `<Service>MCPTools()`, returning no tools, so `main.go` compiles unchanged.

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
go get github.com/raveesh-me/dedo@v0.33.0 github.com/raveesh-me/dedo/mcp@v0.33.0
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

Once these files exist, `dedo upgrade` leaves them alone. When one differs from the release's template, other than in the page's `<title>`, it says so and prints the `dedo templates` command that refreshes it. A page set up before the move to Scalar is one of these, and sets its `<title>` again after the refresh.

Each service's handlers live in its module's package, `internal/<module>/` ([domains.md](domains.md#code-and-data-follow-the-modules)): the module of `acme.time_counter.v1` is `time_counter`. `dedo init` writes a stub there for each service, `internal/time_counter/counter_service.go`, whose type answers Unimplemented until you write the methods (`dedo templates server.go.tmpl` prints it). A second version of a module, `acme.time_counter.v2`, is the same module: its stub is `CounterServiceV2` in the same package.

Then wire everything in `main.go`, the composition root: the only package, with `app/` if you add one, that imports several modules. `dedo init` writes it with your names filled in, registering every service (or prints it, when you have a `main.go` already); here `CounterService` from `acme.time_counter.v1`:

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
	// Each module's handlers, from internal/<module>.
	time_counterv1connect.RegisterCounterServiceHandler(server, time_counter.NewCounterService())

	mux := http.NewServeMux()
	// Connect, gRPC and gRPC-Web, then REST from the google.api.http bindings.
	connecthttp.Mount(mux, server)
	if err := vanguard.Mount(mux, server); err != nil {
		return nil, err
	}

	// MCP: every RPC is a tool, as protoc-gen-dedo-mcp generated it, called
	// in-process on the same server with the caller's token.
	tools := mcp.NewServer(&mcp.Implementation{Name: "time-counter", Version: "v1"}, nil)
	client := connect.NewClient(connectinprocess.New(server))
	if err := mcpserver.AddTools(tools, client, time_counterv1.CounterServiceMCPTools()...); err != nil {
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

Auth is a server interceptor (`auth` in the template), not HTTP middleware, so it covers MCP calls, which reach the server in-process, as well as Connect, gRPC and REST. The complete file, with `main`, imports and a development `auth`, is [templates/main.go.tmpl](../templates/main.go.tmpl) (`dedo init` fills it in). Add a policy-table test ([templates/policy_table_test.go.tmpl](../templates/policy_table_test.go.tmpl)), which builds every service's table, so a missing declaration fails `go test` before deploy.

### Publishing events

Most services publish no events; skip this until a command declares one in `emits` ([domains.md](domains.md#domain-events-outward-only)). The handler then publishes through `events.Publisher`, from `github.com/raveesh-me/dedo/events`, which the root module already holds.

The bus is a driver: anything that implements `driver.Driver` (`github.com/raveesh-me/dedo/events/driver`), one method that hands an event to Pub/Sub, Kafka, NATS or an HTTP sink and returns nil once the bus holds it durably. dedo ships no broker driver, only `eventstest.Driver` for tests. Build the publisher in `main.go` and inject it into the handlers that publish:

```go
	pub, err := events.New(events.Config{
		Driver: bus,                         // your driver.Driver
		Source: "//time-counter.example.com", // names this service in every event
		Log:    log,
	})
	if err != nil {
		return nil, err
	}
	time_counterv1connect.RegisterCounterServiceHandler(server, time_counter.NewCounterService(pub))
```

In the handler, publish after the change commits, and return the error if the bus did not take the event, so the caller retries:

```go
	if _, err := s.events.Publish(ctx, "acme.time_counter.counter.reset", counter.GetName(),
		events.IdempotencyKey(req.GetRequestId())); err != nil {
		return nil, err // UNAVAILABLE, EVENT_NOT_PUBLISHED; declare it in the RPC's errors
	}
```

`events.IdempotencyKey` is optional. With the request's `request_id` (AIP-155), the events a retried command publishes all carry the same key, though each has an ID of its own, so a consumer can drop the repeats.

`Publish` refuses, with `INTERNAL`, a type the RPC does not declare and a call outside an RPC: it reads the RPC's contract from the context the policy interceptor sets. It retries with exponential backoff and jitter until the driver acknowledges the event, the driver marks an error `driver.Permanent`, or the request's deadline passes; `events.Retry` sets the backoff and `Timeout` bounds requests without a deadline (5s by default). The event's ID stays the same on every retry, and the request's flow ID goes with it as the `flowid` extension. In tests, give the handler a publisher over `eventstest.New()` and read what it published with `Events()`.

### Keeping modules apart: `dedo modules`

`dedo modules`, run in the repository (`make modules`; `make check` runs it), checks the layout of [domains.md](domains.md#code-and-data-follow-the-modules). It takes the modules from the repository's versioned proto packages, `<context>.<m>.v<n>`, leaving out the protos inside a nested Go module, and the Go module path from `go.mod`, and loads the import graph with `go list`. It fails on:

- a cycle between modules, through an import of `internal/<m>/**` or of the code generated from `<m>`'s packages, messages or Connect clients, any version; it names the imports that close the cycle;
- a shared package, `internal/shared/**` or the code generated from an unversioned package such as `<context>.type`, that imports a module;
- a directory named after a module anywhere but `internal/<m>` and the trees that mirror the protos (`proto/`, `gen/`), and a directory in `internal/` that is neither a module nor `shared`;
- SQL, in a module's or shared `.sql` files or in Go string literals outside generated code, that names another module's schema, quoted or not; shared SQL may name no module's schema.

A [backend for frontend](domains.md#backends-for-frontends) (BFF) is a directory with its own `go.mod` that holds a module path of `buf.yaml`, such as `bff/` holding `bff/proto`. `dedo modules` finds it from that layout, with no setting in `dedo.yaml`, and also fails on:

- a buf module inside a nested Go module that has a `name`, which `buf push` would publish;
- an `AUDIENCE_COMPONENT_INTERNAL` service in a named module, and a service in the BFF's module that declares another audience or none;
- a database driver anywhere in the BFF's dependencies outside tests, however it is reached: `database/sql` (but not `database/sql/driver`, which value types implement), `pgx`, `lib/pq`, `go-sql-driver/mysql`, `gorm.io`, `mattn/go-sqlite3`, `modernc.org/sqlite`, the MongoDB driver or `go-redis`; it names the BFF package the dependency enters through and the imports that lead to the driver;
- a BFF package that imports any package of the backend's Go module other than those generated from its protos: `internal/**`, `app/**`, `cmd/**`, the root's `main` or any other;
- a backend package that imports the BFF.

On success it also prints each BFF after the module graph: `bff: bff (frontend; not a backend module)`.

It finds generated packages by the `source:` line protoc plugins write at the top of each file, so it works wherever `buf.gen.yaml` writes them, and leaves tests out: a test may import the composition root or another module's fakes. On success it prints each module and the modules it depends on:

```text
dedo modules: 2 modules in acme, no cycles; each depends on:
  catalog  (nothing)
  orders   -> catalog
```

Events add no edges: a service publishes them outward only and never subscribes, so an event type is a string in the contract, not a dependency between modules.

```sh
make generate
make run
```

## 5. Guidance for coding agents

`dedo init` adds [claude.md](claude.md) to your `AGENTS.md` and `CLAUDE.md`, creating `AGENTS.md` when neither exists, as a block between `<!-- dedo:begin guidance … -->` and `<!-- dedo:end guidance -->` lines, and [domains.md](domains.md) after it as a `domains` block. `dedo upgrade` rewrites the blocks from the new release, so the guidance moves with dedo; keep your own notes outside them. Guidance a release before v0.20.0 appended without those lines becomes the block on the next `dedo init` or `dedo upgrade`.

Your organisation's own guidance for coding agents, such as the headers its platform requires, goes in `dedo.yaml`'s `annexure`: a Markdown file published over HTTPS, pinned by its SHA-256 like a `url` import (`dedo upgrade` prints the sum when it is missing), or one in the repository:

```yaml
# dedo.yaml
annexure:
  - url: https://acme.github.io/api-standards/v3/AGENTS.md
    sha256: 9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08
  - path: docs/agents.md
```

`dedo init` and `dedo upgrade` add each annexure after dedo's guidance, in a block of its own, rewrite it when its source changes, and remove it when `dedo.yaml` no longer names it. dedo's guidance tells agents that an annexure wins where the two conflict. With `-offline`, a url annexure keeps the copy already there.

## Adding dedo to an existing Buf repository

`dedo init` adds to what is there and changes nothing you rely on. What it does depends on the repository:

| The repository | What `dedo init` does |
| --- | --- |
| A Go module with protos and `buf.gen.yaml` | Keeps your generators, including Buf's remote `protocolbuffers/go` and `connectrpc/go` plugins, and adds only dedo's two, writing to the same `out` with the same `paths=` option. Reads each file's `go_package`, or managed mode's `go_package_prefix`, for the Go names in `main.go`. Leaves managed mode as it is. Warns when `connectrpc/go` is pinned to a release that generates connect-go v1 code, which dedo's server cannot register. |
| A Go module with protos, no `buf.gen.yaml` | Writes `buf.gen.yaml` with managed mode and four generators run through `go tool`. |
| Protos and `buf.yaml`, but no Go module (a schema repository) | Adds the options module, pinned to the release, and the lint plugin, as in any repository. It replaces the BSR plugin (`buf.build/r-a/dedo-lint`) that earlier releases listed here, which cannot read `dedo.yaml`. Nothing that needs Go is added; instead it appends [generators.md](generators.md) to `AGENTS.md`, which tells coding agents how to add dedo's generators and runtime to the repository's own tooling and server. For a new service, run `go mod init` and `dedo init` again. |
| A Go module without protos | Writes a starter service that passes lint. |

In `buf.yaml` it adds the deps, the `DEDO` and `DEDO_BREAKING` categories (keeping the breaking rules `breaking.use` already chose) and the plugin, and in `dedo.yaml` the rule sets that run. A `buf.yaml` from a release before v0.15.0 chose the plugin's rules itself: `dedo init` and `dedo upgrade` move that choice to `dedo.yaml`, its sets to `lint.use` and its exceptions to `lint.disable`, and drop the ones dedo now skips by itself. A repository set up before v0.20.0 kept these in `rules.yml`: `dedo init` and `dedo upgrade` move it into `dedo.yaml`, with your comments, and delete it. A `dedo.yaml` from before v0.32.1 kept them in one `rules` section: `dedo upgrade` renames it `lint` and moves its `imports` to the top of the file, where they bring breaking rules too. The header an earlier `dedo init` wrote at its top goes, since `dedo.yaml`'s own comment replaces it. Where there is no `dedo.yaml`, the lint plugin still reads `rules.yml`. It keeps your own deps, Buf's categories and exceptions, plugins, and comments, in both files. It needs `buf.yaml` version v2: run `buf config migrate` first if yours is v1.

Your existing services declare nothing for dedo yet, and `dedo init` names them. Until they do, `buf lint` lists what each RPC needs, and the policy-table test and the server's startup fail on purpose, naming the first missing declaration. Declare one service at a time; [guideline.md](guideline.md#3-write-the-contract) shows a complete one.

`main.go`, `policy_table_test.go` and the handler stubs in `internal/<module>/` are written only where you have none, and only in a `package main` root. A library repository keeps its package. A repository that keeps its handlers in a root `server.go` (`newServer`, as `dedo init` wrote before v0.28.0), or whose `main.go` builds its services without dedo's stubs, gets no stubs; move the handlers into their modules as [domains.md](domains.md#code-and-data-follow-the-modules) lays out.

## Adding a BFF: `dedo init -bff`

`dedo init -bff`, run in a repository `dedo init` set up (or with `-dir`), adds a [backend for frontend](domains.md#backends-for-frontends):

```text
bff/
  go.mod               # a Go module of its own, <module>/bff, requiring the backend's from ../
  buf.gen.yaml         # Go and Connect for the BFF into bff/gen/go; the frontend's generator as a commented example
  proto/               # an unnamed buf module
    <context>/web/v1/web.proto  # a starter AUDIENCE_COMPONENT_INTERNAL service, WebService
  main.go              # serves it with github.com/raveesh-me/dedo/bff
  web_service.go       # WebService's handler, holding a client of the backend's first service
```

It adds `bff/proto` to `buf.yaml`'s modules with no `name` and a `breaking` section of its own that ignores it, so the repository's rules lint it, `buf push --exclude-unnamed` never publishes it, and `buf breaking` never checks it. It writes the starter proto only when `bff/proto` has none, and keeps any of the other files that exist. When the repository has a `frontend/` directory, it adds `ignore ./frontend` to the backend's `go.mod`, so `./...` leaves the frontend out. It then runs `buf generate --template bff/buf.gen.yaml` from the repository root and `go mod tidy` in `bff/`; with `-offline` it prints them. Uncomment the frontend's generator in `bff/buf.gen.yaml`, such as `protoc-gen-es`, and point its `out` at the frontend's sources.

Build the BFF with its frontend's commit, which `main.go` refuses to start without, and run it with `BACKEND_URL` set to the backend's address:

```sh
cd bff && go build -ldflags "-X main.buildSHA=$(git rev-parse --short=12 HEAD)" .
```

`main.go` uses the runtime package `github.com/raveesh-me/dedo/bff`:

- **`bff.Pin(sha, prefix, next)`** serves `next` only under `<prefix>/<sha>/`, its own commit SHA of `bff.SHALength` (12) lowercase hexadecimal characters (`bff.CheckSHA`), and answers any other SHA, or none, with `FAILED_PRECONDITION`, a `google.rpc.ErrorInfo` whose reason is `bff.ReasonVersionRetired` (`BFF_VERSION_RETIRED`) and `Cache-Control: no-store`, on which the frontend reloads. `main.go`'s prefix is `/bff`.
- **`bff.Forward(headers...)`** is a Connect client interceptor for the clients the BFF calls the backend and other contexts with. It copies the caller's credentials onto every call: `Authorization`, `X-Flow-ID` and the headers it is given.
- **`bff.Activity`** (`bff.NewActivity(sha)`) counts the requests its `Middleware` serves, distinct callers (by a hash of `Authorization`, never the token) and the first and last request, and its `Handler` serves them as JSON, for the job that retires a previous version. `main.go` serves it at `GET /activity`, beside `GET /healthz`, both outside the prefix.

`make check`'s `dedo modules` then keeps the BFF to [its rules](#keeping-modules-apart-dedo-modules). `dedo init -bff` refuses a repository with no `go.mod` or `buf.yaml`, a `buf.yaml` that is not v2, and a backend that has a module `web` already; for that one, write the BFF's protos into `bff/proto` first.

## Upgrading

```sh
dedo update     # installs the latest dedo
dedo upgrade    # moves this repository to it
```

`dedo update` replaces the `dedo` command and changes no repository. `dedo upgrade`, run in each repository, moves everything to the release of the `dedo` you run, as one version:

- the generators and the runtime in `go.mod`; the lint plugin installed into `$GOBIN` at the release and copied into `.tools/`;
- the options module's label in `buf.yaml`, then `buf.lock` (`buf dep update`); in a schema repository, the lint plugin installed into `$GOBIN` and copied into `.tools/` at that release;
- `Makefile.api`, and plugin entries and generated files that earlier releases named differently;
- a handler stub in `internal/<module>/` for each service that has none, when `main.go` is dedo's;
- the generated code (`buf generate`) and `go.mod` (`go mod tidy`);
- from connect-go v1 (dedo before v0.11.0): connect-go's own `connect-go-v2-migrate`, before and after regenerating, rewrites handlers and clients. It lists what it cannot rewrite; the wiring in `main.go` (one `connect.Server`, `connecthttp.Mount`, `vanguard.Mount`, auth as an interceptor) is printed beside yours to copy.

It keeps your `main.go`, your handlers and any root `server.go`, and prints dedo's current `main.go` beside yours to compare, naming any service yours does not register. `dedo update -version vX.Y.Z` installs a given release instead of the latest.

## Uninstalling

```sh
dedo uninstall  # removes dedo and buf-plugin-dedo-lint from $GOBIN
```

It changes no repository. Each keeps its copy of the plugin and the fetched house rules in `.tools/`, ignored by git; `rm -rf .tools/` removes them. To take dedo out of a repository, revert what `dedo init` wrote: `buf.yaml`, `dedo.yaml`, the Go tools in `go.mod`, `Makefile.api` and the guidance in `AGENTS.md`.
