# dedo options

Protobuf options for declaring a public API's operational contract next to its RPCs. One `.proto` then serves RPC (Connect, gRPC), REST with OpenAPI docs, and MCP tools, with the same auth and behaviour.

```yaml
# buf.yaml
deps:
  - buf.build/r-a/dedo
```

```protobuf
import "dedo/v1/options.proto";
```

| Option | On | Declares |
| --- | --- | --- |
| `dedo.v1.service` | service | audience and owner team |
| `dedo.v1.method` | RPC | scopes, error reasons, cache policy, deprecation schedule, and the MCP tool: title, description and `ToolAnnotations` (MCP specification 2026-07-28) |
| `dedo.v1.message` | message | a description for API readers and AI agents |
| `dedo.v1.field` | field | sensitive data |

The lint rules that enforce these declarations, the Go runtime that applies them, and a step-by-step guide are at [github.com/raveesh-me/dedo](https://github.com/raveesh-me/dedo).

Licensed under the Apache License 2.0.
