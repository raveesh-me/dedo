import { Injectable, Logger } from '@nestjs/common';
import { Server } from '@modelcontextprotocol/sdk/server/index.js';
import { CallToolRequestSchema, ListToolsRequestSchema, type Tool } from '@modelcontextprotocol/sdk/types.js';
import {
  createContactsTools,
  createTodoTools,
  createToolRegistry,
  handleToolCall,
  type ToolCallContext,
  type ToolRegistration,
  type ToolRegistry,
} from '../../../../libs/mcp-server';
import { contacts, todo } from './integrations';

type Headers = Record<string, string | string[] | undefined>;

function header(headers: Headers | undefined, name: string): string | undefined {
  const v = headers?.[name];
  return Array.isArray(v) ? v[0] : v;
}

// Toolsets this server can offer, by the name MCP_TOOLSETS uses.
const TOOLSETS: Readonly<Record<string, () => ToolRegistration[]>> = {
  todo: () => createTodoTools(todo),
  contacts: () => createContactsTools(contacts),
};

/**
 * Builds the tool registry once, and an MCP Server per session that lists
 * it and dispatches every tools/call through handleToolCall.
 */
@Injectable()
export class McpServerService {
  private readonly logger = new Logger('McpTools');
  private readonly logCalls = process.env.LOG_TOOL_CALLS !== 'false';
  readonly registry: ToolRegistry;
  private readonly tools: Tool[];

  constructor() {
    const names = (process.env.MCP_TOOLSETS ?? 'todo').split(',').map((s) => s.trim()).filter(Boolean);
    this.registry = createToolRegistry(
      ...names.map((name) => {
        const create = TOOLSETS[name];
        if (!create) throw new Error(`MCP_TOOLSETS: unknown toolset "${name}"`);
        return create();
      }),
    );
    this.tools = [...this.registry.values()].map((t) => t.definition);
  }

  createServer(): Server {
    const server = new Server(
      { name: process.env.MCP_SERVER_NAME ?? 'todo', version: 'v1' },
      { capabilities: { logging: {}, tools: { listChanged: true } } },
    );
    server.setRequestHandler(ListToolsRequestSchema, () => ({ tools: this.tools }));
    server.setRequestHandler(CallToolRequestSchema, async (request, extra) => {
      const headers = extra.requestInfo?.headers as Headers | undefined;
      const ctx: ToolCallContext = {
        authorization: header(headers, 'authorization'),
        flowId: header(headers, 'x-flow-id'),
      };
      const started = performance.now();
      const result = await handleToolCall(this.registry, request.params.name, request.params.arguments, ctx);
      if (this.logCalls) {
        const ms = (performance.now() - started).toFixed(1);
        this.logger.log(`tool=${request.params.name} ms=${ms} isError=${result.isError === true}`);
      }
      return result;
    });
    return server;
  }
}
