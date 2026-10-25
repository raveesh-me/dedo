import { CanActivate, ExecutionContext, Injectable, UnauthorizedException } from '@nestjs/common';
import { createHash, timingSafeEqual } from 'node:crypto';
import type { Request } from 'express';

function digest(value: string): Buffer {
  return createHash('sha256').update(value).digest();
}

/**
 * McpAuthGuard: every MCP request must carry Authorization: Bearer <token>
 * with one of the static tokens in MCP_AUTH_TOKENS (default reader,writer).
 * Tokens are compared in constant time: both sides are hashed to equal
 * length for crypto.timingSafeEqual, and every configured token is checked.
 */
@Injectable()
export class McpAuthGuard implements CanActivate {
  private readonly tokens: readonly Buffer[] = (process.env.MCP_AUTH_TOKENS ?? 'reader,writer')
    .split(',')
    .map((t) => t.trim())
    .filter((t) => t !== '')
    .map(digest);

  canActivate(context: ExecutionContext): boolean {
    const header = context.switchToHttp().getRequest<Request>().headers.authorization;
    if (!header?.startsWith('Bearer ')) {
      throw new UnauthorizedException('Invalid or missing MCP authentication');
    }
    const presented = digest(header.slice('Bearer '.length));
    let ok = false;
    for (const token of this.tokens) {
      ok = timingSafeEqual(presented, token) || ok;
    }
    if (!ok) {
      throw new UnauthorizedException('Invalid or missing MCP authentication');
    }
    return true;
  }
}
