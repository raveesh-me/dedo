import { onMounted, onUnmounted, ref } from 'vue'
import { useQueryClient } from '@tanstack/vue-query'
import { todos } from './clients'
import { WatchTodosResponse_Change } from './gen/raveesh_me/todo/v1/todo_pb'

export type WatchState = 'connecting' | 'live' | 'reconnecting'

const listKey = ['listTodos']
const sleep = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms))

// useWatchTodos keeps a WatchTodos stream open while the component is
// mounted. Every change, wherever it was made (this tab, another browser,
// REST, or an agent through MCP), refetches the to-do list at once.
export function useWatchTodos() {
  const queryClient = useQueryClient()
  const state = ref<WatchState>('connecting')
  const refetch = () => queryClient.invalidateQueries({ queryKey: listKey })
  let abort: AbortController | undefined
  let stopped = false

  async function run() {
    let delay = 500
    while (!stopped) {
      abort = new AbortController()
      try {
        const stream = todos.watchTodos({}, { signal: abort.signal })
        for await (const event of stream) {
          if (event.change === WatchTodosResponse_Change.WATCHING) {
            // The watch is open: catch up on anything that changed before.
            state.value = 'live'
            delay = 500
          }
          void refetch()
        }
      } catch {
        // The stream failed or the server went away; retry below.
      }
      if (stopped) return
      state.value = 'reconnecting'
      await sleep(delay)
      delay = Math.min(delay * 2, 5000)
    }
  }

  onMounted(() => void run())
  onUnmounted(() => {
    stopped = true
    abort?.abort()
  })
  return { state }
}
