import { Injectable, Logger, OnModuleDestroy } from '@nestjs/common';
import { randomUUID } from 'node:crypto';
import type { IncomingMessage, ServerResponse } from 'node:http';
import type { Server } from '@modelcontextprotocol/sdk/server/index.js';
import { StreamableHTTPServerTransport } from '@modelcontextprotocol/sdk/server/streamableHttp.js';
import { isInitializeRequest } from '@modelcontextprotocol/sdk/types.js';
import { McpServerService } from './mcp-server.service';

interface Session {
  readonly transport: StreamableHTTPServerTransport;
  readonly server: Server;
  lastSeen: number;
}

const DEFAULT_TTL_MS = 1_800_000;
const MAX_SWEEP_INTERVAL_MS = 60_000;

/**
 * Stateful Streamable HTTP sessions, in memory: one transport and one MCP
 * Server per Mcp-Session-Id, dropped after MCP_SESSION_TTL_MS of inactivity
 * by a periodic sweep.
 */
@Injectable()
export class McpSessionService implements OnModuleDestroy {
  private readonly logger = new Logger('McpSessions');
  private readonly sessions = new Map<string, Session>();
  readonly ttlMs = Number(process.env.MCP_SESSION_TTL_MS ?? DEFAULT_TTL_MS);
  private readonly sweeper: NodeJS.Timeout;

  constructor(private readonly mcpServer: McpServerService) {
    this.sweeper = setInterval(() => this.sweep(), Math.min(this.ttlMs, MAX_SWEEP_INTERVAL_MS));
    this.sweeper.unref();
  }

  get size(): number {
    return this.sessions.size;
  }

  /**
   * MCP_STATELESS=true: no sessions. Every POST gets a new MCP Server and a
   * transport without a session ID, closed when the response ends, as the
   * MCP TypeScript SDK's stateless example does. Each call then pays for its
   * own server, as a stateless dedo service does.
   */
  readonly stateless = process.env.MCP_STATELESS === 'true';

  /** Serves one POST in stateless mode. */
  async handleStateless(req: IncomingMessage, res: ServerResponse, body: unknown): Promise<void> {
    const server = this.mcpServer.createServer();
    const transport = new StreamableHTTPServerTransport({
      sessionIdGenerator: undefined,
      enableJsonResponse: process.env.MCP_JSON_RESPONSE !== 'false',
    });
    res.on('close', () => {
      void transport.close();
      void server.close();
    });
    await server.connect(transport);
    await transport.handleRequest(req, res, body);
  }

  /** The live session with this ID, or undefined if it is unknown or expired. */
  find(id: string | undefined): StreamableHTTPServerTransport | undefined {
    if (!id) return undefined;
    const session = this.sessions.get(id);
    if (!session) return undefined;
    const now = Date.now();
    if (now - session.lastSeen > this.ttlMs) {
      void this.close(id, session);
      return undefined;
    }
    session.lastSeen = now;
    return session.transport;
  }

  /**
   * The transport for a POST. No Mcp-Session-Id: a new session, which the
   * initialize request in the body sets up. An unknown or expired ID: a new
   * session too; if the body is not an initialize request, the new session
   * starts initialized under the ID the client sent, so the client carries
   * on without noticing.
   */
  async forPost(sessionId: string | undefined, body: unknown): Promise<StreamableHTTPServerTransport> {
    const existing = this.find(sessionId);
    if (existing) return existing;
    if (!sessionId || isInitializeRequest(body)) {
      return this.open();
    }
    return this.open(sessionId);
  }

  private async open(preInitializedId?: string): Promise<StreamableHTTPServerTransport> {
    const server = this.mcpServer.createServer();
    const transport: StreamableHTTPServerTransport = new StreamableHTTPServerTransport({
      sessionIdGenerator: randomUUID,
      enableJsonResponse: process.env.MCP_JSON_RESPONSE !== 'false',
      onsessioninitialized: (id): void => this.track(id, transport, server),
      onsessionclosed: (id) => {
        this.sessions.delete(id);
      },
    });
    await server.connect(transport);
    if (preInitializedId) {
      markInitialized(transport, preInitializedId);
      this.track(preInitializedId, transport, server);
    }
    return transport;
  }

  private track(id: string, transport: StreamableHTTPServerTransport, server: Server): void {
    this.sessions.set(id, { transport, server, lastSeen: Date.now() });
  }

  private sweep(): void {
    const cutoff = Date.now() - this.ttlMs;
    for (const [id, session] of this.sessions) {
      if (session.lastSeen < cutoff) void this.close(id, session);
    }
  }

  private async close(id: string, session: Session): Promise<void> {
    this.sessions.delete(id);
    try {
      await session.server.close();
    } catch (err: unknown) {
      this.logger.warn(`closing session ${id}: ${err instanceof Error ? err.message : String(err)}`);
    }
  }

  async onModuleDestroy(): Promise<void> {
    clearInterval(this.sweeper);
    await Promise.all([...this.sessions].map(([id, s]) => this.close(id, s)));
  }
}

/**
 * The SDK has no public way to start a session without an initialize
 * request, which auto-recovery needs; set the transport's session state
 * directly. Pinned to @modelcontextprotocol/sdk 1.32.x.
 */
function markInitialized(transport: StreamableHTTPServerTransport, id: string): void {
  const inner = (transport as unknown as { _webStandardTransport: { sessionId?: string; _initialized: boolean } })
    ._webStandardTransport;
  inner.sessionId = id;
  inner._initialized = true;
}
