import { z } from 'zod';
import { CONTACTS_TOOL_DEFINITIONS as defs } from './contacts.tool-definitions';
import { defineTool, type ToolCallContext, type ToolRegistration } from './tool-registration';
import { toToolResult, type IntegrationResult } from './tool-result';

// zod schemas matching the JSON Schemas in contacts.tool-definitions.ts.
const contactName = z.string().regex(/^locations\/[^/]+\/contacts\/[^/]+$/);

export const getContactArgs = z.object({ name: contactName });
export const updateContactArgs = z.object({
  contact: z.object({
    name: contactName,
    display_name: z.string().optional(),
    email: z.string().optional(),
    home_location: z.string().regex(/^locations\/[^/]+$/).optional(),
  }),
  update_mask: z.string().optional(),
});

type Handler<S extends z.ZodType> = (args: z.infer<S>, ctx: ToolCallContext) => Promise<IntegrationResult>;

/** The contacts integration's functions, one per tool. */
export interface ContactsHandlers {
  getContact: Handler<typeof getContactArgs>;
  updateContact: Handler<typeof updateContactArgs>;
}

/** The contacts tools, each backed by one function of the contacts integration. */
export function createContactsTools(contacts: ContactsHandlers): ToolRegistration[] {
  return [
    defineTool({
      definition: defs.get_contact,
      schema: getContactArgs,
      handler: async (args, ctx) => toToolResult(await contacts.getContact(args, ctx)),
    }),
    defineTool({
      definition: defs.update_contact,
      schema: updateContactArgs,
      handler: async (args, ctx) => toToolResult(await contacts.updateContact(args, ctx)),
    }),
  ];
}
