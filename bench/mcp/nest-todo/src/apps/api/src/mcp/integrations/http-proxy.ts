// The HTTP proxy pattern: a tool is one call to the backend's REST API.
// Pure functions, no NestJS. Uses Node's built-in fetch (undici), which keeps
// connections alive by default.
import type { IntegrationResult, ToolCallContext, ToolErrorBody } from '../../../../../libs/mcp-server';

export interface ProxyRequest {
  readonly baseUrl: string;
  readonly method: 'GET' | 'POST' | 'PATCH' | 'DELETE';
  readonly path: string;
  readonly query?: Readonly<Record<string, string | number | undefined>>;
  readonly body?: unknown;
  /** The response field the REST binding unwrapped (response_body), re-wrapped for the tool result. */
  readonly wrapIn?: string;
}

// google.rpc.Code numbers, as the REST error body carries them, by name.
const CODE_NAMES = [
  'OK', 'CANCELLED', 'UNKNOWN', 'INVALID_ARGUMENT', 'DEADLINE_EXCEEDED', 'NOT_FOUND',
  'ALREADY_EXISTS', 'PERMISSION_DENIED', 'RESOURCE_EXHAUSTED', 'FAILED_PRECONDITION',
  'ABORTED', 'OUT_OF_RANGE', 'UNIMPLEMENTED', 'INTERNAL', 'UNAVAILABLE', 'DATA_LOSS',
  'UNAUTHENTICATED',
] as const;

const HTTP_CODES: Readonly<Record<number, string>> = {
  400: 'INVALID_ARGUMENT', 401: 'UNAUTHENTICATED', 403: 'PERMISSION_DENIED', 404: 'NOT_FOUND',
  409: 'ALREADY_EXISTS', 429: 'RESOURCE_EXHAUSTED', 501: 'UNIMPLEMENTED', 503: 'UNAVAILABLE',
  504: 'DEADLINE_EXCEEDED',
};

const ERROR_INFO_TYPE = 'type.googleapis.com/google.rpc.ErrorInfo';

/** Puts a resource name such as todos/1 into a URL path, segment by segment. */
export function namePath(name: string): string {
  return name.split('/').map(encodeURIComponent).join('/');
}

/** Converts a field mask's snake_case paths to the lowerCamelCase the REST query parameter takes. */
export function maskParam(mask: string | undefined): string | undefined {
  if (mask === undefined) return undefined;
  return mask
    .split(',')
    .map((path) => path.trim().replace(/_([a-z0-9])/g, (_, c: string) => c.toUpperCase()))
    .filter((path) => path !== '')
    .join(',');
}

export async function proxy(req: ProxyRequest, ctx: ToolCallContext): Promise<IntegrationResult> {
  const url = new URL(req.path, req.baseUrl);
  for (const [key, value] of Object.entries(req.query ?? {})) {
    if (value !== undefined && value !== '') url.searchParams.set(key, String(value));
  }
  const headers: Record<string, string> = { Accept: 'application/json' };
  if (ctx.authorization) headers.Authorization = ctx.authorization;
  if (ctx.flowId) headers['X-Flow-ID'] = ctx.flowId;
  if (req.body !== undefined) headers['Content-Type'] = 'application/json';

  let res: Response;
  try {
    res = await fetch(url, {
      method: req.method,
      headers,
      body: req.body === undefined ? undefined : JSON.stringify(req.body),
    });
  } catch (err: unknown) {
    return { ok: false, error: { code: 'UNAVAILABLE', message: errorMessage(err) } };
  }
  const text = await res.text();
  const json = parseJson(text);
  if (!res.ok) {
    return { ok: false, error: toErrorBody(res.status, json, text) };
  }
  const data = protoNames(json ?? {}) as Record<string, unknown>;
  return { ok: true, data: req.wrapIn ? { [req.wrapIn]: data } : data };
}

function parseJson(text: string): unknown {
  if (text === '') return undefined;
  try {
    return JSON.parse(text) as unknown;
  } catch {
    return undefined;
  }
}

/**
 * The REST API answers in proto3 JSON's lowerCamelCase with null for unset
 * messages; dedo's tools answer with proto field names and leave unset
 * messages out. Converting keeps structuredContent true to the output schema.
 * (The todo and contacts messages have no map fields, whose keys would be data.)
 */
function protoNames(value: unknown): unknown {
  if (Array.isArray(value)) return value.map(protoNames);
  if (value === null || typeof value !== 'object') return value;
  const out: Record<string, unknown> = {};
  for (const [key, v] of Object.entries(value)) {
    if (v === null) continue;
    out[key.replace(/[A-Z]/g, (c) => `_${c.toLowerCase()}`)] = protoNames(v);
  }
  return out;
}

/** Maps a google.rpc.Status error body to dedo's tool error: code name, message, ErrorInfo reason. */
function toErrorBody(status: number, json: unknown, text: string): ToolErrorBody {
  if (json && typeof json === 'object' && 'message' in json) {
    const s = json as { code?: unknown; message?: unknown; details?: unknown };
    const code = typeof s.code === 'number' ? CODE_NAMES[s.code] : undefined;
    const details = Array.isArray(s.details) ? (s.details as Array<Record<string, unknown>>) : [];
    const info = details.find((d) => d?.['@type'] === ERROR_INFO_TYPE);
    const reason = typeof info?.reason === 'string' && info.reason !== '' ? info.reason : undefined;
    return {
      code: code ?? HTTP_CODES[status] ?? 'UNKNOWN',
      message: String(s.message),
      ...(reason ? { reason } : {}),
    };
  }
  return { code: HTTP_CODES[status] ?? (status >= 500 ? 'INTERNAL' : 'UNKNOWN'), message: text || `HTTP ${status}` };
}

function errorMessage(err: unknown): string {
  if (err instanceof Error) {
    const cause = err.cause instanceof Error ? `: ${err.cause.message}` : '';
    return `${err.message}${cause}`;
  }
  return 'backend request failed';
}
