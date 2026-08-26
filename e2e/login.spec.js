const { test, expect } = require('@playwright/test');

test('ログイン画面を表示する', async ({ page }) => {
  const pageErrors = [];
  const failedResponses = [];
  page.on('pageerror', (error) => pageErrors.push(error.message));
  page.on('response', (response) => {
    const url = new URL(response.url());
    const isExpectedAuthCheck = url.pathname === '/api/bookmarks' && response.status() === 401;
    if (response.status() >= 400 && !isExpectedAuthCheck) {
      failedResponses.push(`${response.status()} ${url.pathname}`);
    }
  });

  await page.goto('/');

  await expect(page.locator('#login-screen')).toBeVisible();
  await expect(page.getByRole('heading', { name: 'Shirushi' })).toBeVisible();
  await expect(page.getByPlaceholder('パスワード')).toBeVisible();
  await expect(page.getByRole('checkbox', { name: 'ログイン状態を維持する' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'ログイン' })).toBeVisible();
  await expect(page.locator('#main-screen')).toBeHidden();
  await expect(page).toHaveScreenshot('login-screen.png', {
    animations: 'disabled',
    fullPage: true,
  });
  expect(pageErrors).toEqual([]);
  expect(failedResponses).toEqual([]);
});
