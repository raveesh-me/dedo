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

## Server

Generated MCP tools do nothing until the server mounts dedo's runtime. In the Go module of the server:

```sh
go get github.com/raveesh-me/dedo@<release> github.com/raveesh-me/dedo/mcp@<release>
```

Then wire it on the server's one connect-go v2 `connect.Server`: `policy.Build` for the policy table, `interceptor.New` for scopes, tenants and headers, and `mcpserver.AddTools(tools, connect.NewClient(connectinprocess.New(server)), <pkg>.<Service>MCPTools()...)` for MCP; `vanguard.Mount` too where it serves REST. `dedo templates main.go.tmpl` prints a complete `main.go` to copy from, and [install.md](https://github.com/raveesh-me/dedo/blob/main/guidance/install.md#4-the-server) describes each part, including the `replace` of the MCP Go SDK that `go.mod` needs. A server on connect-go v1 moves to v2 first. Keep the existing auth: it goes in a `connect.ServerInterceptor` on that server, since MCP calls reach it in-process.

Add one service at a time, and keep a test that builds the policy table (`dedo templates policy_table_test.go.tmpl`), so a missing declaration fails `go test` before deploy.
