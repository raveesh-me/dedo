<script setup lang="ts">
import { computed, ref } from 'vue'
import { useQueryClient } from '@tanstack/vue-query'
import { Code, ConnectError } from '@connectrpc/connect'
import { todos, token } from './clients'
import { useWatchTodos } from './useWatchTodos'

const queryClient = useQueryClient()
const { state } = useWatchTodos()

const list = todos.listTodos.useQuery({ pageSize: 100 })
const items = computed(() => list.data.value?.todos ?? [])

const error = ref('')
const refresh = () => {
  error.value = ''
  return queryClient.invalidateQueries({ queryKey: ['listTodos'] })
}
const onError = (e: Error) => {
  const err = ConnectError.from(e)
  error.value = `${Code[err.code]}: ${err.rawMessage}`
}

const create = todos.createTodo.useMutation({ onSuccess: refresh, onError })
const update = todos.updateTodo.useMutation({ onSuccess: refresh, onError })
const remove = todos.deleteTodo.useMutation({ onSuccess: refresh, onError })

const title = ref('')
function add() {
  const text = title.value.trim()
  if (!text) return
  create.mutate({ todo: { title: text } })
  title.value = ''
}
</script>

<template>
  <main>
    <header>
      <h1>To-dos</h1>
      <span class="state" :class="state" data-testid="watch-state">{{ state }}</span>
      <label>
        Token
        <select v-model="token" data-testid="token">
          <option value="writer">writer</option>
          <option value="reader">reader</option>
        </select>
      </label>
    </header>

    <form @submit.prevent="add">
      <input v-model="title" placeholder="What needs doing?" data-testid="new-title" />
      <button type="submit" data-testid="add">Add</button>
    </form>

    <p v-if="error" class="error" data-testid="error">{{ error }}</p>

    <ul>
      <li v-for="todo in items" :key="todo.name" data-testid="todo-item" :data-name="todo.name">
        <input
          type="checkbox"
          :checked="todo.done"
          data-testid="toggle"
          @change="update.mutate({ todo: { name: todo.name, done: !todo.done }, updateMask: { paths: ['done'] } })"
        />
        <span :class="{ done: todo.done }" data-testid="todo-title">{{ todo.title }}</span>
        <button type="button" data-testid="delete" @click="remove.mutate({ name: todo.name })">Delete</button>
      </li>
    </ul>
    <p v-if="list.isSuccess.value && items.length === 0" class="empty">Nothing to do.</p>

    <footer>
      Changes from anywhere (this tab, another browser, REST, or an agent through MCP) appear here live.
      <a href="/docs">REST reference</a>
    </footer>
  </main>
</template>
