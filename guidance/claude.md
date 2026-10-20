# Building an API with dedo: guidance for coding agents

Append this file to your repository's `AGENTS.md` or `CLAUDE.md`; `dedo init` does it for you. dedo's full guidance: [install.md](https://github.com/raveesh-me/dedo/blob/main/guidance/install.md) sets a service up, [tutorial.md](https://github.com/raveesh-me/dedo/blob/main/guidance/tutorial.md) builds one from scratch, [guideline.md](https://github.com/raveesh-me/dedo/blob/main/guidance/guideline.md) explains the reasoning, and [RULES.md](https://github.com/raveesh-me/dedo/blob/main/guidance/RULES.md) documents every lint rule. It tells a coding agent how a service built with [dedo](https://github.com/raveesh-me/dedo) works and which mistakes to avoid.

## The proto is the source of truth

- One `.proto` defines the API. dedo serves it as Connect/gRPC, REST (`google.api.http`, through Vanguard), an OpenAPI 3.1 reference and MCP tools. Change the proto, never the generated output.
- Never edit anything under `gen/` or `docs/openapi.json`. Run `make api-generate` and commit the result; CI fails on stale output.
- Set up and upgrade dedo with `dedo init` (after `go install github.com/raveesh-me/dedo/cmd/dedo@<version>`), not by hand-editing pins: it moves `DEDO_VERSION`, the generators and the runtime together.
- Before you finish a change to `proto/`, run `make api-check` (format, lint, breaking). Fix findings in the proto. Suppress one only with `// buf:lint:ignore <RULE_ID>` and a written reason.
- Scopes, cache policy, deprecation and error reasons are declared in the proto (`dedo.v1` options) and enforced by the runtime. Do not write scope checks, cache headers or deprecation headers in handlers.
- Return errors with a declared reason in a `google.rpc.ErrorInfo` detail. Undeclared reasons are logged as audit findings.

## Lint classes and precedence

`buf-plugin-dedo-lint` reports four classes of rules:

| Prefix | Source |
| --- | --- |
| `AIP_*` | Google's AIPs, through api-linter |
| `ZALANDO_*` | The Zalando RESTful API guidelines dedo adopts |
| `DEDO_*` | dedo's own conventions |
| `HOUSE_*` | Your organisation's own plugin, if it has one |

When sources disagree, **DEDO > AIP > ZALANDO**, and HOUSE rules may add constraints but never contradict the others. So:

- Every RPC returns its own `<Rpc>Response`; REST unwraps it with `response_body`. Do not "fix" this to AIP's bare-resource returns.
- The major version is in the package and the path (`/<root>/v1/...`). Path segments are lowerCamelCase, never kebab-case.
- JSON field names are the protojson default. Never set `json_name`.
- Errors are `google.rpc.Status` with `ErrorInfo`, not Problem JSON. Pagination is `page_token`/`next_page_token`.

[RULES.md](https://github.com/raveesh-me/dedo/blob/main/guidance/RULES.md#skipped-rules) lists every rule dedo skips on purpose, by source. Do not re-enable them.

## MCP tools

Every unary, non-deprecated RPC becomes an MCP tool named after it in snake_case (`GetTodo` becomes `get_todo`). `protoc-gen-dedo-mcp` generates them as `<Service>MCPTools()`, and `main.go` serves them with `mcpserver.AddTools(server, mcpserver.Target{Client: mcpserver.InProcessClient(api)}, <pkg>.<Service>MCPTools()...)`. Never write tool definitions by hand: change the proto and regenerate. Calls go through the same auth, scopes and validation as REST and Connect.

### What the model sees

- **Only `name`, `description` and `inputSchema` reach the model.** Hosts such as Claude Desktop and claude.ai turn MCP tools into model tool definitions with just those three fields. `title`, `annotations` (`readOnlyHint`, `destructiveHint`, `idempotentHint`, `openWorldHint`) and `outputSchema` reach the client application but not the model. That is by design: the MCP specification makes annotations hints for clients, untrusted when the server is.
- So **put everything the model must know in `method.tool.description`**: what the tool does, whether it is irreversible ("cannot be recovered"), and when to use it instead of a similar tool. dedo appends the declared error reasons to it.
- Still declare `method.tool.annotations` accurately. Clients use them, for example to ask the user before a destructive call, and `DEDO_TOOL_ANNOTATIONS` checks them against `idempotency_level`.
- Keep spec citations such as "AIP-134" out of comments on request fields and messages: those comments become schema descriptions the model reads.

### Where the input schema comes from

`protoc-gen-dedo-mcp` builds each tool's schemas at generation time:

| Declaration | In the input schema |
| --- | --- |
| Leading field comment | The field's `description`. Write it for a model. |
| `dedo.v1.message` description | The object's `description` |
| `google.api.field_behavior` | `REQUIRED` is required; `OUTPUT_ONLY` is left out. A create omits the resource name; an update with a `FieldMask` requires only the name and lists the accepted field names. |
| `google.api.resource`, `resource_reference` | A `pattern` for resource names, such as `^todos/[^/]+$` |
| `buf.validate` rules | `minimum`, `maximum`, `minLength`, `maxLength`, `pattern`, `enum`, `minItems`, `maxItems`, and `format` for email, uri and uuid |
| `google.type.Money` | The currency code pattern and the `nanos` range |

State a limit as a `buf.validate` rule, not only in prose, and add `connectrpc.com/validate`'s interceptor so the server enforces it on every transport. Rules the schema cannot express (CEL, such as Money's sign rule) are still enforced by the server; describe them in the field comment.

### Behaviour of the served tools

- Arguments and results use proto field names (`budget_amount`). Field masks take the same names; `mcpserver` converts them to the JSON mapping, and lowerCamelCase also works.
- Results write default values, so an empty list is `{"todos": [], "next_page_token": ""}`, never `{}`.
- 64-bit integers: inputs accept a JSON number or a decimal string; outputs are strings.
- A failed call, including a schema validation failure, is a `CallToolResult` with `isError: true` and a JSON body `{"code", "message", "reason"}`. JSON-RPC errors are only for protocol faults such as an unknown tool.
- Wrap the MCP handler in `http.NewCrossOriginProtection()`. The MCP transport requires servers to reject requests from other web origins (DNS rebinding); MCP clients send no `Origin` and pass.

### Checking a tool on the wire

Do not judge the server from what a model reports: it cannot see annotations, titles or output schemas. Check the raw response instead:

```sh
curl -s http://localhost:8080/mcp -H 'Authorization: Bearer <token>' \
  -H 'Content-Type: application/json' -H 'Accept: application/json, text/event-stream' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}'
```

The official suite, `@modelcontextprotocol/conformance server --url <url>`, mostly calls fixture tools (`test_simple_text` and the like) that only its reference server has; those failures do not apply to a dedo service. Its DNS-rebinding scenario needs a `localhost` URL. Run Node tools in a container.

### Connecting a local client

- Claude Code: `claude mcp add --transport http <name> http://localhost:<port>/mcp --header "Authorization: Bearer <token>"`
- Claude Desktop needs HTTPS with OAuth for custom connectors. For a local server, bridge it with `mcp-remote`, run in a container, with `--allow-http` (see [the todo example's MCP.md](https://github.com/raveesh-me/dedo/blob/main/examples/todo/MCP.md)). A static bearer token is for local development only; production MCP authorization is OAuth 2.1.
