const { test, expect } = require('@playwright/test');

async function login(page) {
  await page.goto('/');
  await page.getByPlaceholder('パスワード').fill('playwright-test-password');
  await Promise.all([
    page.waitForResponse((response) => response.url().endsWith('/api/login') && response.status() === 200),
    page.getByRole('button', { name: 'ログイン' }).click(),
  ]);
  await expect(page.locator('#main-screen')).toBeVisible();
}

test.use({ locale: 'ja-JP', timezoneId: 'Asia/Tokyo' });

test('404チェックの進捗とサムネイル更新を完了まで表示する', async ({ page }, testInfo) => {
  const fixtureHost = process.env.SHIRUSHI_E2E_404_HOST || '127.0.0.1';
  const fixturePort = process.env.SHIRUSHI_E2E_404_PORT || '18182';
  const fixtureURL = `http://${fixtureHost}:${fixturePort}/missing-${testInfo.project.name}`;
  await login(page);

  const consoleErrors = [];
  const pageErrors = [];
  page.on('console', (message) => {
    if (message.type() === 'error') consoleErrors.push(message.text());
  });
  page.on('pageerror', (error) => pageErrors.push(error.message));

  const bookmark = await page.evaluate(async (url) => {
    const response = await fetch('/api/bookmarks', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ url, title: '404チェック Playwright fixture' }),
    });
    if (!response.ok) throw new Error(`bookmark作成失敗: ${response.status}`);
    return response.json();
  }, fixtureURL);

  await page.reload();
  const card = page.locator(`.bookmark-item[data-id="${bookmark.id}"]`);
  await expect(card).toBeVisible();

  let resolveCompletion;
  const completionDialog = new Promise((resolve) => { resolveCompletion = resolve; });
  const dialogMessages = [];
  page.on('dialog', async (dialog) => {
    dialogMessages.push(dialog.message());
    if (dialog.type() === 'confirm') {
      await dialog.accept();
      return;
    }
    resolveCompletion(dialog.message());
    await dialog.accept();
  });

  const button = page.locator('#check-404-btn');
  await button.click();
  await expect(button).toBeDisabled();
  await expect(button).toContainText('確認中');

  const completionMessage = await completionDialog;
  expect(completionMessage).toBe('1件を確認しました。404: 1件');
  expect(dialogMessages[0]).toContain('登録済みの全ブックマークURLを確認します。');
  await expect(button).toBeEnabled();
  await expect(button).toHaveText('404チェック');
  await expect(card.locator('.bookmark-thumb')).toHaveAttribute('src', '/404.svg');

  const updated = await page.evaluate(async (id) => {
    const response = await fetch(`/api/bookmarks/${id}`);
    if (!response.ok) throw new Error(`bookmark取得失敗: ${response.status}`);
    return response.json();
  }, bookmark.id);
  expect(updated.image_url).toBe('/404.svg');
  expect(updated.modified_at).not.toBeNull();
  expect(consoleErrors).toEqual([]);
  expect(pageErrors).toEqual([]);

  const deleted = await page.evaluate(async (id) => {
    const response = await fetch(`/api/bookmarks/${id}`, { method: 'DELETE' });
    return response.status;
  }, bookmark.id);
  expect(deleted).toBe(204);
});
