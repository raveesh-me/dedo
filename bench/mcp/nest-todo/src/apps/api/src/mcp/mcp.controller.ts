import { Body, Controller, Delete, Get, HttpStatus, Post, Req, Res, UseGuards } from '@nestjs/common';
import { SkipThrottle } from '@nestjs/throttler';
import type { Request, Response } from 'express';
import { McpAuthGuard } from './mcp-auth.guard';
import { McpServerService } from './mcp-server.service';
import { McpSessionService } from './mcp-session.service';

/** MCP Streamable HTTP: POST, GET and DELETE on /mcp. */
@Controller('mcp')
@UseGuards(McpAuthGuard)
export class McpController {
  constructor(private readonly sessions: McpSessionService) {}

  @Post()
  async post(@Req() req: Request, @Res() res: Response, @Body() body: unknown): Promise<void> {
    const id = req.headers['mcp-session-id'];
    const transport = await this.sessions.forPost(typeof id === 'string' ? id : undefined, body);
    await transport.handleRequest(req, res, body);
  }

  @Get()
  async get(@Req() req: Request, @Res() res: Response): Promise<void> {
    await this.existing(req, res);
  }

  @Delete()
  async delete(@Req() req: Request, @Res() res: Response): Promise<void> {
    await this.existing(req, res);
  }

  /** GET and DELETE need a live session: 400 without an ID, 410 Gone for an expired one. */
  private async existing(req: Request, res: Response): Promise<void> {
    const id = req.headers['mcp-session-id'];
    if (typeof id !== 'string' || id === '') {
      res.status(HttpStatus.BAD_REQUEST).json(rpcError(-32000, 'Bad Request: Mcp-Session-Id header is required'));
      return;
    }
    const transport = this.sessions.find(id);
    if (!transport) {
      res.status(HttpStatus.GONE).json(rpcError(-32001, 'Session expired; send a new POST to start a session'));
      return;
    }
    await transport.handleRequest(req, res);
  }
}

/** Unauthenticated health checks, for Kubernetes probes. */
@Controller()
@SkipThrottle()
export class HealthController {
  constructor(
    private readonly sessions: McpSessionService,
    private readonly mcpServer: McpServerService,
  ) {}

  @Get('health')
  health(): { status: string } {
    return { status: 'ok' };
  }

  @Get('mcp/health')
  mcpHealth(): Record<string, unknown> {
    return {
      status: 'ok',
      active_sessions: this.sessions.size,
      session_ttl_ms: this.sessions.ttlMs,
      tools_total: this.mcpServer.registry.size,
      timestamp: new Date().toISOString(),
    };
  }
}

function rpcError(code: number, message: string): Record<string, unknown> {
  return { jsonrpc: '2.0', error: { code, message }, id: null };
}
