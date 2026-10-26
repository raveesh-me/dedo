# Building an API with dedo

This guide explains how the pieces fit and why, in the order you meet them. To set a service up, run `dedo init` or follow [install.md](install.md); for a walkthrough of a new product, follow [tutorial.md](tutorial.md). `examples/todo` is the reference: every step below exists there in working form.

## What you get

You write one `.proto` file per API. From it you get:

- **Three transports with one behaviour.** Connect, gRPC and REST serve the same handlers. REST paths come from `google.api.http` annotations.
- **Authorization without code.** Each RPC declares its scopes. The runtime rejects callers without one, on every transport. A multi-tenant service also declares its tenant, and the runtime keeps each caller to its own ([section 7](#multi-tenant-services)).
- **REST docs.** `protoc-gen-dedo-openapi` writes an OpenAPI 3.1 document of the REST API only. Connect's own routes stay out of it.
- **MCP tools.** Every unary RPC becomes an MCP tool for AI agents, named after the RPC in snake_case (`GetProduct` becomes `get_product`). `protoc-gen-dedo-mcp` generates the tools; tool calls pass through the same scope checks and validation as REST calls.
- **A blocking review gate.** `buf lint` runs Buf's rules and buf-plugin-dedo-lint's rules (`AIP_*` from Google's AIPs, `ZALANDO_*` from Zalando's guidelines, `DEDO_*` of its own, and the opt-in `DEDO_MULTI_TENANT_*`), plus any `HOUSE_*` rules your organisation adds, in your editor and in CI. [RULES.md](RULES.md) lists them, the order in which they may override each other (your house rules above dedo's, dedo's above the AIPs, the AIPs above Zalando), and which rules dedo skips.

Your handlers contain business logic only. Scope and tenant checks, cache and deprecation headers, flow IDs and error auditing come from the runtime.

## 1. Before you start

Install:

- Go 1.25 or later
- The [Buf CLI](https://buf.build/docs/installation)
- VS Code with the Buf extension (`bufbuild.vscode-buf`), or Zed. Remove other Protobuf extensions.
- The `dedo` command: `go install github.com/raveesh-me/dedo/cmd/dedo@v0.14.0`

Decide these names with your team:

| Decision | Example | Who decides |
| --- | --- | --- |
| Proto package | `acme.products.v1` | You. Use `<org>.<platform>.v<major>`. |
| REST root | `/commerce/products` or `/commerce/pim` | You. The linter does not care what it is. |
| Tenant collection, for a multi-tenant API | `locations` | You. It becomes the first segment of every resource name. |

Every REST path is `<root>/v<major>/<resource path>`, for example `/commerce/pim/v1/products/{product}`. The version segment is mandatory and must match the package version. The root must be the same for every RPC of a service. Each root segment is one lowercase or lowerCamelCase word: `/commerce/timeCounter` works, and `/commerce/time-counter` fails AIP-127's path syntax check.

## 2. Create the repository

In a Go module (`go mod init`), run `dedo init`. A service repository then looks like this:

```text
my-service/
  proto/acme/products/v1/products.proto
  buf.yaml               # deps, lint categories and the lint plugin
  buf.gen.yaml           # the four generators, run through go tool
  Makefile               # include Makefile.api
  Makefile.api           # run, generate, check, test, tools
  rules.yml              # optional: your organisation's HOUSE rules
  docs/                  # serves the REST reference
  server.go              # your handlers (dedo init writes a stub)
  policy_table_test.go   # fails on a missing declaration
  AGENTS.md              # guidance for coding agents
  .github/CODEOWNERS     # from templates/CODEOWNERS
  go.mod                 # the generators as tools, the dedo runtime
  main.go                # written by dedo init
```

Why it is set up this way:

- **The generators are Go tools** pinned in `go.mod`, so every machine and CI generate the same code without Buf's remote plugins.
- **The lint plugin is pinned in `go.mod` too**, and built into `.tools/` (ignored by git) by `dedo tools`, so the editor and CI run the same version. It runs as a native binary because Buf starts it about a dozen times per lint. Commit `buf.lock`.
- **connect-go v2:** one `connect.Server` holds the interceptors (flow IDs, auth, dedo's policy, validation); Connect and gRPC (`connecthttp.Mount`), REST (`vanguard.Mount`) and MCP (`connectinprocess`) all reach it, so every transport gets the same checks.

[install.md](install.md) shows each part by hand, for repositories that prefer not to run `dedo init`. Add `.github/CODEOWNERS` yourself (`dedo templates CODEOWNERS`) so every change under `proto/` needs an API review.

## 3. Write the contract

Model the API as resources, the way [AIP-121](https://google.aip.dev/121) describes. A resource has a name like `products/{product}`. Methods are the standard ones (`Get`, `List`, `Create`, `Update`, `Delete`) or custom ones bound as `:verb`.

Every service, RPC and message carries options from `dedo.v1`. Here is one complete method:

```protobuf
// ProductService serves the product catalogue.
service ProductService {
  option (dedo.v1.service) = {
    audience: AUDIENCE_EXTERNAL_PUBLIC
    owner_team: "pim-team"
  };

  // GetProduct returns one product.
  rpc GetProduct(GetProductRequest) returns (GetProductResponse) {
    option idempotency_level = NO_SIDE_EFFECTS;
    option (google.api.http) = {
      get: "/commerce/pim/v1/{name=products/*}"
      response_body: "product"
    };
    option (dedo.v1.method) = {
      scopes: "pim.product.read"
      cache: {visibility: VISIBILITY_PRIVATE, max_age: {seconds: 30}}
      errors: {reason: "PRODUCT_NOT_FOUND", code: NOT_FOUND}
      tool: {
        title: "Get a product"
        description: "Returns one product by its resource name, such as products/123."
        annotations: {
          read_only_hint: true
          open_world_hint: false
        }
      }
    };
  }
}

// GetProductRequest is the request of GetProduct.
message GetProductRequest {
  option (dedo.v1.message) = {description: "Which product to get."};

  // The resource name of the product.
  string name = 1 [
    (google.api.field_behavior) = REQUIRED,
    (google.api.resource_reference) = {type: "pim.acme.com/Product"}
  ];
}
```

What each option does:

| Option | Read by | Effect |
| --- | --- | --- |
| `service.audience`, `service.owner_team` | Lint, docs | Who may call the API, and who owns it |
| `method.scopes` | Runtime, docs | A caller needs one of them. The service will not start if an RPC has none. |
| `method.errors` | Runtime, docs | Every error the RPC returns. Undeclared ones are logged. The docs list them per HTTP status. |
| `method.cache` | Runtime, docs | `Cache-Control` on GET responses of read-only RPCs |
| `method.deprecation` with `deprecated = true` | Runtime, docs | `Deprecation` and `Sunset` headers, and the successor in the docs |
| `method.tool` | MCP, docs | The MCP tool's `title` and `description`, also the REST operation's summary and description. The description is the one field every client passes to the model. |
| `method.tool.annotations` | MCP | The MCP specification's `ToolAnnotations` hints (`read_only_hint`, `destructive_hint`, `idempotent_hint`, `open_world_hint`), for clients deciding, for example, whether to ask the user first |
| `message.description` | MCP, docs | The schema description shown to agents and API readers |
| `field.sensitive` | Docs | Marks personal or secret data (`x-sensitive`) |
| `service.tenancy`, `method.cross_tenant` | Lint, runtime | A multi-tenant service: the runtime checks every request's tenant ([section 7](#multi-tenant-services)) |

The MCP tool schemas also read the standard annotations, so a model can get a call right the first time:

| Declaration | In the tool's input schema |
| --- | --- |
| Field comments | Each field's `description` |
| `google.api.field_behavior` | `REQUIRED` fields are required; `OUTPUT_ONLY` fields are left out. A create leaves out the resource's name; an update with a `FieldMask` requires only the name and lists the field names the mask accepts. |
| `google.api.resource` and `resource_reference` | A `pattern` for resource names, such as `^todos/[^/]+$` |
| `buf.validate` rules | `minimum`, `maximum`, `minLength`, `maxLength`, `pattern`, `enum`, `minItems`, `maxItems`, and `format` for `email`, `uri` and `uuid`. Add `connectrpc.com/validate`'s interceptor so the server enforces the same rules on every transport (see `examples/todo/app/app.go`). |
| `google.type.Money` | The currency code pattern and the `nanos` range its comments document |

Tool calls name fields as the proto does (`budget_amount`), field masks included; `mcpserver` converts mask paths to the JSON mapping. Responses carry every field, defaults included, so an empty list reads `{"todos": [], "next_page_token": ""}`.

Write `tool.description` and `message.description` for the person or model choosing between operations: what it does and when to use it. Proto comments stay the place for notes to implementers.

Every RPC returns a unique `<Rpc>Response` message. Set `response_body` on Get, Create and Update, so REST callers receive the bare resource.

## 4. Lint

```sh
make check         # format, lint, breaking-change check
```

The same findings appear in VS Code as you type. To suppress one, put `// buf:lint:ignore <RULE_ID>` above the element, followed by the reason. Reviewers treat every suppression as a decision to justify.

dedo's own rules (each finding links to its section in [RULES.md](RULES.md), which explains the rule with examples; the AIP rules link to linter.aip.dev):

| Rule | Fix |
| --- | --- |
| `ZALANDO_SERVICE_RULES_REQUIRED` | Add the service option with an audience and an owner team. |
| `DEDO_METHOD_RULES_REQUIRED` | Add the method option with a scope and an error reason. |
| `ZALANDO_SCOPE_NAME_FORMAT` | Name scopes `<app>.<resource>.<read\|write>`. |
| `DEDO_ERROR_REASON_FORMAT` | Use UPPER_SNAKE_CASE reasons. Do not use the codes `OK` or `UNKNOWN`. |
| `ZALANDO_CACHE_REQUIRES_NO_SIDE_EFFECTS` | Cache only `NO_SIDE_EFFECTS` RPCs, with a visibility and a max age. |
| `ZALANDO_DEPRECATION_CONSISTENT` | Set `deprecated = true` and a deprecation schedule together. Sunset after deprecation. Name a successor that exists. |
| `DEDO_READ_METHOD_NO_SIDE_EFFECTS` | Set `idempotency_level = NO_SIDE_EFFECTS` on unary `Get*` and `List*`. |
| `DEDO_HTTP_RESPONSE_BODY` | Set `response_body` on `Get*`, `Create*` and `Update*` HTTP rules. |
| `DEDO_HTTP_PATH_VERSION` | Put `/v<major>/` in every path, after the same root for the whole service. |
| `DEDO_NO_JSON_NAME` | Remove `json_name`. JSON uses the protojson default, lowerCamelCase. |
| `ZALANDO_MONEY_FIELD_TYPE` | Use `google.type.Money` for `price`, `amount`, `*_price` and `*_amount`. |
| `DEDO_TOOL_REQUIRED` | Give every unary RPC a tool title and description. |
| `DEDO_TOOL_ANNOTATIONS` | Declare the MCP hints. They must match `idempotency_level`, and Delete and Remove are destructive. |
| `DEDO_MESSAGE_DESCRIPTION_REQUIRED` | Give every message a `message.description`. |

`examples/bad-todo` breaks every rule once. Its `expected-lint.txt` lists the findings, and its tests show what each mistake does at runtime.

## 5. Generate

```sh
make generate
```

This writes, for each service:

- `gen/go/.../<pkg>.pb.go` and `<pkg>connect/` for the API
- `gen/go/.../<pkg>.mcp.pb.go` with `<Service>MCPTools()`: the MCP tools, their input and output schemas, and typed forwarders
- `docs/openapi.json` for the REST reference

Commit all of it. CI fails when the committed output is stale.

## 6. Wire the service

`examples/todo/app/app.go` and `app/mcp.go` show the whole setup. In short:

```go
// One connect.Server, with the interceptors every transport runs: flow IDs,
// auth, the declared contract, then the buf.validate rules.
table, err := policy.Build(productsv1connect.ProductServiceName) // fails on a missing declaration
guard, err := interceptor.New(interceptor.Config{
	Table:         table,
	Scopes:        auth.ScopesFromContext, // your token interceptor
	EnforceScopes: true,                   // false only logs denials, to roll out to an existing service
	Log:           log,
})
server := connect.NewServer(flowid.Interceptor(), auth.Interceptor(), guard, validate.NewServerInterceptor())
productsv1connect.RegisterProductServiceHandler(server, newServer())

mux := http.NewServeMux()
connecthttp.Mount(mux, server)                       // Connect, gRPC, gRPC-Web
err = vanguard.Mount(mux, server)                    // REST, from google.api.http
client := connect.NewClient(connectinprocess.New(server)) // MCP calls, in-process

// MCP: every RPC is a tool, as protoc-gen-dedo-mcp generated it.
tools := mcp.NewServer(&mcp.Implementation{Name: "products", Version: "v1"}, nil)
err = mcpserver.AddTools(tools, client, productsv1.ProductServiceMCPTools()...) // fails if two RPCs share a tool name

// Who may reach /mcp: public (anyone may list the tools) or authenticated.
endpoint, err := mcpserver.Guard(mcp.NewStreamableHTTPHandler(
	func(*http.Request) *mcp.Server { return tools },
	&mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true}),
	mcpserver.Access(os.Getenv("MCP_ACCESS")), auth.Authenticated)

mux.Handle("GET /openapi.json", docs.Spec()) // embeds docs/openapi.json
mux.Handle("GET /docs", docs.Page())
// Cross-origin protection: the MCP transport requires an Origin check.
mux.Handle("/mcp", flowid.Middleware(http.NewCrossOriginProtection().Handler(endpoint)))
```

**Who may reach `/mcp`** is a setting, `MCP_ACCESS` in the `main.go` `dedo init` writes:

- `public`, the default: anyone may list the tools and their schemas, which carry nothing secret. Every tool call still goes through your token check and the scope checks, so a caller without the scope is denied.
- `authenticated`: a request without credentials your token check accepts gets HTTP 401 before any MCP handling, tool discovery included, as the MCP authorization specification describes. Choose it when even the list of what your API can do is not for everyone.

An unknown value stops the service at startup rather than serving it publicly.

**Calling another service over HTTP.** MCP tool calls stay in-process, but a handler that calls another service through a Connect client over HTTP (`connecthttp.NewTransport`) needs a connection pool sized for its traffic. `http.DefaultClient` keeps only two idle connections per host: under concurrent calls it keeps opening and closing connections, and the closed ones hold local ports until they time out, so a busy service runs out of ports and every call fails with `cannot assign requested address`. In dedo's benchmark that happened at 32 concurrent calls. Create one `http.Client` per backend, with an `http.Transport` whose `MaxIdleConnsPerHost` covers your concurrency (`examples/todo/cmd/todo-mcp` uses 256), and reuse it for every call.

Return errors with a declared reason in a `google.rpc.ErrorInfo` detail. `examples/todo/app/errors.go` has a helper.

`dedo init` writes this `main.go` with your names when the repository has none, and `policy_table_test.go`, so a missing declaration fails `go test` before deploy.

## 7. Authorization: what is declared and what is not

The proto declares **which callers may invoke which RPC**. The interceptor enforces it on Connect, gRPC, REST and MCP alike. You write no checks for that in handlers.

Two parts stay in code:

- **Authentication.** Your token check, a `connect.ServerInterceptor` on the same `connect.Server`, verifies the token and puts the caller's scopes in the call's context; `interceptor.Config.Scopes` reads them from there. You write it once per platform. As an interceptor rather than HTTP middleware, it also covers MCP calls, which reach the server in-process. With `MCP_ACCESS=authenticated`, the same token check also answers `mcpserver.Guard`'s question of whether a request is authenticated at all.
- **Resource-level checks.** A scope says a caller may read products. It does not say *which* products. Ownership of one resource (only the caller's own drafts) stays in handlers.

A new service enforces scopes from the start, as the `main.go` `dedo init` writes does. To add dedo to a service that already has callers, roll out with `EnforceScopes: false` first: the runtime logs every call that would have been denied. Switch to `true` once the logs are clean.

### Multi-tenant services

When the data is partitioned between tenants (customers, sub-accounts, business locations), declare it and the runtime enforces the boundary too:

1. Put the tenant at the root of the resource hierarchy: `locations/{location}/products/{product}`, with paths such as `/commerce/pim/v1/{name=locations/*/products/*}`.
2. Declare it on the service: `option (dedo.v1.service) = {... tenancy: {collection: "locations"}}`. Mark the few RPCs that span tenants, such as listing the caller's own locations, `cross_tenant: true`.
3. Add `DEDO_MULTI_TENANT` to `lint.use` (`dedo init` does), so the lint checks that every resource and every request names its tenant.
4. Pass `Tenants` to `interceptor.Config`: a function that says whether the caller may act in a tenant ID. Your auth interceptor puts what it needs in the context, as for scopes.

The interceptor then reads the tenants of every request, on every transport, from the field the path binds and from every field with a `google.api.resource_reference`, and answers `PERMISSION_DENIED` for another tenant's data before your handler runs. The service will not start if an RPC names no tenant, or if `Tenants` is missing. [RULES.md](RULES.md#multi-tenant-rules) has the details, and [`examples/contacts`](../examples/contacts) a complete contract.

## 8. Change the API safely

- **Breaking changes** fail `buf breaking` against `main`. A breaking change means a new major version: a new package (`.v2`) and a new path (`/v2/`).
- **Deprecating an RPC:** set `deprecated = true` and a `deprecation` with a sunset time and a successor. Clients see `Deprecation` and `Sunset` headers. The docs and the MCP tool show it too.
- **Review:** `.github/CODEOWNERS` requires an API reviewer for everything under `proto/` and the Buf configuration. What you annotate is what you publish, as REST, docs and MCP tools at once.

## 9. CI

`templates/Jenkinsfile.api-stage` (or the GitHub Actions equivalent) runs:

| Check | Fails when |
| --- | --- |
| `make tools` | The lint plugin pinned in `go.mod`, or a house-rules import, cannot be built or fetched, or a rule overrides one it may not |
| `buf format --diff --exit-code` | A `.proto` file is not formatted |
| `buf lint` | Any Buf, AIP, Zalando, dedo or house rule is broken |
| `buf breaking --against '.git#branch=main'` | The change breaks clients |
| `buf generate && git diff --exit-code gen/ docs/` | Generated code or docs are stale |
| `go test ./...` | The policy table test or your tests fail |

## 10. Try the reference service

```sh
make plugins      # once, at the root of this repository
cd examples/todo && go run .
```

Then open <http://localhost:8080/docs> for the REST reference, or call the API:

```sh
curl -H 'Authorization: Bearer writer' -H 'Content-Type: application/json' \
  -d '{"title":"try it"}' localhost:8080/commerce/todo/v1/todos
curl -i -H 'Authorization: Bearer reader' localhost:8080/commerce/todo/v1/todos/1
```

Point an MCP client at `http://localhost:8080/mcp`, with the header `Authorization: Bearer reader` or `Bearer writer`.

## Questions

**Why is the version in the path, when Zalando advises against URL versioning?** The contract follows the AIPs, and the proto package already carries the major version. A breaking change becomes a new package and a new path, with no media-type negotiation.

**How does a client know a server stream is open?** Send a first message that says so: the todo example's `WatchTodos` starts with `CHANGE_WATCHING`. Headers alone are not enough: buffering proxies hold them back, and connect-go v2.0.1's HTTP transport sends none until the first message (fixed in connect-go's main branch by [#1035](https://github.com/connectrpc/connect-go/pull/1035), after v2.0.1).

**Can a service skip REST for an RPC?** No. AIP-127 requires an HTTP binding on every RPC, so every RPC is reachable over every transport.

**Why both a proto comment and a `tool.description`?** The comment is for people who maintain the API. The description is published to API readers and AI agents, and the lint makes sure it exists.

**Where do the options and the lint plugin come from?** A service depends on `buf.build/r-a/dedo` in `buf.yaml` and pins the lint plugin in `go.mod`; `dedo tools` builds it. The examples in this repository are the exception: they test the working tree, so they vendor `options.proto` under `third_party/` and load the plugin from `dist/`.

**Why a native plugin binary instead of the BSR plugin?** Speed in the editor. One lint takes about 0.3 s with the native binary and about 5 s with the WebAssembly one, and the editor reruns it as you type. The BSR plugin (`buf.build/r-a/dedo-lint`) needs no Go, so it suits repositories of protos alone, which `dedo init` sets up with it; it cannot read `rules.yml`, so house rules do not run there.

**How do I run the generators, `protoc-gen-dedo-mcp` and `protoc-gen-dedo-openapi`?** They run on your machine, the same way in the editor, locally and in CI. Pick one:

- **Go repositories:** pin them with `go get -tool ...@vX.Y.Z` and refer to them in `buf.gen.yaml` as `local: ["go", "tool", "protoc-gen-dedo-mcp"]`. This is what `dedo init` sets up.
- **Any repository, without Go:** download the binary for your platform from the GitHub release (`protoc-gen-dedo-openapi_darwin_arm64` and so on) into `.tools/`, and refer to it as `local: .tools/protoc-gen-dedo-openapi`.
- **Installed on your PATH:** `go install github.com/raveesh-me/dedo/openapi/cmd/protoc-gen-dedo-openapi@vX.Y.Z`, then `local: protoc-gen-dedo-openapi`.

If they join Buf's plugin catalog, `remote: buf.build/community/...` becomes a fourth option that needs no install.

**How do I update dedo?** `dedo update`, then `dedo upgrade` in each repository: it moves the options module's label in `buf.yaml` and the lint plugin, generators and runtime in `go.mod` to the new release together, refreshes `buf.lock`, rebuilds the plugin and regenerates the code. CI builds the plugin from the same `go.mod`, so the editor and CI always run the same rules.
