import type { APIRequestContext } from '@playwright/test'

let id = 0

// callTool calls an MCP tool the way an agent does: a JSON-RPC tools/call
// to the service's /mcp endpoint with the agent's bearer token.
export async function callTool(
  request: APIRequestContext,
  token: string,
  name: string,
  args: Record<string, unknown>,
): Promise<{ isError?: boolean; structuredContent?: unknown; content?: { text?: string }[] }> {
  const res = await request.post('/mcp', {
    headers: {
      Authorization: `Bearer ${token}`,
      Accept: 'application/json, text/event-stream',
    },
    data: { jsonrpc: '2.0', id: ++id, method: 'tools/call', params: { name, arguments: args } },
  })
  if (!res.ok()) {
    throw new Error(`MCP ${name}: HTTP ${res.status()} ${await res.text()}`)
  }
  const body = await res.json()
  if (body.error) {
    throw new Error(`MCP ${name}: ${JSON.stringify(body.error)}`)
  }
  return body.result
}

export const unique = (prefix: string) => `${prefix} ${Date.now()}-${Math.floor(Math.random() * 1e6)}`
