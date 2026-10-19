import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

// In development the UI calls the API on the same origin, like it does
// behind nginx in the cluster. API_URL is where the todo service listens.
const api = process.env.API_URL ?? 'http://host.containers.internal:8080'

export default defineConfig({
  plugins: [vue()],
  server: {
    proxy: {
      '/raveesh_me.todo.v1.TodoService': api,
      '/commerce': api,
      '/mcp': api,
      '/openapi.json': api,
      '/docs': api,
    },
  },
})
