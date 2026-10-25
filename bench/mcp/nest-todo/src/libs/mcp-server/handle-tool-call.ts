import { ErrorCode, McpError, type CallToolResult } from '@modelcontextprotocol/sdk/types.js';
import { z } from 'zod';
import type { ToolCallContext, ToolRegistration } from './tool-registration';
import { invalidArguments } from './tool-result';

/** The tools a server offers, by name. */
export type ToolRegistry = ReadonlyMap<string, ToolRegistration>;

export function createToolRegistry(...groups: readonly ToolRegistration[][]): ToolRegistry {
  const registry = new Map<string, ToolRegistration>();
  for (const tool of groups.flat()) {
    if (registry.has(tool.definition.name)) {
      throw new Error(`duplicate tool ${tool.definition.name}`);
    }
    registry.set(tool.definition.name, tool);
  }
  return registry;
}

/**
 * The central dispatch: finds the tool by name, validates its arguments with
 * its zod schema, and runs it. An unknown tool is a protocol error, as in
 * dedo; bad arguments are a tool error the model can read and fix.
 */
export async function handleToolCall(
  registry: ToolRegistry,
  name: string,
  args: unknown,
  ctx: ToolCallContext,
): Promise<CallToolResult> {
  const tool = registry.get(name);
  if (!tool) {
    throw new McpError(ErrorCode.InvalidParams, `unknown tool "${name}"`);
  }
  const parsed = tool.schema.safeParse(args ?? {});
  if (!parsed.success) {
    return invalidArguments(`the arguments do not match the input schema: ${z.prettifyError(parsed.error)}`);
  }
  return tool.handler(parsed.data, ctx);
}
