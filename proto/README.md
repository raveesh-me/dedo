# http-proto-lint options

Protobuf options for declaring a public API's operational contract next to its RPCs. One `.proto` then serves RPC (Connect, gRPC), REST with OpenAPI docs, and MCP tools, with the same auth and behaviour.

```yaml
# buf.yaml
deps:
  - buf.build/cookytech-in/http-proto-lint
```

```protobuf
import "raveesh_me/http_proto_lint/v1/options.proto";
```

| Option | On | Declares |
| --- | --- | --- |
| `raveesh_me.http_proto_lint.v1.service` | service | audience and owner team |
| `raveesh_me.http_proto_lint.v1.method` | RPC | scopes, error reasons, cache policy, deprecation schedule, and the MCP tool: title, description and `ToolAnnotations` (MCP specification 2026-07-28) |
| `raveesh_me.http_proto_lint.v1.message` | message | a description for API readers and AI agents |
| `raveesh_me.http_proto_lint.v1.field` | field | sensitive data |

The house lint rules that enforce these declarations, the Go runtime that applies them, and a step-by-step guide are at [github.com/raveesh-me/http-proto-lint](https://github.com/raveesh-me/http-proto-lint).
