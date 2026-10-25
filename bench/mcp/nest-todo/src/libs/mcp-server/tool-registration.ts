import type { CallToolResult, Tool } from '@modelcontextprotocol/sdk/types.js';
import type { z } from 'zod';

/** What tools/list shows for a tool. */
export type ToolDefinition = Tool;

/** Per-request values a tool forwards to its backend. */
export interface ToolCallContext {
  /** The caller's Authorization header, forwarded as is. */
  readonly authorization?: string;
  /** The caller's X-Flow-ID header, if any. */
  readonly flowId?: string;
}

/**
 * A tool: its listing, the zod schema its arguments must match, and the
 * handler that runs it with the validated arguments.
 */
export interface ToolRegistration<S extends z.ZodType = z.ZodType> {
  readonly definition: ToolDefinition;
  readonly schema: S;
  handler(args: z.infer<S>, ctx: ToolCallContext): Promise<CallToolResult>;
}

/** Ties a schema to its handler's argument type. */
export function defineTool<S extends z.ZodType>(registration: ToolRegistration<S>): ToolRegistration {
  return registration as ToolRegistration;
}
