import { z } from 'zod';
import { TODO_TOOL_DEFINITIONS as defs } from './todo.tool-definitions';
import { defineTool, type ToolCallContext, type ToolRegistration } from './tool-registration';
import { toToolResult, type IntegrationResult } from './tool-result';

// zod schemas matching the JSON Schemas in todo.tool-definitions.ts. Unknown
// keys are stripped, as dedo discards unknown fields.
const todoName = z.string().regex(/^todos\/[^/]+$/);

const money = z.object({
  currency_code: z.string().regex(/^[A-Z]{3}$/).optional(),
  nanos: z.number().int().min(-999_999_999).max(999_999_999).optional(),
  units: z.union([z.string(), z.number().int()]).optional(),
});

const todoFields = {
  title: z.string().optional(),
  done: z.boolean().optional(),
  budget_amount: money.optional(),
  assignee_email: z.union([z.literal(''), z.email()]).optional(),
};

export const getTodoArgs = z.object({ name: todoName });
export const listTodosArgs = z.object({
  page_size: z.number().int().min(0).optional(),
  page_token: z.string().optional(),
});
export const createTodoArgs = z.object({
  todo: z.object({ ...todoFields, title: z.string() }),
  todo_id: z.string().optional(),
});
export const updateTodoArgs = z.object({
  todo: z.object({ ...todoFields, name: todoName }),
  update_mask: z.string().optional(),
});
export const deleteTodoArgs = z.object({ name: todoName });

type Handler<S extends z.ZodType> = (args: z.infer<S>, ctx: ToolCallContext) => Promise<IntegrationResult>;

/** The todo integration's functions, one per tool. */
export interface TodoHandlers {
  getTodo: Handler<typeof getTodoArgs>;
  listTodos: Handler<typeof listTodosArgs>;
  createTodo: Handler<typeof createTodoArgs>;
  updateTodo: Handler<typeof updateTodoArgs>;
  deleteTodo: Handler<typeof deleteTodoArgs>;
}

/** The todo tools, each backed by one function of the todo integration. */
export function createTodoTools(todo: TodoHandlers): ToolRegistration[] {
  return [
    defineTool({
      definition: defs.get_todo,
      schema: getTodoArgs,
      handler: async (args, ctx) => toToolResult(await todo.getTodo(args, ctx)),
    }),
    defineTool({
      definition: defs.list_todos,
      schema: listTodosArgs,
      handler: async (args, ctx) => toToolResult(await todo.listTodos(args, ctx)),
    }),
    defineTool({
      definition: defs.create_todo,
      schema: createTodoArgs,
      handler: async (args, ctx) => toToolResult(await todo.createTodo(args, ctx)),
    }),
    defineTool({
      definition: defs.update_todo,
      schema: updateTodoArgs,
      handler: async (args, ctx) => toToolResult(await todo.updateTodo(args, ctx)),
    }),
    defineTool({
      definition: defs.delete_todo,
      schema: deleteTodoArgs,
      handler: async (args, ctx) => toToolResult(await todo.deleteTodo(args, ctx)),
    }),
  ];
}
