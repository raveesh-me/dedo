# dedo

**Define once, deploy everywhere.** Author one `.proto`, ship it everywhere. The same contract serves your API as RPC (Connect and gRPC), REST and MCP tools for AI agents, with the same auth, documentation and behaviour on every transport:

| From one `.proto` you get | How |
| --- | --- |
| RPC over Connect and gRPC | connect-go |
| REST, with resource-style URLs | `google.api.http` annotations, served by Vanguard |
| REST reference docs (OpenAPI 3.1) | `protoc-gen-dedo-openapi` |
| MCP tools for AI agents | `protoc-gen-go-mcp` with the `mcpserver` adapter |
| Authorization, cache and deprecation headers, flow IDs, error audit | The policy runtime, read from options in the `.proto` |

**First-class lint guidance.** A Buf check plugin turns the standard into findings in your editor as you type, each linked to an explanation in [HOUSE_RULES.md](HOUSE_RULES.md). The same `buf lint` gates CI. Handlers carry business logic only.

New here? [install.md](install.md) builds a new product from scratch, step by step. [guideline.md](guideline.md) explains the reasoning.

**Latest release: [v0.5.0](https://github.com/raveesh-me/dedo/releases/tag/v0.5.0).** The options module is [`buf.build/r-a/dedo`](https://buf.build/r-a/dedo). The lint plugins come as native binaries on the GitHub release (`make tools`) and as Wasm plugins on the BSR. The Go modules are `github.com/raveesh-me/dedo/...@v0.5.0`.

Contract rules come from Google's AIPs. Operational rules (audience, ownership, scopes, caching, deprecation, error reasons) come from Zalando's guidelines. Both live in the schema.

## What is in this repository

| Component | Path | Job |
| --- | --- | --- |
| Options module | `proto/dedo/v1/options.proto` | Declares audience, owner, scopes, cache policy, deprecation, error reasons, sensitive fields, message descriptions, and how each RPC appears as an MCP tool (title, description and the MCP specification's `ToolAnnotations`). Published as `buf.build/r-a/dedo`. |
| House lint plugin | `lint/` | Fourteen house rules, `HOUSE_*` in the `HOUSE` category. Released as native binaries per platform (fast in editors) and as the Wasm check plugin `buf.build/r-a/dedo-lint`. |
| Policy table | `policy/` | Reads the options from descriptors at startup. Fails on a missing declaration. |
| Flow ID | `flowid/` | Reads or creates `X-Flow-ID` and propagates it on outbound calls. |
| Policy interceptor | `interceptor/` | Checks scopes, sets `Cache-Control`, `Deprecation` and `Sunset`, and logs undeclared error reasons. |
| REST doc generator | `openapi/` | `protoc-gen-dedo-openapi`: an OpenAPI 3.1 document from the `google.api.http` bindings, with scopes, error reasons, deprecation and the declared descriptions. Connect routes are left out. |
| MCP adapter and schemas | `mcp/` | `protoc-gen-dedo-mcp` writes each tool's input schema at code-generation time: field descriptions from comments, `OUTPUT_ONLY` fields left out, partial-update and create semantics from AIP. `mcpserver` serves the tools `protoc-gen-go-mcp` generates under short snake_case names (`get_todo`), with those schemas, the declared title, description, `ToolAnnotations` and error reasons, and without deprecated RPCs. Tool calls run through the same auth and policy as REST. |
| Templates | `templates/` | `buf.yaml`, `buf.gen.yaml`, `Makefile.api`, VS Code settings, a Jenkins stage and a policy-table test for service repositories. |
| Examples | `examples/` | `library`: a contract-only module. `todo` and `bad-todo`: two runnable services with the same interface, one compliant and one breaking every rule. |

The root Go module (`github.com/raveesh-me/dedo`) holds the runtime and the generated options code. `lint/`, `openapi/` and `mcp/` are separate modules, so services pull in only what they use. A `go.work` workspace builds them against each other locally, so no `go.mod` carries a `replace` and `go install <module>@<version>` works.

## Declaring a contract

One RPC with every declaration the standard asks for, from `examples/library` (which CI lints):

```protobuf
service LibraryService {
  option (dedo.v1.service) = {
    audience: AUDIENCE_EXTERNAL_PUBLIC
    owner_team: "library-team"
  };

  // GetBook returns one book.
  //
  // Errors: BOOK_NOT_FOUND.
  rpc GetBook(GetBookRequest) returns (GetBookResponse) {
    option idempotency_level = NO_SIDE_EFFECTS;
    option (google.api.http) = {
      get: "/v1/{name=shelves/*/books/*}"
      response_body: "book"
    };
    option (dedo.v1.method) = {
      tool: {
        title: "Get a book"
        description: "Returns one book by its resource name, such as shelves/1/books/2."
        annotations: {
          read_only_hint: true
          open_world_hint: false
        }
      }
      scopes: "library.book.read"
      cache: {
        visibility: VISIBILITY_PRIVATE
        max_age: {seconds: 30}
      }
      errors: {
        reason: "BOOK_NOT_FOUND"
        code: NOT_FOUND
      }
    };
  }
}

// GetBookRequest is the request of GetBook.
message GetBookRequest {
  option (dedo.v1.message) = {description: "Which book to get."};

  // The resource name of the book.
  string name = 1 [
    (google.api.field_behavior) = REQUIRED,
    (google.api.resource_reference) = {type: "library.raveesh.me/Book"}
  ];
}
```

See `examples/library/proto` for a complete module.

## Lint rules

| Rule ID | Checks |
| --- | --- |
| `HOUSE_SERVICE_RULES_REQUIRED` | Every service declares the service option with an audience and an owner team |
| `HOUSE_METHOD_RULES_REQUIRED` | Every RPC declares the method option with at least one scope and one error reason |
| `HOUSE_SCOPE_NAME_FORMAT` | Scopes match `<app>.<resource>.<read\|write>` |
| `HOUSE_ERROR_REASON_FORMAT` | Reasons are UPPER_SNAKE_CASE. Codes are neither `OK` nor `UNKNOWN`. |
| `HOUSE_CACHE_REQUIRES_NO_SIDE_EFFECTS` | A cache policy appears only on `NO_SIDE_EFFECTS` methods, with a visibility and a positive max age |
| `HOUSE_DEPRECATION_CONSISTENT` | `deprecated = true` and a deprecation schedule appear together, sunset follows deprecation, and the successor exists |
| `HOUSE_READ_METHOD_NO_SIDE_EFFECTS` | Unary `Get*` and `List*` RPCs set `idempotency_level = NO_SIDE_EFFECTS` |
| `HOUSE_HTTP_RESPONSE_BODY` | HTTP rules of `Get*`, `Create*` and `Update*` RPCs set `response_body` |
| `HOUSE_HTTP_PATH_VERSION` | Every HTTP path, additional bindings included, contains the package version, such as `/v1/`, after a root that is the same for the whole service. The root itself, such as `/commerce/pim`, is the owner's choice. |
| `HOUSE_NO_JSON_NAME` | No field overrides the protojson default name |
| `HOUSE_MONEY_FIELD_TYPE` | Fields named `price`, `amount`, `*_price` or `*_amount` use `google.type.Money` |
| `HOUSE_TOOL_REQUIRED` | Every unary RPC declares a tool title and description for MCP and the REST docs |
| `HOUSE_TOOL_ANNOTATIONS` | Every unary RPC declares the MCP `ToolAnnotations` hints (spec 2026-07-28), consistent with its `idempotency_level` |
| `HOUSE_MESSAGE_DESCRIPTION_REQUIRED` | Every message declares a consumer-facing description |

[HOUSE_RULES.md](HOUSE_RULES.md) explains each rule with examples. Every finding ends with a link to its section, pinned to the plugin's release tag.

Suppress one finding with `// buf:lint:ignore HOUSE_MONEY_FIELD_TYPE` above the element, followed by the reason.

## Using it in a service repository

[install.md](install.md) walks through it end to end with a working example. In short:

1. Copy the files in `templates/` (`buf.yaml`, `buf.gen.yaml`, `Makefile.api`, `CODEOWNERS`, VS Code settings, the Jenkins stage) and replace the `YOUR_*` placeholders.
2. Pin the code generators in `go.mod` with `go get -tool`, run `make tools` for the native lint plugins, and run `buf dep update`.
3. Write the contract, then `make api-check` (lint, format, breaking) and `buf generate` (Go, Connect, MCP tools, REST docs).
4. Wire the runtime: `policy.Build` for the policy table, `interceptor.New` for scopes and headers, Vanguard for REST, `mcpserver.New` with the generated `MCPInputSchemas` for MCP, and the docs handler. Start with `EnforceScopes: false` (audit mode), then enforce.
5. Add `templates/policy_table_test.go.tmpl` as a test, so a missing declaration fails before deploy.

`buf.gen.yaml` uses managed mode. A module-scoped `go_package_prefix` override points imports of the options module at this repository's Go module, so generated code links against the runtime's types.

Known gaps: error responses carry no deprecation headers, and streaming calls get the scope check only, with no error audit.

## Examples: `todo` and `bad-todo`

Two Go services, each laid out like a service repository built from `templates/`, with its own `buf.yaml`, `buf.gen.yaml`, `go.mod` and proto files. Both serve the same `TodoService` over Connect, gRPC and REST (through Vanguard), with the same server code and the full runtime: policy table, scope interceptor, flow IDs and error audit.

- `examples/todo` follows the standard. `buf lint` is clean. Its tests check scopes, CRUD, pagination, cache and deprecation headers, REST unwrapping and the error audit end to end. It also serves the REST reference at `/docs` (Stoplight Elements, rendering the generated `/openapi.json`) and an MCP server at `/mcp`. Its tests check that MCP tools carry the declared text and `ToolAnnotations` and enforce the same scopes, and that a change made through MCP reaches an open `WatchTodos` stream at once.
- `examples/bad-todo` breaks all fourteen house rules, plus one Buf and one AIP rule. Open `proto/raveesh_me/todo/v1/todo.proto` in VS Code to see the findings. `expected-lint.txt` lists them, and `app/app_test.go` shows what each mistake does at runtime: a misnamed scope locks out writers, an unversioned path moves the endpoint, undeclared reasons show up in logs, and a custom `json_name` renames the field for every JSON client.

`examples/todo` also runs as a deployment, with everything built in containers (podman, kind and kubectl are the only host tools):

```sh
cd examples/todo
make up      # kind cluster "todo-example": the service and a live Vue UI at http://localhost:8090
make e2e     # Playwright: an agent adds a to-do over MCP and the open UI shows it without a reload
make down
```

The UI (`web/`) uses TanStack Query through [connect-tan-vue](https://github.com/raveesh-me/connect-tan-vue)'s `createClientWithQuery`, and stays live through the `WatchTodos` server stream. [examples/todo/MCP.md](examples/todo/MCP.md) shows how to connect Claude Code or Claude Desktop to the service's MCP endpoint.

The examples test this repository's working tree, not a release: each vendors `options.proto` under `third_party/` and loads the lint plugins from `dist/`. Run `make plugins` once so the editor finds them, then `make examples`. Rerun `make plugins` after changing a rule. Service repositories use the published module and plugins instead.

```sh
cd examples/todo && go run . &
curl -H 'Authorization: Bearer writer' -H 'Content-Type: application/json' -d '{"title":"try it"}' localhost:8080/commerce/todo/v1/todos
curl -i -H 'Authorization: Bearer reader' localhost:8080/commerce/todo/v1/todos/1
open http://localhost:8080/docs
```

## Lint performance

The plugins run natively wherever they can. On `examples/todo`, one `buf lint` with both plugins takes:

| Plugin form | Time |
| --- | --- |
| Native binaries (default) | 0.34 s |
| WebAssembly | 6.0 s |

The editor reruns the lint as you type, so this is the difference between findings that keep up and findings that lag. Most of the WebAssembly time is the AIP plugin: a 45 MB module. Buf caches the compiled module, so the cost is in running it, and a smaller build does not help. `buf.yaml` refers to the native binaries by path, so an editor started outside a shell finds them too. The WebAssembly plugins on the BSR stay available for setups that cannot download binaries, such as Windows.

## Developing

```sh
make check          # format, generate, Go tests, plugin builds, buf lint, examples
make plugins        # both lint plugins, natively for this machine, into dist/
make wasm-plugins   # both lint plugins as WebAssembly, for the BSR
make breaking       # options module against main
```

`make check` needs `buf` and `jq`. Code generators are pinned in each `go.mod` as Go tools and run through `go tool`. The root `buf.yaml` lints the options module (Buf rules only, since it defines the extensions) and `examples/library` (all rules, with the locally built plugin from `dist/`).

Lint rule tests live in `lint/rules/testdata/<rule>/{pass,fail}`. A pass fixture must produce no finding. A fail fixture marks each line that must produce one with `// want`.

## Releasing

A pull request that breaks the options module fails CI's `buf breaking` check. When the break is deliberate, label the PR `allow-breaking`, re-run the check, and release a new minor version.

Before tagging, set the new version in `templates/Makefile.api` (`DEDO_VERSION`), the `@vX.Y.Z` pins in `install.md` and `guideline.md`, and the readme's "Latest release" line. The release job runs `scripts/check-release-versions.sh` first and stops if any of them names another version.

To release, tag the merge commit on `main` with `lint/vX.Y.Z`, `openapi/vX.Y.Z` and `mcp/vX.Y.Z` (so `go get` works for the nested modules) and push those first. Then push `vX.Y.Z` **on its own**: GitHub starts no workflow when more than three tags arrive in one push.

The `v*` tag runs `.github/workflows/ci.yaml`, which publishes one version everywhere:

- `buf push` of the options module with the tag and `main` as labels; `main` is the default label that unpinned dependencies resolve to
- `buf plugin push` of `buf.build/r-a/dedo-lint` and `buf.build/r-a/buf-plugin-aip`
- a GitHub release carrying both lint plugins and both generators (`protoc-gen-dedo-mcp`, `protoc-gen-dedo-openapi`) as native binaries for macOS and Linux (arm64 and amd64), and the lint plugins as `.wasm` files. `make tools` downloads the lint plugins.

The Go module version is the same tag. The job reads a `BUF_KEY` repository secret: a Buf token for the `r-a` organization with module and plugin create and push scopes.

`buf-plugin-aip` is a young third-party project ([hestiatechnology/buf-plugin-aip](https://github.com/hestiatechnology/buf-plugin-aip), Apache-2.0). It is pinned to commit `5158ec0f16f3` and republished from that commit.

## Why "dedo"?

dedo stands for *define once, deploy everywhere*.

It is also a word heard a lot in the author's home. The author's daughter is one, and whenever she wants something she says "de do", Hindi for "give it to me". She says it so often that the family has started calling her Dedo.

The project works the same way: you hand it one `.proto`, and it gives back RPC, REST, docs and MCP tools.

## License

[Apache License 2.0](LICENSE).
