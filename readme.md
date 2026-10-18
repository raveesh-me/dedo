# http-proto-lint

A proto-first standard for public ConnectRPC APIs. The `.proto` file declares the contract and its operational rules. A Buf check plugin enforces both before merge, and a small Go runtime applies the operational rules on every call.

New here? Read [guideline.md](guideline.md). It walks through building a service in a new repository.

Contract rules come from Google's AIPs. Operational rules (audience, ownership, scopes, caching, deprecation, error reasons) come from Zalando's guidelines. Both live in the schema.

## What is in this repository

| Component | Path | Job |
| --- | --- | --- |
| Options module | `proto/raveesh_me/http_proto_lint/v1/options.proto` | Declares audience, owner, scopes, cache policy, deprecation, error reasons and sensitive fields. Published as `buf.build/cookytech-in/http-proto-lint`. |
| House lint plugin | `lint/` | Thirteen `RAVEESH_ME_*` rules. Published as the Wasm check plugin `buf.build/cookytech-in/http-proto-lint-plugin`. |
| Policy table | `policy/` | Reads the options from descriptors at startup. Fails on a missing declaration. |
| Flow ID | `flowid/` | Reads or creates `X-Flow-ID` and propagates it on outbound calls. |
| Policy interceptor | `interceptor/` | Checks scopes, sets `Cache-Control`, `Deprecation` and `Sunset`, and logs undeclared error reasons. |
| REST doc generator | `openapi/` | `protoc-gen-rest-openapi`: an OpenAPI 3.1 document from the `google.api.http` bindings, with scopes, error reasons, deprecation and the declared descriptions. Connect routes are left out. |
| MCP adapter | `mcp/` | Serves the tools that `protoc-gen-go-mcp` generates with the declared title, description and behaviour hints. Tool calls run through the same auth and policy as REST. |
| Templates | `templates/` | `buf.yaml`, `buf.gen.yaml`, `Makefile.api`, VS Code settings, a Jenkins stage and a policy-table test for service repositories. |
| Examples | `examples/` | `library`: a contract-only module. `todo` and `bad-todo`: two runnable services with the same interface, one compliant and one breaking every rule. |

The root Go module (`github.com/raveesh-me/http-proto-lint`) holds the runtime and the generated options code. `lint/`, `openapi/` and `mcp/` are separate modules, so services pull in only what they use.

## Declaring a contract

```protobuf
service LibraryService {
  option (raveesh_me.http_proto_lint.v1.service) = {
    audience: AUDIENCE_EXTERNAL_PUBLIC
    owner_team: "library-team"
  };

  // GetBook returns one book.
  rpc GetBook(GetBookRequest) returns (GetBookResponse) {
    option idempotency_level = NO_SIDE_EFFECTS;
    option (google.api.http) = {
      get: "/v1/{name=shelves/*/books/*}"
      response_body: "book"
    };
    option (raveesh_me.http_proto_lint.v1.method) = {
      scopes: "library.book.read"
      cache: {visibility: VISIBILITY_PRIVATE, max_age: {seconds: 30}}
      errors: {reason: "BOOK_NOT_FOUND", code: NOT_FOUND}
    };
  }
}
```

See `examples/library/proto` for a complete module.

## Lint rules

| Rule ID | Checks |
| --- | --- |
| `RAVEESH_ME_SERVICE_RULES_REQUIRED` | Every service declares the service option with an audience and an owner team |
| `RAVEESH_ME_METHOD_RULES_REQUIRED` | Every RPC declares the method option with at least one scope and one error reason |
| `RAVEESH_ME_SCOPE_NAME_FORMAT` | Scopes match `<app>.<resource>.<read\|write>` |
| `RAVEESH_ME_ERROR_REASON_FORMAT` | Reasons are UPPER_SNAKE_CASE. Codes are neither `OK` nor `UNKNOWN`. |
| `RAVEESH_ME_CACHE_REQUIRES_NO_SIDE_EFFECTS` | A cache policy appears only on `NO_SIDE_EFFECTS` methods, with a visibility and a positive max age |
| `RAVEESH_ME_DEPRECATION_CONSISTENT` | `deprecated = true` and a deprecation schedule appear together, sunset follows deprecation, and the successor exists |
| `RAVEESH_ME_READ_METHOD_NO_SIDE_EFFECTS` | Unary `Get*` and `List*` RPCs set `idempotency_level = NO_SIDE_EFFECTS` |
| `RAVEESH_ME_HTTP_RESPONSE_BODY` | HTTP rules of `Get*`, `Create*` and `Update*` RPCs set `response_body` |
| `RAVEESH_ME_HTTP_PATH_VERSION` | Every HTTP path, additional bindings included, contains the package version, such as `/v1/`, after a root that is the same for the whole service. The root itself, such as `/commerce/pim`, is the owner's choice. |
| `RAVEESH_ME_NO_JSON_NAME` | No field overrides the protojson default name |
| `RAVEESH_ME_MONEY_FIELD_TYPE` | Fields named `price`, `amount`, `*_price` or `*_amount` use `google.type.Money` |
| `RAVEESH_ME_TOOL_REQUIRED` | Every unary RPC declares a tool title and description for MCP and the REST docs. Delete and Remove RPCs are marked destructive. |
| `RAVEESH_ME_MESSAGE_DESCRIPTION_REQUIRED` | Every message declares a consumer-facing description |

Suppress one finding with `// buf:lint:ignore RAVEESH_ME_MONEY_FIELD_TYPE` above the element, followed by the reason.

## Using it in a service repository

1. Copy `templates/buf.yaml`, `templates/buf.gen.yaml`, `templates/Makefile.api` and `templates/vscode/*` (into `.vscode/`). Replace `YOUR_ORG/YOUR_REPO`.
2. Run `make api-update` (`buf dep update` and `buf plugin update`). Commit `buf.lock`. It pins the options module and both check plugins.
3. Run `make api-check`. VS Code shows the same findings through the Buf extension (`buf lsp serve`).
4. Wire the runtime:

```go
table, err := policy.Build(libraryv1connect.LibraryServiceName)
if err != nil {
	return err
}
guard, err := interceptor.New(interceptor.Config{
	Table:         table,
	Scopes:        auth.ScopesFromContext, // your auth middleware
	EnforceScopes: false,                  // audit first, then enforce
	Log:           log,
})
if err != nil {
	return err
}
path, handler := libraryv1connect.NewLibraryServiceHandler(
	library.NewServer(),
	connect.WithInterceptors(guard, validator), // policy before validation
)
mux := http.NewServeMux()
mux.Handle(path, handler)
return http.ListenAndServe(":8080", flowid.Middleware(auth.Middleware(mux)))
```

5. Add `templates/policy_table_test.go.tmpl` as a test, so a missing declaration fails before deploy.

`buf.gen.yaml` uses managed mode. A module-scoped `go_package_prefix` override points imports of the options module at this repository's Go module, so generated code links against the runtime's types.

Known v1 gaps: error responses carry no deprecation headers, and streaming calls get the scope check only.

## Examples: `todo` and `bad-todo`

Two Go services, each laid out like a service repository built from `templates/`, with its own `buf.yaml`, `buf.gen.yaml`, `go.mod` and proto files. Both serve the same `TodoService` over Connect, gRPC and REST (through Vanguard), with the same server code and the full runtime: policy table, scope interceptor, flow IDs and error audit.

- `examples/todo` follows the standard. `buf lint` is clean. Its tests check scopes, CRUD, pagination, cache and deprecation headers, REST unwrapping and the error audit end to end. It also serves the REST reference at `/docs` and an MCP server at `/mcp`, and its tests check that MCP tools carry the declared text and enforce the same scopes.
- `examples/bad-todo` breaks all thirteen house rules, plus one Buf and one AIP rule. Open `proto/raveesh_me/todo/v1/todo.proto` in VS Code to see the findings. `expected-lint.txt` lists them, and `app/app_test.go` shows what each mistake does at runtime: a misnamed scope locks out writers, an unversioned path moves the endpoint, undeclared reasons show up in logs, and a custom `json_name` renames the field for every JSON client.

Until the options module and plugins are published, each example vendors `options.proto` under `third_party/` and loads the plugins from `dist/`. Run `make plugins` once so the editor finds them, then `make examples`.

```sh
cd examples/todo && go run . &
curl -H 'Authorization: Bearer writer' -H 'Content-Type: application/json' -d '{"title":"try it"}' localhost:8080/commerce/todo/v1/todos
curl -i -H 'Authorization: Bearer reader' localhost:8080/commerce/todo/v1/todos/1
open http://localhost:8080/docs
```

## Developing

```sh
make check        # format, generate, Go tests, Wasm builds, buf lint, examples
make aip-plugin   # build buf-plugin-aip at the pinned commit as Wasm
make breaking     # options module against main
```

`make check` needs `buf` and `jq`. Code generators are pinned in each `go.mod` as Go tools and run through `go tool`. The root `buf.yaml` lints the options module and the example with the locally built plugin from `dist/`.

Lint rule tests live in `lint/rules/testdata/<rule>/{pass,fail}`. A pass fixture must produce no finding. A fail fixture marks each line that must produce one with `// want`.

## Releasing

Pushing a `v*` tag runs `.github/workflows/ci.yaml`, which publishes one version everywhere:

- `buf push` of the options module with the tag as label
- `buf plugin push` of `buf.build/cookytech-in/http-proto-lint-plugin` and `buf.build/cookytech-in/buf-plugin-aip`
- a GitHub release carrying both `.wasm` files

The Go module version is the same tag. The job reads a `BUF_KEY` repository secret: a Buf token for the `cookytech-in` organization with module and plugin create and push scopes.

`buf-plugin-aip` is a young third-party project ([hestiatechnology/buf-plugin-aip](https://github.com/hestiatechnology/buf-plugin-aip), Apache-2.0). It is pinned to commit `5158ec0f16f3` and republished from that commit.
