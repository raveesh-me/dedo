import type { CallToolResult } from '@modelcontextprotocol/sdk/types.js';

/** A failed call as the model sees it, in the same shape as dedo's ToolError. */
export interface ToolErrorBody {
  readonly code: string;
  readonly message: string;
  readonly reason?: string;
}

/** The outcome of an integration call. */
export type IntegrationResult =
  | { readonly ok: true; readonly data: Record<string, unknown> }
  | { readonly ok: false; readonly error: ToolErrorBody };

/**
 * Turns an integration result into a tool result the way dedo does: the
 * JSON as structuredContent plus a text block of the same JSON, or, on
 * failure, isError with the error JSON as text.
 */
export function toToolResult(result: IntegrationResult): CallToolResult {
  if (!result.ok) {
    return { content: [{ type: 'text', text: JSON.stringify(result.error) }], isError: true };
  }
  return {
    content: [{ type: 'text', text: JSON.stringify(result.data) }],
    structuredContent: result.data,
  };
}

/** A tool error for arguments that fail validation. */
export function invalidArguments(message: string): CallToolResult {
  return toToolResult({ ok: false, error: { code: 'INVALID_ARGUMENT', message } });
}
