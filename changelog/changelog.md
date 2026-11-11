# Changelog

What each dedo release changed, newest first. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow [semantic versioning](https://semver.org/spec/v2.0.0.html). Before v1, a minor release may break things: every break is listed under **Breaking**, with what to do about it.

A release is one version of everything: the options module (`buf.build/r-a/dedo`), the lint plugin, the generators, the Go modules (`github.com/raveesh-me/dedo/...`) and the `dedo` command. `dedo update && dedo upgrade` moves a repository to the latest one.

Version bumps, dependency updates and changes to benchmarks or examples alone are left out. Each release's [GitHub page](https://github.com/raveesh-me/dedo/releases) lists every pull request it merged.

## [Unreleased]

### Breaking

- `DEDO_NO_HEALTH_RPC`, in `DEDO`: the contract declares no health check. It flags a service, RPC or message named `Health`, `HealthCheck`, `Healthz`, `Ping`, `Liveness` or `Readiness` (a message also with a `Request` or `Response` suffix, or starting with the word `HealthCheck`, but not a domain's `HealthCheckup`), `Check`, `Watch` and `List` on a health service, and any import of `grpc/health/v1/health.proto`. Health is the server's: buf's check plugins cannot warn, so a contract with a health check now fails lint; a service keeping one for now disables the rule in `dedo.yaml`. [#137](https://github.com/raveesh-me/dedo/pull/137)

### Added

- `dedo init`'s `main.go` serves `GET /healthz` (204) beside `/docs`, outside the interceptors, as the BFF's already did. A server written before this release adds the route itself. [#137](https://github.com/raveesh-me/dedo/pull/137)

### Fixed

- A backend for frontend builds against dedo's build of the MCP Go SDK, as its backend does: `dedo init -bff` and `dedo upgrade` write and maintain the `replace` of `github.com/modelcontextprotocol/go-sdk` in its `go.mod`, such as `bff/go.mod`, and remove it with the backend's once upstream has the fix. Go reads a `replace` only from the `go.mod` of the module it builds, so a BFF, which links the backend's generated `*.mcp.pb.go`, built against the upstream release unless the line was copied by hand. After `dedo upgrade` adds the line, run `go mod tidy` in the BFF. [#144](https://github.com/raveesh-me/dedo/pull/144)

## [v0.36.0] - 2026-11-11

### Added

- House rules look across files: `image`, on every kind of element, holds each file, service and method buf passes, the module's own and its imports', keyed by path or full name, each with the variables a rule on it reads, `options` included, and `imported`. A rule can check that every method a call names exists and has one scope, or that a permission grants only scopes some method declares. It is built once per run, and only when a check reads it. A breaking rule reads the current version's. [#139](https://github.com/raveesh-me/dedo/pull/139)

## [v0.35.0] - 2026-11-06

### Added

- House rules read options: `options`, on every kind of element, holds each extension set on its options by full name, such as `options['dedo.v1.method'].scopes` or an organisation's own `options['acme.auth.v1.service'].tenant`, resolved from the files buf passes with the module's imports, so options the plugin is not built with read too. Messages are maps of their fields by `.proto` name and enums their value names; an unset message field or oneof member is left out for `has()`. A method also reads its service's options as `service_options`, such as `service_options['dedo.v1.service'].audience`, and a breaking rule reads the earlier element's as `against.options`. [#135](https://github.com/raveesh-me/dedo/pull/135)

## [v0.34.0] - 2026-11-06

### Breaking

- `protoc-gen-dedo-openapi` and `protoc-gen-dedo-mcp` leave out `AUDIENCE_COMPONENT_INTERNAL` services by default: they have no paths, tags or schemas in the reference, and their `<Service>MCPTools()` returns no tools, so `main.go` still compiles. A service that declares no audience is kept. To document such a service anyway, add `min_audience=COMPONENT_INTERNAL` to the generator's `opt`. The reference now names a schema by its short name whenever no other message the documented services reach has that name, so `examples/contacts` reads `Location` where it read `raveesh_me.contacts.v1.Location`; regenerate and update any link to the old name. [#129](https://github.com/raveesh-me/dedo/pull/129)

### Added

- `min_audience` for `protoc-gen-dedo-openapi` and `protoc-gen-dedo-mcp`: an `Audience` value, with or without its `AUDIENCE_` prefix, below which services are left out, such as `min_audience=EXTERNAL_PUBLIC` for a reference published to everyone. An unknown value fails the run. `templates/buf.gen.yaml` shows it commented out. [#129](https://github.com/raveesh-me/dedo/pull/129)
- `dedo modules` finds a backend for frontend (BFF), a nested Go module that holds a `buf.yaml` module such as `bff/proto`, and fails on: a name on its buf module; a service in it that is not `AUDIENCE_COMPONENT_INTERNAL`, or one that is in a named module; a data-store client anywhere in its dependencies, from one table of client families by import path (SQL drivers and ORMs, MongoDB, Redis including `go-redis/redis` v6 to v8 and `redigo`, DynamoDB, Firestore, Datastore, Spanner, Bigtable, BigQuery, Cassandra, Elasticsearch, OpenSearch and others); a dependency on the backend's own packages other than those generated from its protos, however it is reached, such as through a module nested under the backend's path; and a backend package importing it. It prints `bff: <dir> (frontend; not a backend module)` after the module graph. [#129](https://github.com/raveesh-me/dedo/pull/129)
- `dedo init -bff` adds a BFF to a repository `dedo init` set up: `bff/go.mod`, an unnamed `bff/proto` module in `buf.yaml` that `buf breaking` ignores, `bff/buf.gen.yaml` for its Go and Connect code and, commented out, the frontend's `protoc-gen-es`, a starter `AUDIENCE_COMPONENT_INTERNAL` service, its handler and `main.go`, and `ignore ./frontend` in `go.mod` when there is a `frontend/`. When `bff/proto` has protos already, `main.go` registers the services they declare, each with a handler, and `bff/buf.gen.yaml` gives each of their directories the BFF's Go prefix; a backend with a module `web` is refused only while the starter would take that package. It warns when the root `buf.gen.yaml` has no `inputs`, or inputs that hold `bff/proto`. [#129](https://github.com/raveesh-me/dedo/pull/129)
- Package `bfftools`, in `bff-tools/`, serves a BFF paired with its frontend by commit SHA: `Pin` answers only `<prefix>/<sha>/` and any other SHA with `FAILED_PRECONDITION` and the `google.rpc.ErrorInfo` reason `BFF_VERSION_RETIRED` (`ReasonVersionRetired`), on which the frontend reloads; `Forward` copies the caller's `Authorization`, flow ID and named headers onto the BFF's calls; `Activity` writes one structured `log/slog` line per request (`bff request`, with `sha`, `procedure` and `caller`), the durable record the job that retires a version reads, with a caller key from `ActivityOptions.CallerKey` (such as user and location from token claims or tenancy headers; by default `AuthorizationHash`, and none for a request without `Authorization`), and counts requests, distinct callers (at most `MaxCallers`, 10,000) and the first and last request per process for `GET /activity`. `BuildSHACommand`, `git rev-parse HEAD | cut -c1-12`, prints the 12-character SHA that pairs a frontend and its BFF. [#129](https://github.com/raveesh-me/dedo/pull/129)
- Guidance on backends for frontends, in `guidance/domains.md` (with checklist items), `install.md`, `claude.md`, `guideline.md` and `generators.md`: a nested Go module with an unnamed buf module, linted with the repository's rules and never checked for breaking changes; owning no data and holding no credentials; reading its context through internal-audience services, which check tokens and scopes and change additively; paired with its frontend by commit and released BFF first; the frontend's entry file purged (or served `no-cache`) on every release, so a reload never loops on a retired version; the previous BFF retired after a fixed window, 7 days recommended, from its per-SHA activity log. [#129](https://github.com/raveesh-me/dedo/pull/129)
- `dedo breaking` runs `buf breaking` for each module of `buf.yaml` against its own directory on a branch (`-against`, default `main`), and skips a module with no protos there yet, such as one the branch adds. [#129](https://github.com/raveesh-me/dedo/pull/129)

### Changed

- `templates/Makefile.api`: `check` runs `go tool dedo breaking -against $(API_BASE)` in place of `buf breaking` over the whole workspace and, when `bff/` exists, checks that `bff/buf.gen.yaml`'s output is current and vets and tests `bff/`; `test` also tests `bff/`, and `generate` also generates its code. `templates/Jenkinsfile.api-stage` runs `dedo breaking`, `dedo modules` and the same `bff/` steps, and `guideline.md`'s CI table lists every step. `dedo upgrade` rewrites `Makefile.api`; copy the Jenkins stage again with `dedo templates Jenkinsfile.api-stage`. [#129](https://github.com/raveesh-me/dedo/pull/129)

### Fixed

- The breaking check of `Makefile.api` and the Jenkins stage, which compared the whole workspace, failed every pull request that added a buf module with a `breaking` section of its own, such as the one `dedo init -bff` adds (`input contained 2 images, whereas against contained 1 images`). [#129](https://github.com/raveesh-me/dedo/pull/129)
- `dedo init`, `dedo upgrade` and `dedo modules` leave out the protos of a Go module nested in the repository, such as a BFF's: they no longer count them as backend modules, write handler stubs or `main.go` registrations for their services, or take their directory as `buf.yaml`'s proto root. [#129](https://github.com/raveesh-me/dedo/pull/129)

## [v0.33.0] - 2026-11-05

### Breaking

- Lint rule `DEDO_DOCS_NO_CODE_STRUCTURE`: the reference speaks the domain's language, not the code's. A service's heading (its title, else its name without `Service`) says no *service*, *module*, *aggregate* or *microservice*, unless a resource it manages is named with the word; and no comment, tool text or message description names a proto service, such as `OrderService`. Comments in Go's style, `// OrderService reads orders.`, now fail: start with what the API does (`// Reads orders.`). Give a service a `title` rather than renaming it, which would change its Connect and gRPC paths. `dedo init`'s starter, the examples and `guidance/domains.md` follow it. [#127](https://github.com/raveesh-me/dedo/pull/127)

## [v0.32.3] - 2026-11-05

### Fixed

- `dedo upgrade` keeps the commented-out `disable` and `rules` examples that `dedo init` wrote, when it moves a `dedo.yaml` from before v0.32.1 to the `lint` section. YAML kept them on the `imports` key, so they left with it. [#125](https://github.com/raveesh-me/dedo/pull/125)

## [v0.32.2] - 2026-11-05

The release of the breaking rules, **DEDO BREAKING RULES**: the same code as v0.32.1, released in full.

### Fixed

- v0.32.1 was tagged on a commit whose examples required `cel.dev/cel-go` v0.32.1, which does not exist, so its release job stopped before publishing the options module to the BSR (`buf.build/r-a/dedo:v0.32.1`) and the plugin binaries. Every Go module retracts v0.32.1; use v0.32.2. [#123](https://github.com/raveesh-me/dedo/pull/123)

## [v0.32.1] - 2026-11-05

### Breaking

- `dedo.yaml` has a `lint` section in place of `rules`, a `breaking` section beside it, and `imports` at the top of the file, since an import brings both kinds of rules. A shared rules file (`rules.yml`) has the same `imports`, `lint` and `breaking`, with no rules at its top. `dedo upgrade` moves a repository's own `dedo.yaml`, keeping its comments; a shared rules file in the old layout fails to load, saying where its keys go, until its owners publish it anew. [#121](https://github.com/raveesh-me/dedo/pull/121)
- `rules.Load` returns a `*rules.Plugin` with the lint and breaking rule sets, and `config.Config` holds a `Selection` for each. `dedo tools` lists both. [#121](https://github.com/raveesh-me/dedo/pull/121)

### Added

- Breaking rules, for what `buf breaking` cannot see because it never compares option values. `buf-plugin-dedo-lint` holds ten in a new category, `DEDO_BREAKING`, which `dedo init` and `dedo upgrade` add to `buf.yaml`'s `breaking.use` beside `FILE`: `DEDO_EMITS_NO_DELETE`, `DEDO_SCOPES_NO_DELETE`, `DEDO_ERROR_REASON_SAME_CODE`, `DEDO_AUDIENCE_NO_NARROW`, `DEDO_TENANCY_SAME`, `DEDO_CROSS_TENANT_NO_DELETE`, `DEDO_HTTP_BINDING_SAME`, `DEDO_FIELD_NO_NEW_REQUIRED`, `DEDO_RESOURCE_SAME` and `DEDO_SUNSET_NO_EARLIER`. Each gap was confirmed against `buf breaking` 1.65, which passed all ten changes. [#121](https://github.com/raveesh-me/dedo/pull/121)
- House breaking rules: CEL checks in `dedo.yaml`'s `breaking` section, which `buf breaking` runs on each element the compared version also has, reading that element's variables as `against` (`against.scopes.all(s, s in scopes)`). They get everything house lint rules have: `use` and `disable`, imports from folders, git and pinned URLs, `overrides` of dedo's breaking rules and of imported ones, `final`, `doc` links, the WebAssembly build and `dedo tools`' report. [#121](https://github.com/raveesh-me/dedo/pull/121)
- `DEDO_EVENT_NOT_PUBLISHED_DECLARED`: an RPC with `emits` declares `EVENT_NOT_PUBLISHED` with `UNAVAILABLE`. `DEDO_EVENT_TYPES` also checks that an event type names a resource its package declares. Neither makes `emits` required. [#121](https://github.com/raveesh-me/dedo/pull/121)
- `events.IdempotencyKey`, an optional key, such as the request's `request_id`, that every event of a retried command carries, while each gets an ID of its own: `driver.Event.IdempotencyKey`. A malformed key is refused with `events.ErrBadIdempotencyKey`. [#121](https://github.com/raveesh-me/dedo/pull/121)

## [v0.32.0] - 2026-11-05

### Added

- Domain events, outward only. An RPC declares the event types it may publish in `dedo.v1.method`'s new `emits` field, as `<context>.<module>.<resource>.<past-tense verb>` (`acme.orders.order.cancelled`), and `DEDO_EVENT_TYPES` checks them. The new `events` package publishes them through a bus driver the service injects (`events/driver`, in the pattern of `database/sql/driver`; `events/eventstest` for tests): an event is a type, the resource's name, an ID and a time, with no payload, and consumers fetch the resource through the API. `Publish` refuses a type the RPC does not declare, and retries with exponential backoff within the request's deadline until the driver acknowledges; then it returns `UNAVAILABLE` with reason `EVENT_NOT_PUBLISHED`, so the caller retries the command. The policy interceptor puts the RPC's contract in the handler's context (`policy.MethodFrom`). The REST reference lists each operation's events, in its description and as `x-emits`. `guidance/domains.md` says how: a core service only answers calls, never subscribes or watches, and announces what happened to a bus when it takes part in orchestration. [#119](https://github.com/raveesh-me/dedo/pull/119), closes [#98](https://github.com/raveesh-me/dedo/issues/98)

### Changed

- `guidance/domains.md` no longer maps domain events to server streams or published messages with payloads, and drops "keep a copy, updated through events" as a way out of a module cycle; `dedo modules` no longer suggests it either. The outbox is gone from the layout. [#119](https://github.com/raveesh-me/dedo/pull/119)

## [v0.31.0] - 2026-11-05

### Added

- `protoc-gen-dedo-openapi` warns, without failing, when a service's title repeats its package's heading ("Calculation › Calculation") and the service is not named after the one root it manages. The warning names that root, such as `TaxQuoteService` titled "Tax quotes". A service named after its root, like `OrderService` titled `Orders` in package `orders`, draws none. [#117](https://github.com/raveesh-me/dedo/pull/117), closes [#100](https://github.com/raveesh-me/dedo/issues/100)
- Package `github.com/raveesh-me/dedo/aggregate` finds the resources a service takes or returns and the roots among them. House rules and the REST doc generator share it. [#117](https://github.com/raveesh-me/dedo/pull/117)

## [v0.30.0] - 2026-11-05

### Added

- House rules on `service` get `resources`, the `google.api.resource` types its methods take or return at any depth; `roots`, those not nested under another's pattern; and `title`, its `dedo.v1.service` title. An organisation can then lint one root per service (`roots.size() <= 1`) and a service named after its root, as `guidance/RULES.md` shows. [#115](https://github.com/raveesh-me/dedo/pull/115), closes [#94](https://github.com/raveesh-me/dedo/issues/94)

## [v0.29.0] - 2026-11-05

### Added

- This changelog, covering every release since v0.1.0. [#112](https://github.com/raveesh-me/dedo/pull/112)
- `guidance/domains.md` says how to model a domain service, an operation with no aggregate: a service that manages no resource and holds only custom methods. In a multi-tenant service they hang off the tenant (`{location=locations/*}:resolveAddress`), otherwise off a collection noun (`addresses:resolveAddress`). One that only answers is a `NO_SIDE_EFFECTS` query, over `GET` where the input fits in a URL, and none changes an aggregate. `guidance/RULES.md` lets a read-only call that returns reference data from a third party keep `open_world_hint: false`. [#113](https://github.com/raveesh-me/dedo/pull/113), closes [#95](https://github.com/raveesh-me/dedo/issues/95)

## [v0.28.0] - 2026-11-04

### Breaking

- `dedo init` no longer writes a root `server.go`, and its `main.go` calls `<module>.New<Service>()` instead of `newServer()`. Existing repositories keep their files. [#109](https://github.com/raveesh-me/dedo/pull/109)
- `Makefile.api`'s `check` depends on a new `modules` target, which needs the `dedo` command at v0.28.0 (`go tool dedo`). `dedo upgrade` pins it. [#109](https://github.com/raveesh-me/dedo/pull/109)

### Added

- `dedo modules` checks the module layout that `guidance/domains.md` describes, and `make check` runs it. It fails on a cycle between modules (through `internal/<m>` or the code generated from `<m>`), a shared package that imports a module, module code outside `internal/<m>`, a proto module named `shared`, and SQL that names another module's schema. On success it prints the dependency graph. [#109](https://github.com/raveesh-me/dedo/pull/109)
- `dedo init` and `dedo upgrade` scaffold one handler stub per service in its module's package (`internal/<m>/<service>.go`, constructor `New<Service>()`). The generated `main.go` registers every service and its MCP tools, not only the first, and `policy_table_test.go` builds every service's policy table. Upgrades never overwrite a handler, and write no stubs where `main.go` wires services its own way. [#109](https://github.com/raveesh-me/dedo/pull/109)

### Changed

- `examples/todo` and `examples/contacts` keep their handlers and stores in `internal/<m>/`. `app/` is the composition root. [#109](https://github.com/raveesh-me/dedo/pull/109)

## [v0.27.0] - 2026-11-04

### Added

- Lint rule `DEDO_PACKAGE_SHARED_RESERVED`: a package's module segment may not be `shared` (`acme.shared.v1`). [#108](https://github.com/raveesh-me/dedo/pull/108)
- `guidance/domains.md` lays out code and data by proto module: one Go package per module at `internal/<m>/`, shared helpers under `internal/shared/`, ports owned by their provider, and one Postgres schema and database role per module. [#108](https://github.com/raveesh-me/dedo/pull/108)

## [v0.26.0] - 2026-11-04

### Changed

- `dedo upgrade` says when a kept `docs/index.html` or `docs/docs.go` differs from the release's template (the page's `<title>` aside), and prints the `dedo templates` command that refreshes it. It still never overwrites them. [#105](https://github.com/raveesh-me/dedo/pull/105), fixes [#101](https://github.com/raveesh-me/dedo/issues/101)
- `guidance/domains.md` counts calls and ports, not only proto imports, as dependencies between modules, and lists the ways out of a cycle. [#106](https://github.com/raveesh-me/dedo/pull/106), closes [#96](https://github.com/raveesh-me/dedo/issues/96)

## [v0.25.0] - 2026-11-04

### Added

- House rules can use `cel.bind`, and method rules get `http_bindings`: the `google.api.http` rule and each of its `additional_bindings`, in order, as `{verb, path}` maps. [#103](https://github.com/raveesh-me/dedo/pull/103)

### Changed

- Forking moves out of the readme's onboarding paragraph into a section of its own, and `guidance/forking.md`'s examples fork v0.24.0, the first release with `scripts/fork.sh`. [#90](https://github.com/raveesh-me/dedo/pull/90), [#91](https://github.com/raveesh-me/dedo/pull/91)

## [v0.24.0] - 2026-11-04

### Added

- `guidance/forking.md` and `scripts/fork.sh`, so an organisation can own its copy of dedo (Go modules, BSR names, MCP Go SDK build) and keep taking upstream releases. [#86](https://github.com/raveesh-me/dedo/pull/86), [#88](https://github.com/raveesh-me/dedo/pull/88)

### Changed

- Pre-release versions are read whole: `dedo tools` and `dedo upgrade` read a label such as `v0.24.1-rc.1` as written, `dedo upgrade`'s newer-release hint compares by semver, and the release check accepts pre-release tags. [#86](https://github.com/raveesh-me/dedo/pull/86)
- The `main.go` template puts dedo's imports in a group of their own. [#86](https://github.com/raveesh-me/dedo/pull/86)

### Fixed

- Moving `rules.yml` into `dedo.yaml` no longer copies the header an earlier `dedo init` wrote into `rules.yml`, and keeps the repository's own comments, including those at the top of the file. [#87](https://github.com/raveesh-me/dedo/pull/87), fixes [#75](https://github.com/raveesh-me/dedo/issues/75)

## [v0.23.0] - 2026-11-04

### Added

- `dedo docs -static FILE` (`make docs-static`, into `.tools/docs.html`) writes the REST reference as one HTML file that opens offline. The OpenAPI document and the page's scripts are inline; each script is checked against its integrity hash and cached, so later builds need no network. [#84](https://github.com/raveesh-me/dedo/pull/84)

## [v0.22.0] - 2026-11-03

### Added

- `guidance/domains.md` maps domain-driven design onto dedo: bounded context to repository, BSR module and REST root; module to proto package; aggregate to service; aggregate root to resource; value object to a message without a resource; commands and queries to RPCs; domain events to streams; and the context map. `dedo init` and `dedo upgrade` copy it into `AGENTS.md` and `CLAUDE.md` as its own block. [#81](https://github.com/raveesh-me/dedo/pull/81), [#82](https://github.com/raveesh-me/dedo/pull/82)
- When the documented services span several proto packages, the REST reference nests each service under its package: a `kind: nav` tag whose summary comes from the package name and whose description is the package comment. [#81](https://github.com/raveesh-me/dedo/pull/81)

### Changed

- `protoc-gen-dedo-openapi` writes OpenAPI 3.2.0. Generation fails when two tags share a name, and when a file without a package sits among packaged ones. [#81](https://github.com/raveesh-me/dedo/pull/81)
- The REST docs page moves from Stoplight Elements to Scalar 1.75.1, loaded with an SRI hash, with its AI, SDK, telemetry and branding features off. `dedo upgrade` leaves an existing `docs/index.html` alone: refresh it with `dedo templates docs/index.html`. [#81](https://github.com/raveesh-me/dedo/pull/81)

## [v0.21.1] - 2026-11-03

### Fixed

- `dedo upgrade` no longer renames dedo's `run`, `check` and `test` targets to `api-*` when the service's Makefile only extends them with prerequisites or a target variable. The rename had left `make check` and `make test` checking nothing. [#79](https://github.com/raveesh-me/dedo/pull/79)
- `dedo upgrade` no longer re-creates `server.go` and `policy_table_test.go` in a service that has its own server and calls `policy.Build`. [#79](https://github.com/raveesh-me/dedo/pull/79), fixes [#74](https://github.com/raveesh-me/dedo/issues/74)

## [v0.21.0] - 2026-11-03

### Added

- `dedo docs` serves only the REST reference (`docs/index.html` and `docs/openapi.json`), on `127.0.0.1:8099` by default. `-tunnel` shares it at a public Cloudflare quick-tunnel URL without exposing the service. `Makefile.api` gains `docs` and `docs-tunnel`. [#77](https://github.com/raveesh-me/dedo/pull/77)

### Changed

- The lint plugin runs api-linter 2.4.1. [#76](https://github.com/raveesh-me/dedo/pull/76)

## [v0.20.0] - 2026-10-30

### Added

- `dedo.yaml`, with a `rules` section (what `rules.yml` held) and an `annexure` section: the organisation's guidance for coding agents, from a `path` in the repository or a `url` pinned by `sha256`. [#72](https://github.com/raveesh-me/dedo/pull/72)
- `mcpserver.AddToolsWith(s, Options{Client, ForwardHeaders}, tools...)` and `Config.ForwardHeaders` copy extra headers, such as `Version`, from the MCP request onto the forwarded call. [#71](https://github.com/raveesh-me/dedo/pull/71)

### Changed

- A repository's `rules.yml` becomes `dedo.yaml`'s `rules` section; `dedo init` and `dedo upgrade` move it. The lint plugin still reads `rules.yml` where there is no `dedo.yaml`, and shared rules files that services import stay `rules.yml`. [#72](https://github.com/raveesh-me/dedo/pull/72)
- dedo's guidance in `AGENTS.md` and `CLAUDE.md` is now managed blocks (`<!-- dedo:begin <key> -->`) followed by each annexure. `dedo init` and `dedo upgrade` rewrite them every run, so guidance changes reach existing repositories. Where an annexure conflicts with dedo's guidance, the annexure wins. [#72](https://github.com/raveesh-me/dedo/pull/72)
- The guides cover url imports for published house rules. [#70](https://github.com/raveesh-me/dedo/pull/70)

## [v0.19.0] - 2026-10-30

### Added

- House rules can be imported from a file published over HTTPS, pinned by its SHA-256, so CI can fetch a private repository's published rules with no credential. `dedo tools` fetches and caches it; the lint stays offline. [#68](https://github.com/raveesh-me/dedo/pull/68)

## [v0.18.0] - 2026-10-29

### Added

- `dedo.v1.service.title` (optional) names a service's group in the REST reference. [#66](https://github.com/raveesh-me/dedo/pull/66)

### Changed

- Without a title, the REST reference groups a service's operations under its name without the `Service` suffix, split into words (`CatalogEntryService` becomes "Catalog Entry"). `operationId` is unchanged, but SDK generators that name classes after tags now produce `TodoApi` rather than `TodoServiceApi`. [#66](https://github.com/raveesh-me/dedo/pull/66)

## [v0.17.0] - 2026-10-29

### Breaking

- `protoc-gen-dedo-mcp` names the schema maps per service, `<Service>MCPInputSchemas` and `<Service>MCPOutputSchemas`, so one Go package can hold several service files. Code that passes the old `MCPInputSchemas` and `MCPOutputSchemas` to `mcpserver.New` renames them; `mcpserver.AddTools` with `<Service>MCPTools()` is unaffected. [#64](https://github.com/raveesh-me/dedo/pull/64)

## [v0.16.1] - 2026-10-29

### Added

- `guidance/generators.md`. Where no `go.mod` sits beside `buf.yaml`, `dedo init` appends it to `AGENTS.md`, telling coding agents how to add dedo's generators and runtime to the repository's own tooling and server. [#62](https://github.com/raveesh-me/dedo/pull/62)

## [v0.16.0] - 2026-10-29

### Breaking

- The lint plugin follows the protos: its release is the options module's label in `buf.yaml`, and `dedo tools` vendors it into `.tools/` in every repository, with or without `go.mod`. It is no longer pinned in `go.mod`, and `buf.yaml` no longer lists the BSR Wasm plugin. `dedo update && dedo upgrade` moves a repository; CI runs `dedo tools`, which needs Go, before `buf lint`. [#60](https://github.com/raveesh-me/dedo/pull/60)

### Added

- `dedo install` installs the lint plugin into `$GOBIN`, and `dedo uninstall` removes it and `dedo`. [#60](https://github.com/raveesh-me/dedo/pull/60)

### Fixed

- `dedo init` no longer lists a second `buf-plugin-dedo-lint` next to one at another path, which made Buf reject every rule as registered twice. [#60](https://github.com/raveesh-me/dedo/pull/60)

## [v0.15.0] - 2026-10-26

### Breaking

- Every plugin rule is in one Buf category, `DEDO`, and `buf.yaml` lists only that. `rules.yml` chooses which rules run, with `use` (by default `AIP_NOLANG`, `ZALANDO` and `DEDO`) and `disable`, which now also turns off dedo's own rules. `dedo init` and `dedo upgrade` move an earlier `buf.yaml` selection into `rules.yml`. [#57](https://github.com/raveesh-me/dedo/pull/57)

### Added

- `dedo init` ends by running `buf generate` and `go mod tidy` and reporting lint findings, so an empty Go module is two commands from a running API: `dedo init`, then `make run`. [#55](https://github.com/raveesh-me/dedo/pull/55)
- `Makefile.api` gains `run`, `generate`, `check`, `test` and `tools`. Where the service's Makefile already has one of them, dedo's is named `api-<name>`. [#55](https://github.com/raveesh-me/dedo/pull/55)
- `dedo tools -wasm FILE` builds the lint plugin as WebAssembly with the repository's house rules built in, for repositories without Go. [#56](https://github.com/raveesh-me/dedo/pull/56)

### Changed

- A rule that does not run overrides nothing, so the rules it overrides run again. [#57](https://github.com/raveesh-me/dedo/pull/57)

## [v0.14.0] - 2026-10-25

### Added

- `mcpserver.Guard` and `MCP_ACCESS` in the generated `main.go`. `public`, the default, lets anyone list the tools; `authenticated` answers a request with bad credentials with 401 and `WWW-Authenticate: Bearer`, before tool discovery. Every tool call is checked either way. [#52](https://github.com/raveesh-me/dedo/pull/52)
- `dedo init` and `dedo upgrade` replace the MCP Go SDK with dedo's build of it, `v1.8.0-dedo.1` (upstream plus [go-sdk#1454](https://github.com/modelcontextprotocol/go-sdk/pull/1454)'s in-place JSON decoding), through a release-scoped `replace` in `go.mod`, until an SDK release has the fix. An MCP tool call takes 36–50% less time and about 90% fewer bytes. [#51](https://github.com/raveesh-me/dedo/pull/51)
- `examples/contacts` runs as a multi-tenant service. MCP security tests, `BenchmarkMCPHop`, and `bench/mcp`, which compares dedo with a NestJS MCP server in containers. [#50](https://github.com/raveesh-me/dedo/pull/50)

### Changed

- The guides say a Connect client over HTTP needs its own `http.Client` with `MaxIdleConnsPerHost` sized to its concurrency. [#52](https://github.com/raveesh-me/dedo/pull/52)

## [v0.13.0] - 2026-10-25

### Added

- `final: true` on a house rule: no rule may override it and no `rules.yml` may `disable` it. `buf-plugin-dedo-lint -dedo-rules` and `dedo tools` list every final rule. [#49](https://github.com/raveesh-me/dedo/pull/49)

## [v0.12.0] - 2026-10-25

### Added

- Optional multi-tenancy. A service declares its tenant collection (`tenancy: {collection: "locations"}`), and the interceptor checks the tenant bound in the path and every tenant name in a resource reference, on every request and transport, streams included, through `interceptor.Config.Tenants`. `cross_tenant: true` marks the RPCs that span tenants. The opt-in `DEDO_MULTI_TENANT` category has four rules that check the contract supports it. [#43](https://github.com/raveesh-me/dedo/pull/43), [#44](https://github.com/raveesh-me/dedo/pull/44)
- House rules may override dedo's, by listing the rules they replace in `overrides`. The order is ZALANDO, AIP, DEDO, HOUSE, and a rules file outranks the files it imports. `ZALANDO_SERVICE_RULES_REQUIRED`, `DEDO_METHOD_RULES_REQUIRED` and `DEDO_MULTI_TENANT_REQUEST_TENANT` are final. [#48](https://github.com/raveesh-me/dedo/pull/48)
- `DEDO_HTTP_RESPONSE_BODY` overrides the seven AIP rules the `buf.yaml` template used to except, and `dedo init` and `dedo upgrade` take them out of `lint.except`. [#48](https://github.com/raveesh-me/dedo/pull/48)
- `buf-plugin-dedo-lint -dedo-rules` prints the consolidated rules, and `dedo tools` runs it. `dedo upgrade -lint` moves only the lint plugin. [#48](https://github.com/raveesh-me/dedo/pull/48)
- dedo's logos and banners, in `assets/brand/`. [#47](https://github.com/raveesh-me/dedo/pull/47)

### Changed

- House-rule import paths resolve from the root of the repository the importing file belongs to, not from the importing file's folder. A nested local rules file that imported siblings by bare name must now name them from the root. [#46](https://github.com/raveesh-me/dedo/pull/46)

### Security

- Paths inside a git import of house rules can no longer leave its checkout, through `../` or a symlink. [#46](https://github.com/raveesh-me/dedo/pull/46)

## [v0.11.1] - 2026-10-20

### Added

- House rules are CEL checks in `rules.yml`, read by the lint plugin, with nested imports from git (at a ref) or a folder, and `disable`. There is no plugin to write or build. `dedo tools` fetches git imports into `.tools/rules/`; the lint never touches the network. [#41](https://github.com/raveesh-me/dedo/pull/41)
- `dedo` is pinned as a Go tool, so `make tools` (`go tool dedo tools`) needs only Go. [#41](https://github.com/raveesh-me/dedo/pull/41)

### Removed

- `examples/house-lint`, a house-rule plugin in Go, gives way to the shared `examples/house-rules/rules.yml`. [#41](https://github.com/raveesh-me/dedo/pull/41)

### Fixed

- `dedo upgrade` re-pins every generator that `buf.gen.yaml` runs through `go tool`, and removes the connect-go v1 generator first. [#40](https://github.com/raveesh-me/dedo/pull/40)

## [v0.11.0] - 2026-10-20

### Breaking

- dedo moves to connect-go v2 (v2.0.1), Vanguard v0.5.0 and validate v0.9.0. Connect, gRPC, REST and MCP reach one `connect.Server` through one interceptor chain: flow ID, auth (now a server interceptor), dedo's policy, then validation. `interceptor.New` returns a `connect.ServerInterceptor`, `mcpserver.AddTools` takes a `*connect.Client`, and `InProcessClient` and `Target` are gone. Run `dedo upgrade`, which runs `connect-go-v2-migrate`, then wire `main.go` as the template does. [#39](https://github.com/raveesh-me/dedo/pull/39)
- The lint plugin is pinned in the service's `go.mod` as a Go tool and built by `dedo tools`. `DEDO_VERSION` and the download from the GitHub release are gone. [#37](https://github.com/raveesh-me/dedo/pull/37)
- The options module is pinned by label in `buf.yaml` (`buf.build/r-a/dedo:vX`), so the options and the lint plugin never drift. [#38](https://github.com/raveesh-me/dedo/pull/38)

### Added

- `dedo update` installs the latest `dedo`, and `dedo upgrade` moves a repository to it as one version. [#38](https://github.com/raveesh-me/dedo/pull/38)
- `dedo init` fits existing Buf repositories: it keeps their generators, remote ones included, and adds only its own; takes Go names from `go_package` or managed mode; and sets up a repository of protos without Go with the options and the BSR lint plugin. [#38](https://github.com/raveesh-me/dedo/pull/38)
- In a module with no protos, `dedo init` writes a starter service that passes lint, a `server.go` stub and a `main.go` that builds. [#35](https://github.com/raveesh-me/dedo/pull/35)
- `dedo tools` builds the lint plugin into `.tools/`. [#37](https://github.com/raveesh-me/dedo/pull/37)

### Changed

- The interceptor sets deprecation headers on every response, errors included, sets `Cache-Control` on the first message only, and audits errors on every call, streams included. [#39](https://github.com/raveesh-me/dedo/pull/39)
- One MCP tool call takes about 188 µs and 522 allocations, down from 226 µs and 608. [#39](https://github.com/raveesh-me/dedo/pull/39)

### Fixed

- A `dedo init` that failed part way recovers on the next run, and a failed command no longer stops the rest: every failure is listed at the end. [#35](https://github.com/raveesh-me/dedo/pull/35)
- `make api-check` works before the first commit and with any base branch (`API_BASE`). [#36](https://github.com/raveesh-me/dedo/pull/36)

## [v0.10.0] - 2026-10-20

### Changed

- The readme, the guides and the BSR module's README describe v0.9.0. [#33](https://github.com/raveesh-me/dedo/pull/33)

## [v0.9.0] - 2026-10-20

### Breaking

- `protoc-gen-dedo-mcp` generates each service's tools as `<Service>MCPTools()` in `*.mcp.pb.go`, with names, text, error reasons and annotations written out at generation time. Delete the old `*.mcp_schema.pb.go` files, or let `dedo init` delete them. [#32](https://github.com/raveesh-me/dedo/pull/32)

### Added

- `mcpserver.AddTools` serves generated tools without reflection, so `main.go` no longer needs `protoreflect` or a `File_…` descriptor. `mcpserver.New` still works. [#32](https://github.com/raveesh-me/dedo/pull/32)

## [v0.8.0] - 2026-10-20

### Added

- The `dedo` command. `dedo init` reads `go.mod` and the protos and adds what is missing: the options dependency, the lint plugin, the generators, the server runtime, the REST docs page, a policy-table test and the agent guidance. It edits YAML in place, a second run changes nothing, and `-offline` edits files only. [#31](https://github.com/raveesh-me/dedo/pull/31)
- `dedo templates [NAME]` prints the templates bundled into the binary. Releases ship `dedo_<os>_<arch>` binaries instead of the templates tarball. [#31](https://github.com/raveesh-me/dedo/pull/31)

### Changed

- `options.proto` sets `go_package`, so services generate against it with or without managed mode. [#31](https://github.com/raveesh-me/dedo/pull/31)
- `install.md` becomes `tutorial.md`, built on `dedo init`, and the new `install.md` is a checklist of each part. [#31](https://github.com/raveesh-me/dedo/pull/31)

## [v0.7.0] - 2026-10-20

### Breaking

- The lint plugin is `buf-plugin-dedo-lint`, matching `buf.build/r-a/dedo-lint` on the BSR. In `buf.yaml`, change `.tools/buf-plugin-dedo` to `.tools/buf-plugin-dedo-lint`. [#30](https://github.com/raveesh-me/dedo/pull/30)

### Added

- Releases attach `dedo-templates.tar.gz` (the templates, the REST docs page and the agent guidance), so setting up a service needs no clone. [#30](https://github.com/raveesh-me/dedo/pull/30)
- `guidance/claude.md`, guidance for coding agents that a service adds to its `AGENTS.md` or `CLAUDE.md`. The guides move into `guidance/`. [#28](https://github.com/raveesh-me/dedo/pull/28)
- MCP input schemas carry constraints: resource name patterns, `google.type.Money`'s rules and `buf.validate` rules. [#27](https://github.com/raveesh-me/dedo/pull/27)

### Fixed

- MCP field masks take the snake_case names the input schema uses, empty results are written with their defaults (`{"todos":[]}`), and 64-bit integers in outputs are typed as strings. [#27](https://github.com/raveesh-me/dedo/pull/27)

### Security

- The todo example and the guides wrap the MCP handler in `http.NewCrossOriginProtection()`, since the SDK's DNS-rebinding guard does not apply behind a proxy. [#28](https://github.com/raveesh-me/dedo/pull/28)

### Removed

- jq is no longer needed. [#29](https://github.com/raveesh-me/dedo/pull/29)

## [v0.6.0] - 2026-10-20

### Breaking

- One lint plugin runs Google's api-linter (v2.4.0) in-process, and `buf-plugin-aip` is no longer released: remove its `plugins:` entry. dedo's rules split into `ZALANDO_*` (5) and `DEDO_*` (9), and `HOUSE_*` is left to an organisation's own rules. In `lint.use`, replace `HOUSE` with `DEDO` and `ZALANDO`. `HOUSE_RULES.md` becomes `RULES.md`. [#21](https://github.com/raveesh-me/dedo/pull/21)
- MCP runs on the official MCP Go SDK alone, without Redpanda's `protoc-gen-go-mcp`. `mcpserver.New(server, Config{Services, InputSchemas, OutputSchemas, Client})` registers the tools itself; `Wrap` and `ForwardHeaders` are removed. [#20](https://github.com/raveesh-me/dedo/pull/20)

### Added

- `protoc-gen-dedo-mcp` writes output schemas as well. The SDK validates arguments and results and returns `structuredContent`, and tool errors carry the code, message and declared reason as JSON. [#20](https://github.com/raveesh-me/dedo/pull/20)
- Dependabot pull requests for api-linter and the MCP Go SDK, and a weekly check against the latest MCP specification. [#22](https://github.com/raveesh-me/dedo/pull/22)

## [v0.5.0] - 2026-10-19

### Breaking

- The generators are renamed: `protoc-gen-mcp-schema` to `protoc-gen-dedo-mcp`, and `protoc-gen-rest-openapi` to `protoc-gen-dedo-openapi`. [#17](https://github.com/raveesh-me/dedo/pull/17)

### Added

- Releases carry both generators as native binaries for macOS and Linux. [#17](https://github.com/raveesh-me/dedo/pull/17)

### Fixed

- Every module can be `go install`ed: the nested modules have no `replace` lines, and local development uses `go.work`. [#17](https://github.com/raveesh-me/dedo/pull/17)

## [v0.4.1] - 2026-10-19

### Fixed

- The templates and docs named v0.3.0, which was never released, so `make tools` downloaded nothing. The release job now runs `scripts/check-release-versions.sh` first. [#15](https://github.com/raveesh-me/dedo/pull/15)

## [v0.4.0] - 2026-10-19

v0.3.0 was never released; its changes ship here.

### Breaking

- http-proto-lint is now dedo: the repository `raveesh-me/dedo`, the Go modules `github.com/raveesh-me/dedo/...`, the proto package `dedo.v1` (options read `(dedo.v1.method)`), the BSR organisation `r-a` (`buf.build/r-a/dedo`), the lint plugin `buf-plugin-dedo` and `DEDO_VERSION` in `Makefile.api`. [#14](https://github.com/raveesh-me/dedo/pull/14)

### Added

- The Apache-2.0 license. [#14](https://github.com/raveesh-me/dedo/pull/14)
- `protoc-gen-mcp-schema` writes MCP tool input schemas at generation time, with each field's comment as its description, `OUTPUT_ONLY` fields left out and partial updates by field mask. Deprecated RPCs are no longer tools, and tool descriptions list their error reasons. `mcpserver.New` serves them. [#13](https://github.com/raveesh-me/dedo/pull/13)
- A README for the options module's BSR page. [#11](https://github.com/raveesh-me/dedo/pull/11)

### Changed

- The REST docs page renders with Stoplight Elements, and the spec's server URL is relative, so its console calls whichever origin served the docs. [#12](https://github.com/raveesh-me/dedo/pull/12)

## [v0.2.0] - 2026-10-19

### Breaking

- `tool.destructive` is removed. `Tool` is `title`, `description` and `annotations`, which mirror the MCP specification's `ToolAnnotations`. [#9](https://github.com/raveesh-me/dedo/pull/9), [#10](https://github.com/raveesh-me/dedo/pull/10)

### Added

- `HOUSE_TOOL_ANNOTATIONS` checks the MCP hints are declared and agree with `idempotency_level`. [#9](https://github.com/raveesh-me/dedo/pull/9)
- Pull requests labelled `allow-breaking` skip CI's `buf breaking` check. [#10](https://github.com/raveesh-me/dedo/pull/10)
- The todo example runs on kind, with a Vue UI that updates live over a `WatchTodos` stream when an agent changes data through MCP, Playwright tests, and setup for Claude Code and Claude Desktop. [#7](https://github.com/raveesh-me/dedo/pull/7), [#8](https://github.com/raveesh-me/dedo/pull/8)

### Fixed

- The MCP adapter sends only the hints the proto declares, so the others take the specification's defaults. [#9](https://github.com/raveesh-me/dedo/pull/9)

## [v0.1.1] - 2026-10-18

### Added

- `install.md` walks through starting a new API product. [#6](https://github.com/raveesh-me/dedo/pull/6)

### Fixed

- Releases push to the BSR's `main` label too, so unpinned dependencies resolve. [#6](https://github.com/raveesh-me/dedo/pull/6)

## [v0.1.0] - 2026-10-18

The first release, as http-proto-lint (`github.com/raveesh-me/http-proto-lint`, `buf.build/cookytech-in`).

### Added

- The options module: audience, owner, scopes, cache policy, deprecation, error reasons, sensitive fields, MCP tool text and message descriptions.
- A Buf lint plugin with thirteen house rules, each finding linked to its section in `HOUSE_RULES.md` at the plugin's release. [#1](https://github.com/raveesh-me/dedo/pull/1), [#2](https://github.com/raveesh-me/dedo/pull/2), [#3](https://github.com/raveesh-me/dedo/pull/3)
- Lint plugins as native binaries, about 17 times faster than WebAssembly in an editor, and as Wasm plugins on the BSR. [#4](https://github.com/raveesh-me/dedo/pull/4)
- The runtime: the policy table, flow IDs, and an interceptor that checks scopes, sets `Cache-Control`, `Deprecation` and `Sunset`, and logs undeclared error reasons.
- `protoc-gen-rest-openapi`, an OpenAPI 3.1 reference from the `google.api.http` bindings. [#1](https://github.com/raveesh-me/dedo/pull/1)
- MCP tools, served on the official MCP Go SDK under short snake_case names and through the same auth and policy as REST. [#1](https://github.com/raveesh-me/dedo/pull/1), [#2](https://github.com/raveesh-me/dedo/pull/2)
- Templates for service repositories, `guideline.md`, and the `todo`, `bad-todo` and `library` examples. [#1](https://github.com/raveesh-me/dedo/pull/1), [#5](https://github.com/raveesh-me/dedo/pull/5)

[Unreleased]: https://github.com/raveesh-me/dedo/compare/v0.36.0...HEAD
[v0.36.0]: https://github.com/raveesh-me/dedo/compare/v0.35.0...v0.36.0
[v0.35.0]: https://github.com/raveesh-me/dedo/compare/v0.34.0...v0.35.0
[v0.34.0]: https://github.com/raveesh-me/dedo/compare/v0.33.0...v0.34.0
[v0.33.0]: https://github.com/raveesh-me/dedo/compare/v0.32.3...v0.33.0
[v0.32.3]: https://github.com/raveesh-me/dedo/compare/v0.32.2...v0.32.3
[v0.32.2]: https://github.com/raveesh-me/dedo/compare/v0.32.1...v0.32.2
[v0.32.1]: https://github.com/raveesh-me/dedo/compare/v0.32.0...v0.32.1
[v0.32.0]: https://github.com/raveesh-me/dedo/compare/v0.31.0...v0.32.0
[v0.31.0]: https://github.com/raveesh-me/dedo/compare/v0.30.0...v0.31.0
[v0.30.0]: https://github.com/raveesh-me/dedo/compare/v0.29.0...v0.30.0
[v0.29.0]: https://github.com/raveesh-me/dedo/compare/v0.28.0...v0.29.0
[v0.28.0]: https://github.com/raveesh-me/dedo/compare/v0.27.0...v0.28.0
[v0.27.0]: https://github.com/raveesh-me/dedo/compare/v0.26.0...v0.27.0
[v0.26.0]: https://github.com/raveesh-me/dedo/compare/v0.25.0...v0.26.0
[v0.25.0]: https://github.com/raveesh-me/dedo/compare/v0.24.0...v0.25.0
[v0.24.0]: https://github.com/raveesh-me/dedo/compare/v0.23.0...v0.24.0
[v0.23.0]: https://github.com/raveesh-me/dedo/compare/v0.22.0...v0.23.0
[v0.22.0]: https://github.com/raveesh-me/dedo/compare/v0.21.1...v0.22.0
[v0.21.1]: https://github.com/raveesh-me/dedo/compare/v0.21.0...v0.21.1
[v0.21.0]: https://github.com/raveesh-me/dedo/compare/v0.20.0...v0.21.0
[v0.20.0]: https://github.com/raveesh-me/dedo/compare/v0.19.0...v0.20.0
[v0.19.0]: https://github.com/raveesh-me/dedo/compare/v0.18.0...v0.19.0
[v0.18.0]: https://github.com/raveesh-me/dedo/compare/v0.17.0...v0.18.0
[v0.17.0]: https://github.com/raveesh-me/dedo/compare/v0.16.1...v0.17.0
[v0.16.1]: https://github.com/raveesh-me/dedo/compare/v0.16.0...v0.16.1
[v0.16.0]: https://github.com/raveesh-me/dedo/compare/v0.15.0...v0.16.0
[v0.15.0]: https://github.com/raveesh-me/dedo/compare/v0.14.0...v0.15.0
[v0.14.0]: https://github.com/raveesh-me/dedo/compare/v0.13.0...v0.14.0
[v0.13.0]: https://github.com/raveesh-me/dedo/compare/v0.12.0...v0.13.0
[v0.12.0]: https://github.com/raveesh-me/dedo/compare/v0.11.1...v0.12.0
[v0.11.1]: https://github.com/raveesh-me/dedo/compare/v0.11.0...v0.11.1
[v0.11.0]: https://github.com/raveesh-me/dedo/compare/v0.10.0...v0.11.0
[v0.10.0]: https://github.com/raveesh-me/dedo/compare/v0.9.0...v0.10.0
[v0.9.0]: https://github.com/raveesh-me/dedo/compare/v0.8.0...v0.9.0
[v0.8.0]: https://github.com/raveesh-me/dedo/compare/v0.7.0...v0.8.0
[v0.7.0]: https://github.com/raveesh-me/dedo/compare/v0.6.0...v0.7.0
[v0.6.0]: https://github.com/raveesh-me/dedo/compare/v0.5.0...v0.6.0
[v0.5.0]: https://github.com/raveesh-me/dedo/compare/v0.4.1...v0.5.0
[v0.4.1]: https://github.com/raveesh-me/dedo/compare/v0.4.0...v0.4.1
[v0.4.0]: https://github.com/raveesh-me/dedo/compare/v0.2.0...v0.4.0
[v0.2.0]: https://github.com/raveesh-me/dedo/compare/v0.1.1...v0.2.0
[v0.1.1]: https://github.com/raveesh-me/dedo/compare/v0.1.0...v0.1.1
[v0.1.0]: https://github.com/raveesh-me/dedo/releases/tag/v0.1.0
