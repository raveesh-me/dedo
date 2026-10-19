import { onMounted, onUnmounted, ref } from 'vue'
import { useQueryClient } from '@tanstack/vue-query'
import { todos } from './clients'

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
        const stream = todos.watchTodos({}, {
          signal: abort.signal,
          // The server sends headers once the watch is open: catch up on
          // anything that changed before then.
          onHeader: () => {
            state.value = 'live'
            delay = 500
            void refetch()
          },
        })
        for await (const _change of stream) {
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
