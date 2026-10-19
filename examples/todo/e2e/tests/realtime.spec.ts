import { expect, test, type Page } from '@playwright/test'
import { callTool, unique } from './mcp'

// open loads the UI and waits until its WatchTodos stream is open.
async function open(page: Page) {
  await page.goto('/')
  await expect(page.getByTestId('watch-state')).toHaveText('live')
}

const item = (page: Page, title: string) => page.getByTestId('todo-item').filter({ hasText: title })

test('a to-do an agent adds through MCP appears in the open UI without a reload', async ({ page, request }) => {
  await open(page)
  let reloads = 0
  page.on('load', () => reloads++)

  const title = unique('Added by an agent')
  const created = await callTool(request, 'writer', 'create_todo', { todo: { title } })
  expect(created.isError ?? false).toBe(false)

  await expect(item(page, title)).toBeVisible()

  const name = await item(page, title).getAttribute('data-name')
  const deleted = await callTool(request, 'writer', 'delete_todo', { name })
  expect(deleted.isError ?? false).toBe(false)
  await expect(item(page, title)).toHaveCount(0)

  expect(reloads).toBe(0)
})

test('a change in one browser appears live in another', async ({ browser }) => {
  const alice = await (await browser.newContext()).newPage()
  const bob = await (await browser.newContext()).newPage()
  await open(alice)
  await open(bob)

  const title = unique('Shared')
  await alice.getByTestId('new-title').fill(title)
  await alice.getByTestId('add').click()
  await expect(item(bob, title)).toBeVisible()

  await item(alice, title).getByTestId('toggle').check()
  await expect(item(bob, title).getByTestId('todo-title')).toHaveClass(/done/)

  await item(bob, title).getByTestId('delete').click()
  await expect(item(alice, title)).toHaveCount(0)
})

test('the reader token cannot add a to-do', async ({ page }) => {
  await open(page)
  await page.getByTestId('token').selectOption('reader')
  const title = unique('Not allowed')
  await page.getByTestId('new-title').fill(title)
  await page.getByTestId('add').click()
  // connect-es names the code PermissionDenied.
  await expect(page.getByTestId('error')).toContainText(/permission.?denied/i)
  await expect(item(page, title)).toHaveCount(0)
})

test('an agent cannot write with the reader token', async ({ request }) => {
  const res = await callTool(request, 'reader', 'create_todo', { todo: { title: unique('Agent as reader') } })
  expect(res.isError).toBe(true)
  expect(JSON.stringify(res.content)).toContain('PERMISSION_DENIED')
})
