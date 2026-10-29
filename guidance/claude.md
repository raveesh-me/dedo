# Building an API with dedo: guidance for coding agents

Append this file to your repository's `AGENTS.md` or `CLAUDE.md`; `dedo init` does it for you. dedo's full guidance: [install.md](https://github.com/raveesh-me/dedo/blob/main/guidance/install.md) sets a service up, [tutorial.md](https://github.com/raveesh-me/dedo/blob/main/guidance/tutorial.md) builds one from scratch, [guideline.md](https://github.com/raveesh-me/dedo/blob/main/guidance/guideline.md) explains the reasoning, and [RULES.md](https://github.com/raveesh-me/dedo/blob/main/guidance/RULES.md) documents every lint rule. It tells a coding agent how a service built with [dedo](https://github.com/raveesh-me/dedo) works and which mistakes to avoid.

## The proto is the source of truth

- One `.proto` defines the API. dedo serves it as Connect/gRPC, REST (`google.api.http`, through Vanguard), an OpenAPI 3.1 reference and MCP tools. Change the proto, never the generated output.
- Never edit anything under `gen/` or `docs/openapi.json`. Run `make generate` and commit the result; CI fails on stale output. Use the `make` targets (`run`, `generate`, `check`, `test`, `tools`) and `dedo` rather than calling `buf` or `go` directly; where the repository's own `Makefile` already had one of those names, dedo's is `api-<name>`.
- Set dedo up with `dedo init` and upgrade it with `dedo update && dedo upgrade`, never by hand-editing pins: the options module's label in `buf.yaml` and the lint plugin, generators and runtime in `go.mod` are one release and move together. After a fresh clone, run `dedo tools` to build the lint plugin into `.tools/` (in a repository without `go.mod`, it copies the one `dedo install` put in `$GOBIN`, at `buf.yaml`'s release); it also fetches the house rules and checks their overrides. `dedo upgrade -lint` moves only the lint plugin to a new release. Leave the `replace` of `github.com/modelcontextprotocol/go-sdk` in `go.mod` alone: dedo maintains it (a build of the SDK with a fix not yet released upstream) and removes it when upstream has the fix.
- Before you finish a change to `proto/`, run `make check` (format, lint, breaking). Fix findings in the proto. Suppress one only with `// buf:lint:ignore <RULE_ID>` and a written reason.
- Scopes, cache policy, deprecation and error reasons are declared in the proto (`dedo.v1` options) and enforced by the runtime. Do not write scope checks, cache headers or deprecation headers in handlers.
- Return errors with a declared reason in a `google.rpc.ErrorInfo` detail. Undeclared reasons are logged as audit findings. The service uses connect-go v2: handlers take and return plain messages, errors are `connect.NewError(code, message)`, and any other error reaches callers as `UNKNOWN` with no message.
- A server stream that clients wait on (a watch, a feed) sends a first message saying it is open, such as a `CHANGE_WATCHING` value, rather than relying on headers: buffering proxies hold headers back, and connect-go v2.0.1's HTTP transport sends none until the first message (fixed upstream after v2.0.1).
- In a multi-tenant service (`tenancy` in `dedo.v1.service`), every resource name starts with the tenant (`locations/1/contacts/2`) and every request names it in the field its HTTP path binds. The runtime rejects a request before the handler runs if that field, or any other field with a `google.api.resource_reference`, names a tenant the caller may not act in. Give every field that holds a resource name a `resource_reference`: one without is not checked. Handlers still scope their storage queries to the tenant. Never mark an RPC `cross_tenant` to get past the check: that is only for RPCs that genuinely span tenants, such as listing the caller's own.
- Put cross-cutting checks (auth, logging) in `connect.ServerInterceptor`s on the one `connect.Server`, not in HTTP middleware: MCP tool calls reach the server in-process and skip HTTP middleware.

## Lint classes and precedence

`buf-plugin-dedo-lint` reports these classes of rules:

| Prefix | Source |
| --- | --- |
| `AIP_*` | Google's AIPs, through api-linter |
| `ZALANDO_*` | The Zalando RESTful API guidelines dedo adopts |
| `DEDO_*` | dedo's own conventions |
| `HOUSE_*` | Your organisation's own rules, as CEL checks in `rules.yml` (and the shared files it imports) |
| `DEDO_MULTI_TENANT_*` | dedo's opt-in rules for multi-tenant services, the `DEDO_MULTI_TENANT` set |

`rules.yml` chooses which rules run: `use` names the rule sets (`AIP_NOLANG`, `ZALANDO`, `DEDO`, `HOUSE`, and `DEDO_MULTI_TENANT` for multi-tenant services), `disable` turns rules off by ID, and its `imports` and `rules`, the house rules, build on those sets and override them where they say so. `buf.yaml` lists only `DEDO`, which holds every rule of the plugin; never name another of its rules there, in `use` or `except`.

Rules rank in layers: **the repository's own `rules.yml` > the house rules it imports > DEDO > AIP > ZALANDO**. Nothing resolves a conflict automatically: a rule that replaces others names them in its `overrides`, and only rules of a lower layer, never a final one: dedo's `ZALANDO_SERVICE_RULES_REQUIRED`, `DEDO_METHOD_RULES_REQUIRED` and `DEDO_MULTI_TENANT_REQUEST_TENANT`, and any house rule marked `final: true`, which cannot be disabled either. Run `dedo tools` after every change to `rules.yml` or to an import: it fetches changed imports, checks `use`, `disable`, every override and final rule, and lists what runs and the overrides in force. Read that list before assuming a rule runs. Without a house override, dedo's conventions hold:

- Every RPC returns its own `<Rpc>Response`; REST unwraps it with `response_body`. Do not "fix" this to AIP's bare-resource returns.
- The major version is in the package and the path (`/<root>/v1/...`). Path segments are lowerCamelCase, never kebab-case.
- JSON field names are the protojson default. Never set `json_name`.
- Errors are `google.rpc.Status` with `ErrorInfo`, not Problem JSON. Pagination is `page_token`/`next_page_token`.

[RULES.md](https://github.com/raveesh-me/dedo/blob/main/guidance/RULES.md#skipped-rules) lists every rule dedo skips on purpose, by source. Do not re-enable them.

## MCP tools

Every unary, non-deprecated RPC becomes an MCP tool named after it in snake_case (`GetTodo` becomes `get_todo`). `protoc-gen-dedo-mcp` generates them as `<Service>MCPTools()`, and `main.go` serves them with `mcpserver.AddTools(tools, connect.NewClient(connectinprocess.New(server)), <pkg>.<Service>MCPTools()...)`: calls go in-process to the service's `connect.Server`. Never write tool definitions by hand: change the proto and regenerate. Calls go through the same auth, scopes and validation as REST and Connect.

- Who may reach `/mcp` is configuration, not code: `MCP_ACCESS=public` (the default; anyone may list the tools, every call is still scope-checked) or `authenticated` (requests without a token the service accepts get 401, tool discovery included). Keep `mcpserver.Guard` wired with the service's real token check.
- A Connect client to another service over HTTP needs its own `http.Client` with `Transport: &http.Transport{MaxIdleConnsPerHost: <your concurrency>}`, reused for every call. Never `http.DefaultClient`: it keeps two idle connections per host, and under load the service runs out of local ports (`cannot assign requested address`).

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
