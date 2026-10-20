# Building an API with dedo

This guide explains how the pieces fit and why. For a copy-paste walkthrough of a new product, follow [install.md](install.md).

This guide takes you from an empty repository to a running service. Read it once end to end before you start. `examples/todo` is the reference: every step below exists there in working form.

## What you get

You write one `.proto` file per API. From it you get:

- **Three transports with one behaviour.** Connect, gRPC and REST serve the same handlers. REST paths come from `google.api.http` annotations.
- **Authorization without code.** Each RPC declares its scopes. The runtime rejects callers without one, on every transport.
- **REST docs.** `protoc-gen-dedo-openapi` writes an OpenAPI 3.1 document of the REST API only. Connect's own routes stay out of it.
- **MCP tools.** Every unary RPC becomes an MCP tool for AI agents, named after the RPC in snake_case (`GetProduct` becomes `get_product`). Tool calls pass through the same scope checks as REST calls.
- **A blocking review gate.** `buf lint` runs Buf's rules and buf-plugin-dedo's rules (`AIP_*` from Google's AIPs, `ZALANDO_*` from Zalando's guidelines, `DEDO_*` of its own), plus any `HOUSE_*` rules your organisation adds, in your editor and in CI. [RULES.md](RULES.md) lists them, says which side wins when two sources disagree, and which rules dedo skips.

Your handlers contain business logic only. Scope checks, cache and deprecation headers, flow IDs and error auditing come from the runtime.

## 1. Before you start

Install:

- Go 1.25 or later
- The [Buf CLI](https://buf.build/docs/installation)
- VS Code with the Buf extension (`bufbuild.vscode-buf`). Remove other Protobuf extensions.

Decide two names with your team:

| Decision | Example | Who decides |
| --- | --- | --- |
| Proto package | `acme.products.v1` | You. Use `<org>.<platform>.v<major>`. |
| REST root | `/commerce/products` or `/commerce/pim` | You. The linter does not care what it is. |

Every REST path is `<root>/v<major>/<resource path>`, for example `/commerce/pim/v1/products/{product}`. The version segment is mandatory and must match the package version. The root must be the same for every RPC of a service. Each root segment is one lowercase or lowerCamelCase word: `/commerce/timeCounter` works, and `/commerce/time-counter` fails AIP-127's path syntax check.

## 2. Create the repository

```text
my-service/
  proto/acme/products/v1/products.proto
  buf.yaml            # from templates/buf.yaml
  buf.gen.yaml        # from templates/buf.gen.yaml
  Makefile            # include templates/Makefile.api
  .github/CODEOWNERS  # from templates/CODEOWNERS
  .vscode/            # from templates/vscode/
  go.mod
  main.go
```

1. Copy the files from `templates/` and replace every `YOUR_*` placeholder.
2. Pin the code generators in `go.mod`, so every machine generates the same code:

   ```sh
   go get -tool google.golang.org/protobuf/cmd/protoc-gen-go
   go get -tool connectrpc.com/connect/cmd/protoc-gen-connect-go
   go get -tool github.com/raveesh-me/dedo/mcp/cmd/protoc-gen-dedo-mcp@v0.6.0
   go get -tool github.com/raveesh-me/dedo/openapi/cmd/protoc-gen-dedo-openapi@v0.6.0
   ```

   Pin the dedo modules to the latest release explicitly. Go's module proxy can lag behind a new tag.

   Then point the `buf.gen.yaml` plugins at them with `local: ["go", "tool", "<generator>"]`, as `examples/todo/buf.gen.yaml` does.
3. Run `make tools` to download the native lint plugins for your machine into `.tools/`, and add `.tools/` to `.gitignore`. Run `make api-update` (`buf dep update`) and commit `buf.lock`.
4. Pin `connectrpc.com/vanguard` to `v0.4.0`. Later versions need connect-go v2.

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
make api-check     # format, lint, breaking-change check
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
make api-generate
```

This writes, for each service:

- `gen/go/.../<pkg>.pb.go` and `<pkg>connect/` for the API
- `gen/go/.../<pkg>.mcp_schema.pb.go` with the MCP tools' input and output schemas
- `docs/openapi.json` for the REST reference

Commit all of it. CI fails when the committed output is stale.

## 6. Wire the service

`examples/todo/app/app.go` and `app/mcp.go` show the whole setup. In short:

```go
// The API: policy table, policy interceptor, REST transcoding, auth, flow IDs.
table, err := policy.Build(productsv1connect.ProductServiceName) // fails on a missing declaration
guard, err := interceptor.New(interceptor.Config{
	Table:         table,
	Scopes:        auth.ScopesFromContext, // your token middleware
	EnforceScopes: false,                  // start in audit mode, then enforce
	Log:           log,
})
path, handler := productsv1connect.NewProductServiceHandler(server, connect.WithInterceptors(guard))
transcoder, err := vanguard.NewTranscoder([]*vanguard.Service{vanguard.NewService(path, handler)})
api := flowid.Middleware(auth.Middleware(transcoder))

// MCP: every RPC is a tool, forwarded to the API in-process with the caller's
// token, using the schemas protoc-gen-dedo-mcp generated.
mcpServer := mcp.NewServer(&mcp.Implementation{Name: "products", Version: "v1"}, nil)
err = mcpserver.New(mcpServer, mcpserver.Config{
	Services:      []protoreflect.ServiceDescriptor{serviceDescriptor},
	InputSchemas:  productsv1.MCPInputSchemas,
	OutputSchemas: productsv1.MCPOutputSchemas,
	Client:        mcpserver.InProcessClient(api),
}) // fails if an RPC has no schema, or two RPCs share a tool name

mux := http.NewServeMux()
mux.Handle("GET /openapi.json", docs.Spec()) // embeds docs/openapi.json
mux.Handle("GET /docs", docs.Page())
// Cross-origin protection: the MCP transport requires an Origin check.
mux.Handle("/mcp", flowid.Middleware(http.NewCrossOriginProtection().Handler(mcp.NewStreamableHTTPHandler(
	func(*http.Request) *mcp.Server { return mcpServer },
	&mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true}))))
mux.Handle("/", api)
```

Return errors with a declared reason in a `google.rpc.ErrorInfo` detail. `examples/todo/app/errors.go` has a helper.

Add `templates/policy_table_test.go.tmpl` as a test, so a missing declaration fails before deploy.

## 7. Authorization: what is declared and what is not

The proto declares **which callers may invoke which RPC**. The interceptor enforces it on Connect, gRPC, REST and MCP alike. You write no checks for that in handlers.

Two parts stay in code:

- **Authentication.** Your token middleware verifies the token and puts the caller's scopes in the request context. You write it once per platform and plug it in as `interceptor.Config.Scopes`.
- **Resource-level checks.** A scope says a caller may read products. It does not say *which* products, for example only those of the caller's own location. Handlers still check ownership and tenancy.

Roll out with `EnforceScopes: false` first. The runtime logs every call that would have been denied. Switch to `true` once the logs are clean.

## 8. Change the API safely

- **Breaking changes** fail `buf breaking` against `main`. A breaking change means a new major version: a new package (`.v2`) and a new path (`/v2/`).
- **Deprecating an RPC:** set `deprecated = true` and a `deprecation` with a sunset time and a successor. Clients see `Deprecation` and `Sunset` headers. The docs and the MCP tool show it too.
- **Review:** `.github/CODEOWNERS` requires an API reviewer for everything under `proto/` and the Buf configuration. What you annotate is what you publish, as REST, docs and MCP tools at once.

## 9. CI

`templates/Jenkinsfile.api-stage` (or the GitHub Actions equivalent) runs:

| Check | Fails when |
| --- | --- |
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

**Can a service skip REST for an RPC?** No. AIP-127 requires an HTTP binding on every RPC, so every RPC is reachable over every transport.

**Why both a proto comment and a `tool.description`?** The comment is for people who maintain the API. The description is published to API readers and AI agents, and the lint makes sure it exists.

**Where do the options and plugins come from?** A service depends on `buf.build/r-a/dedo` in `buf.yaml` and gets the lint plugins with `make tools`. The examples in this repository are the exception: they test the working tree, so they vendor `options.proto` under `third_party/` and load the plugins from `dist/`.

**Why native plugin binaries instead of the BSR plugins?** Speed in the editor. One lint takes about 0.3 s with native plugins and about 6 s with the WebAssembly ones, and the editor reruns it as you type. The BSR plugin (`buf.build/r-a/dedo-lint`) needs no download and works on Windows. Use it there.

**How do I run the generators, `protoc-gen-dedo-mcp` and `protoc-gen-dedo-openapi`?** They run on your machine, the same way in the editor, locally and in CI. Pick one:

- **Go repositories:** pin them with `go get -tool ...@vX.Y.Z` and refer to them in `buf.gen.yaml` as `local: ["go", "tool", "protoc-gen-dedo-mcp"]`. This is the default in `templates/` and `install.md`.
- **Any repository, without Go:** download the binary for your platform from the GitHub release (`protoc-gen-dedo-openapi_darwin_arm64` and so on) into `.tools/`, and refer to it as `local: .tools/protoc-gen-dedo-openapi`.
- **Installed on your PATH:** `go install github.com/raveesh-me/dedo/openapi/cmd/protoc-gen-dedo-openapi@vX.Y.Z`, then `local: protoc-gen-dedo-openapi`.

If they join Buf's plugin catalog, `remote: buf.build/community/...` becomes a fourth option that needs no install.

**How do I update the lint rules?** Bump `DEDO_VERSION` in `Makefile.api` and run `make tools`. CI does the same, so the editor and CI always run the same version.
