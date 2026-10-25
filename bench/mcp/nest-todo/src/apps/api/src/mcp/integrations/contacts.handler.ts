// Contacts integration: HTTP proxy to the contacts service's REST API (the
// google.api.http bindings in examples/contacts/proto/raveesh_me/contacts/v1/contacts.proto).
// Pure functions, no NestJS; CONTACTS_API_URL is read at call time.
import type { IntegrationResult, ToolCallContext } from '../../../../../libs/mcp-server';
import { maskParam, namePath, proxy } from './http-proxy';

const PREFIX = '/commerce/contacts/v1';

export interface ContactInput {
  readonly name: string;
  readonly display_name?: string;
  readonly email?: string;
  readonly home_location?: string;
}

function baseUrl(): string {
  return process.env.CONTACTS_API_URL ?? 'http://localhost:8081';
}

/** GET /commerce/contacts/v1/{name=locations/*\/contacts/*}, response_body: contact */
export function getContact(args: { name: string }, ctx: ToolCallContext): Promise<IntegrationResult> {
  return proxy(
    { baseUrl: baseUrl(), method: 'GET', path: `${PREFIX}/${namePath(args.name)}`, wrapIn: 'contact' },
    ctx,
  );
}

/** PATCH /commerce/contacts/v1/{contact.name=locations/*\/contacts/*}, body: contact, update_mask as a query parameter. */
export function updateContact(
  args: { contact: ContactInput; update_mask?: string },
  ctx: ToolCallContext,
): Promise<IntegrationResult> {
  return proxy(
    {
      baseUrl: baseUrl(),
      method: 'PATCH',
      path: `${PREFIX}/${namePath(args.contact.name)}`,
      query: { update_mask: maskParam(args.update_mask) },
      body: args.contact,
      wrapIn: 'contact',
    },
    ctx,
  );
}
