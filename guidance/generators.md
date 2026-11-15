# Adding dedo's generators and server: guidance for coding agents

`dedo init` set this repository up for linting only: no `go.mod` sits beside `buf.yaml`, so it added no generators, server code or `Makefile` targets. Code generation and serving stay with this repository's own tooling. When asked to add dedo's MCP tools or REST reference, follow these steps. Do not run `dedo init` inside a Go module to do it: it writes a `main.go` and a starter service, which an existing server does not want.

`<release>` below is the label of `buf.build/r-a/dedo` in `buf.yaml`. The generators, the runtime and the lint plugin are one release: use that label everywhere, and move them together with `dedo upgrade`.

## Generators

Install them, and run them as local plugins from `PATH`, as the lint plugin is installed:

```sh
go install github.com/raveesh-me/dedo/mcp/cmd/protoc-gen-dedo-mcp@<release>
go install github.com/raveesh-me/dedo/openapi/cmd/protoc-gen-dedo-openapi@<release>
```

If the repository installs its generators from a script or a `Makefile` target, add these two there, pinned to `<release>`, rather than asking each person to.

Add them to the `buf.gen.yaml` that generates the Go code, writing into the same Go output as `protoc-gen-go` and `protoc-gen-connect-go`, with the same `paths=` option:

```yaml
plugins:
  # <Service>MCPTools(): each unary RPC as an MCP tool, with its schemas,
  # text and a typed call.
  - local: protoc-gen-dedo-mcp
    out: gen/go
    opt: paths=source_relative
```

With managed mode, keep dedo's options package on its own Go path, after the repository's own `go_package_prefix` (the last match wins):

```yaml
managed:
  override:
    - file_option: go_package_prefix
      module: buf.build/r-a/dedo
      value: github.com/raveesh-me/dedo/gen/go
```

`protoc-gen-dedo-openapi` documents the `google.api.http` bindings: the REST surface dedo's runtime serves through Vanguard. Add it only where the server serves those routes. A server that mounts Connect only would publish routes that return 404, so keep its existing OpenAPI generator, and ask before replacing a document other teams build against. Where it fits:

```yaml
  - local: protoc-gen-dedo-openapi
    out: docs
    strategy: all            # one document for all files
    opt:
      - title=Your API
      - server=https://api.example.com
```

`protoc-gen-dedo-openapi` documents what a service's protos cannot say. Every field of a `google.type` message gets an example, and, where the plugin request carries no googleapis comments, a description (package `googletype`). A body that is always `{}`, the request body of a `body: "*"` binding whose every field the path binds, or a response without fields, is declared as `enum: [{}]`: a required body and a 200, which is how Vanguard serves it.

`protoc-gen-dedo-openapi` groups the service tags of each package in `x-tagGroups`, which sites such as Docusaurus's OpenAPI plugin build their navigation from; `no_tag_groups` in its `opt` leaves it out.

Both generators take `min_audience`, an `Audience` value with or without its `AUDIENCE_` prefix, and leave out every service whose `audience` is narrower; a service that declares none stays, for lint to report. Without it, they leave out only `AUDIENCE_COMPONENT_INTERNAL` services. A reference published to everyone adds `min_audience=EXTERNAL_PUBLIC` to `opt`, so internal services stay out of it. For a service it leaves out, `protoc-gen-dedo-mcp` still generates `<Service>MCPTools()`, returning no tools, so the server's code keeps compiling.

Both generators also take `access=<path>`, once per file: the access requirements per scope that an auth generator supplies, so that a condition on a scope, such as "only for agencies on the Founders plan", is declared once, in the auth contract, and the REST reference and the MCP tools say it without dedo knowing anything of auth or the contract declaring it twice. An access file (package `accessdoc`) is JSON keyed by scope: `{"scopes": {"taxes/tariff-codes.readonly": {"description": "Only for: Agencies on the Founders plan (`founders`).", "extensions": {"x-commerce-conditions": [...]}, "meta": {"conditions": [...]}}}}`, each field optional. An RPC gets the entry of each scope in its `scopes`: the descriptions as paragraphs of its operation's description, after the scopes, and of its tool's, before its errors, in scope order and then file order; the extensions, each named `x-…` and none of dedo's own, on its operation; and the meta in its tool's `_meta`. A scope no documented RPC declares is ignored. One extension or meta key twice for an RPC, an unreadable file or a field the format does not have fails the run. The refusal itself is not in the file: declare its reason in the method's `errors`, as any other. The path is relative to where `buf generate` runs, so generate the access file first, in a run of its own (commerce-auth's `protoc-gen-commerce-iam` writes one with `opt: access=access.json`), and commit it with the rest.

`protoc-gen-dedo-openapi` also takes `headers=<path>`, once per file: the request headers that every operation declares, which the platform in front of the service requires and the protos cannot say, such as a gateway's `Version` header. dedo assumes none. A header file is JSON, each entry an OpenAPI parameter object without its `in`: `name`, `description` and `schema.type` (`string`, `integer`, `number` or `boolean`) required, `required`, `schema.enum`, `schema.example` and `example` optional:

```json
{
  "headers": [
    {
      "name": "Version",
      "description": "API Version. Send `v4`.",
      "required": true,
      "schema": {"type": "string", "enum": ["v4"], "example": "v4"},
      "example": "v4"
    }
  ]
}
```

```yaml
    opt:
      - title=Your API
      - headers=docs/headers.json
```

Every operation gets each header inline, `"in": "header"`, after its path and query parameters, in file order. A header named like a path or query parameter of an operation, ignoring case, fails the run, and so do one name twice, `Accept`, `Content-Type` or `Authorization` (OpenAPI ignores them; the security scheme documents the token), an example that is not of the type or not one of the enum's values, an unreadable file and a field the format does not have. Give each header an example: HighLevel's public API quality gate requires a description and an example on every parameter, and a header name in kebab-case or Header-Case, such as `Version`. The MCP tools are unchanged: a tool call carries no HTTP headers. The path is relative to where `buf generate` runs.

## Server

Generated MCP tools do nothing until the server mounts dedo's runtime. In the Go module of the server:

```sh
go get github.com/raveesh-me/dedo@<release> github.com/raveesh-me/dedo/mcp@<release>
```

Then wire it on the server's one connect-go v2 `connect.Server`: `policy.Build` for the policy table, `interceptor.New` for scopes, tenants and headers, and `mcpserver.AddTools(tools, connect.NewClient(connectinprocess.New(server)), <pkg>.<Service>MCPTools()...)` for MCP; `vanguard.Mount` too where it serves REST. `dedo templates main.go.tmpl` prints a complete `main.go` to copy from, and [install.md](https://github.com/raveesh-me/dedo/blob/main/guidance/install.md#4-the-server) describes each part, including the `replace` of the MCP Go SDK that `go.mod` needs. A server on connect-go v1 moves to v2 first. Keep the existing auth: it goes in a `connect.ServerInterceptor` on that server, since MCP calls reach it in-process.

Add one service at a time, and keep a test that builds the policy table (`dedo templates policy_table_test.go.tmpl`), so a missing declaration fails `go test` before deploy.
