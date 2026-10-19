import { expect, test } from '@playwright/test'

// The REST reference renders the operations declared in the .proto, with
// their tool titles as summaries.
test('the REST reference renders the API from /openapi.json', async ({ page }) => {
  await page.goto('/docs')
  await expect(page.getByText('Todo API').first()).toBeVisible({ timeout: 20_000 })
  // Endpoints are grouped by service; open the group.
  await page.getByText('TodoService', { exact: true }).first().click()
  for (const summary of ['Get a to-do', 'List to-dos', 'Create a to-do', 'Delete a to-do']) {
    await expect(page.getByText(summary).first()).toBeVisible()
  }
})

// The try-it console calls the origin that served the docs, not a fixed host.
test('the REST reference targets the serving origin', async ({ request }) => {
  const spec = await (await request.get('/openapi.json')).json()
  expect(spec.servers).toEqual([{ url: '/' }])
})
