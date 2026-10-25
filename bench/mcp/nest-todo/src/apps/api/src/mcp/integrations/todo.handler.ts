// Todo integration: HTTP proxy to the todo service's REST API (the
// google.api.http bindings in examples/todo/proto/raveesh_me/todo/v1/todo.proto).
// Pure functions, no NestJS; TODO_API_URL is read at call time.
import type { IntegrationResult, ToolCallContext } from '../../../../../libs/mcp-server';
import { maskParam, namePath, proxy } from './http-proxy';

const PREFIX = '/commerce/todo/v1';

interface Money {
  readonly currency_code?: string;
  readonly units?: string | number;
  readonly nanos?: number;
}

export interface TodoInput {
  readonly name?: string;
  readonly title?: string;
  readonly done?: boolean;
  readonly budget_amount?: Money;
  readonly assignee_email?: string;
}

function baseUrl(): string {
  return process.env.TODO_API_URL ?? 'http://localhost:8080';
}

/** GET /commerce/todo/v1/{name=todos/*}, response_body: todo */
export function getTodo(args: { name: string }, ctx: ToolCallContext): Promise<IntegrationResult> {
  return proxy({ baseUrl: baseUrl(), method: 'GET', path: `${PREFIX}/${namePath(args.name)}`, wrapIn: 'todo' }, ctx);
}

/** GET /commerce/todo/v1/todos */
export function listTodos(
  args: { page_size?: number; page_token?: string },
  ctx: ToolCallContext,
): Promise<IntegrationResult> {
  return proxy(
    {
      baseUrl: baseUrl(),
      method: 'GET',
      path: `${PREFIX}/todos`,
      query: { page_size: args.page_size, page_token: args.page_token },
    },
    ctx,
  );
}

/** POST /commerce/todo/v1/todos, body: todo, response_body: todo */
export function createTodo(
  args: { todo: TodoInput; todo_id?: string },
  ctx: ToolCallContext,
): Promise<IntegrationResult> {
  return proxy(
    {
      baseUrl: baseUrl(),
      method: 'POST',
      path: `${PREFIX}/todos`,
      query: { todo_id: args.todo_id },
      body: args.todo,
      wrapIn: 'todo',
    },
    ctx,
  );
}

/** PATCH /commerce/todo/v1/{todo.name=todos/*}, body: todo, update_mask as a query parameter. */
export function updateTodo(
  args: { todo: TodoInput & { name: string }; update_mask?: string },
  ctx: ToolCallContext,
): Promise<IntegrationResult> {
  return proxy(
    {
      baseUrl: baseUrl(),
      method: 'PATCH',
      path: `${PREFIX}/${namePath(args.todo.name)}`,
      query: { update_mask: maskParam(args.update_mask) },
      body: args.todo,
      wrapIn: 'todo',
    },
    ctx,
  );
}

/** DELETE /commerce/todo/v1/{name=todos/*} */
export function deleteTodo(args: { name: string }, ctx: ToolCallContext): Promise<IntegrationResult> {
  return proxy({ baseUrl: baseUrl(), method: 'DELETE', path: `${PREFIX}/${namePath(args.name)}` }, ctx);
}
