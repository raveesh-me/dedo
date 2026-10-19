# Use the todo service from Claude

The todo service is also an MCP server. Every RPC is a tool, named after it (`list_todos`, `create_todo`, `update_todo`, `delete_todo`, and so on), with the title, description and MCP `ToolAnnotations` hints declared in the `.proto`. The model sees the description; hints such as `destructiveHint` are for the client, which may not pass them on. Tool calls go through the same auth and scope checks as REST and RPC calls. A change an agent makes appears live in the web UI.

Start the service first:

```sh
cd examples/todo
make up            # kind cluster; UI at http://localhost:8090
```

The MCP endpoint is `http://localhost:8090/mcp`. It expects a bearer token:

| Token | Scopes | The agent can |
| --- | --- | --- |
| `reader` | `todo.todo.read` | list and get to-dos |
| `writer` | `todo.todo.read`, `todo.todo.write` | also create, update and delete |

## Claude Code

```sh
claude mcp add --transport http todo http://localhost:8090/mcp \
  --header "Authorization: Bearer writer"
```

Check it with `claude mcp list`, or run `/mcp` inside Claude Code. Then ask, for example:

> Add a to-do "Review the API guideline", then list my to-dos.

Keep the web UI open at <http://localhost:8090> while Claude works: each to-do it adds appears there without a reload.

Try the same with the `reader` token, and Claude gets `PERMISSION_DENIED` when it tries to write. The scope check comes from the `.proto`, not from MCP code.

To remove it: `claude mcp remove todo`.

## Claude Desktop

Claude Desktop's custom connectors need a public HTTPS URL with OAuth. For a local server with a bearer token, bridge it over stdio with [`mcp-remote`](https://www.npmjs.com/package/mcp-remote), run in a container so Node isn't needed on the host.

Add this to `claude_desktop_config.json` (Settings → Developer → Edit Config), then restart Claude Desktop:

```json
{
  "mcpServers": {
    "todo": {
      "command": "podman",
      "args": [
        "run", "-i", "--rm", "docker.io/library/node:24-alpine",
        "npx", "-y", "mcp-remote",
        "http://host.containers.internal:8090/mcp",
        "--header", "Authorization: Bearer writer",
        "--allow-http"
      ]
    }
  }
}
```

`host.containers.internal` is how a podman container reaches your machine's `localhost`. `mcp-remote` accepts plain HTTP only for `localhost`, so `--allow-http` is needed for this name; without it Claude Desktop shows "Server disconnected". With Docker, use `docker` and `host.docker.internal` instead.

If the server shows as failed, Claude Desktop's log for it is `~/Library/Logs/Claude/mcp-server-todo.log`.

## Without Claude: raw JSON-RPC

The server runs stateless with JSON responses, so a single request works:

```sh
# List the tools
curl -s http://localhost:8090/mcp \
  -H 'Authorization: Bearer reader' -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}'

# Add a to-do as an agent would
curl -s http://localhost:8090/mcp \
  -H 'Authorization: Bearer writer' -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"create_todo","arguments":{"todo":{"title":"From curl"}}}}'
```

## How it is tested

`make e2e` runs the same flow with no human in the loop. A browser opens the UI, an agent adds a to-do through `/mcp` with the `writer` token, and the test checks that the UI shows it without a reload, then that it disappears when the agent deletes it. See `e2e/tests/realtime.spec.ts`. `app/watch_test.go` checks the same path in Go: an MCP call reaches an open `WatchTodos` stream.
