# nest-todo: the NestJS MCP baseline

An MCP server for the todo example, built the way HighLevel's engineering
docs say an MCP server should be built. It is the baseline that dedo's Go MCP
server (`examples/todo`, `/mcp`) is benchmarked against. It serves the same
tools as dedo (same names, titles, descriptions, annotations, input and output
schemas) and reaches the todo service over its REST API.

## What it mirrors from the HighLevel docs

| Doc page | What this server does |
| --- | --- |
| *MCP Server Architecture* | Three pieces: the Nest MCP app (`src/apps/api/src/mcp/`: routes, lifecycle, sessions, auth guard), the MCP library (`src/libs/mcp-server/`: `ToolRegistration`, `createTodoTools()` / `createContactsTools()` factories, the central `handleToolCall` that routes by tool name), and integrations. Streamable HTTP transport via `@modelcontextprotocol/sdk`. Sessions tracked in memory with a TTL. |
| *Adding Integrations* | HTTP proxy pattern: `integrations/todo.handler.ts` and `contacts.handler.ts` are pure functions with no NestJS dependencies that call the backend's REST API, reading `TODO_API_URL` / `CONTACTS_API_URL` at call time. Limits: 10 MB request body, 30 s request timeout, 300 requests / 60 s per IP, CORS allowlist. |
| *MCP Authentication* | `McpAuthGuard` on every MCP request: static bearer tokens compared in constant time (`crypto.timingSafeEqual`). Unauthenticated `GET /health` and `GET /mcp/health`. Sessions: `Mcp-Session-Id`, `MCP_SESSION_TTL_MS` (default 30 min), stale session gets 410 Gone on GET/DELETE, POST with a stale ID auto-creates a session, periodic cleanup. |
| *Connect to MCP* | First POST without `Mcp-Session-Id` creates the session; POST with an expired ID is recovered transparently. `mcp/health` reports `active_sessions`, `session_ttl_ms`, `tools_total`. "Server logs timing for every call": one log line per tool call. |
| *Environment Variables* | `API_PORT` (default 3100), `MCP_SESSION_TTL_MS`. |

Not mirrored: GitHub PAT auth, IP allowlist, loopback bypass, stdio and SSE
transports, `AGENT_VISIBLE` tool filtering, GCP Secret Manager. The
benchmark needs exactly the `reader` / `writer` tokens and nothing else, so
any other token, loopback included, gets 401.

## Request path

`POST /mcp` → `ThrottlerGuard` → `McpAuthGuard` → `McpController.post` →
`McpSessionService.forPost` (find, create or recover the session) →
SDK `StreamableHTTPServerTransport.handleRequest` → the session's MCP
`Server` → `CallToolRequestSchema` handler in `McpServerService` →
`handleToolCall` (lookup by name, zod validation) → the tool's handler →
`integrations/todo.handler.ts` → `proxy()` (`fetch` to the REST API with the
caller's `Authorization` and `X-Flow-ID`) → `toToolResult`.

The backend enforces scopes, flow IDs and audit logging, so the caller's
`Authorization` and `X-Flow-ID` headers are forwarded on every backend call.

## Tool results

Like dedo's tools: the response message as `structuredContent` (proto field
names, response fields such as `todo` wrapped back around what the REST
binding's `response_body` unwrapped), plus a text block of the same JSON.
A non-2xx response becomes `isError: true` with
`{"code":"PERMISSION_DENIED","message":"…","reason":"…"}`, taken from the
`google.rpc.Status` body and its `ErrorInfo`. `update_mask` is a
comma-separated list of snake_case field names; it is sent as the
`update_mask` query parameter in lowerCamelCase, as the REST binding wants.

## Environment

| Variable | Default | Meaning |
| --- | --- | --- |
| `API_PORT` | `3100` | HTTP port. |
| `TODO_API_URL` | `http://localhost:8080` | Todo service REST base URL. |
| `CONTACTS_API_URL` | `http://localhost:8081` | Contacts service REST base URL. |
| `MCP_TOOLSETS` | `todo` | Comma-separated toolsets to serve: `todo`, `contacts`. |
| `MCP_AUTH_TOKENS` | `reader,writer` | Accepted bearer tokens. |
| `MCP_SESSION_TTL_MS` | `1800000` | Session idle timeout. |
| `MCP_STATELESS` | `false` | `true`: no sessions; each POST gets a new MCP server and transport, as the MCP TypeScript SDK's stateless example does. GET and DELETE answer 405. The benchmark runs it this way, so it pays per call what a stateless dedo service pays. |
| `MCP_JSON_RESPONSE` | `true` | `false` answers POSTs as SSE streams (SDK default) instead of JSON, which is what dedo sends. |
| `RATE_LIMIT_ENABLED` | `true` | `false` turns off the 300 req / 60 s per-IP limit. **Set it to `false` for the benchmark**, which drives the server from one IP. |
| `CORS_ORIGINS` | none | Comma-separated allowed origins. |
| `LOG_TOOL_CALLS` | `true` | `false` turns off the per-call timing log line. |
| `MCP_SERVER_NAME` | `todo` | `serverInfo.name` in the initialize result. |

## Run

No Node on the host is needed; everything runs in containers.

```sh
# The todo backend, on the host
(cd ../../../examples/todo && go run .)

# Build and run the MCP server
echo '{}' > /tmp/no-auth.json   # empty registry credentials
podman build --authfile /tmp/no-auth.json -t localhost/nest-todo-mcp:dev .
podman run --rm -p 3100:3100 \
  -e TODO_API_URL=http://host.containers.internal:8080 \
  -e RATE_LIMIT_ENABLED=false \
  localhost/nest-todo-mcp:dev

# Initialize a session and list the tools
curl -si localhost:3100/mcp -H 'Authorization: Bearer writer' \
  -H 'Content-Type: application/json' -H 'Accept: application/json, text/event-stream' \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"curl","version":"1"}}}'
# then send tools/list and tools/call with the returned Mcp-Session-Id header
```

To regenerate `package-lock.json`:

```sh
podman run --rm -v "$PWD":/w -w /w docker.io/library/node:22-alpine npm install --package-lock-only
```

`src/libs/mcp-server/*.tool-definitions.ts` are copied from dedo:
`todo` and `contacts` from `tools/list` on the Go todo and contacts
servers. Refresh them when the protos change.
