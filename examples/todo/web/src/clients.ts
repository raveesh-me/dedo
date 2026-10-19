import { createConnectTransport } from '@connectrpc/connect-web'
import type { Interceptor } from '@connectrpc/connect'
import { ref } from 'vue'
import { TodoService } from './gen/raveesh_me/todo/v1/todo_pb'
import { createClientWithQuery } from './createClientWithQuery'

// The demo service accepts two bearer tokens: "reader" may read, "writer"
// may also write. A real app gets a token from its identity platform.
export const token = ref<'reader' | 'writer'>('writer')

const auth: Interceptor = (next) => (req) => {
  req.header.set('Authorization', `Bearer ${token.value}`)
  return next(req)
}

// The API is on the same origin: nginx in the cluster, Vite's proxy in
// development.
const transport = createConnectTransport({
  baseUrl: window.location.origin,
  interceptors: [auth],
})

export const todos = createClientWithQuery(TodoService, transport)
