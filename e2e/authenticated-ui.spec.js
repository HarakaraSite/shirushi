const { test, expect } = require('@playwright/test');

const tags = [
  { id: 11, name: 'Go' },
  { id: 12, name: '資料' },
];

const bookmarks = [
  {
    id: 101,
    url: 'https://example.test/go-guide',
    title: 'Goで作る小さなWebアプリ',
    excerpt: '標準ライブラリを中心に、保守しやすいWebアプリを組み立てるための資料です。',
    author: 'Shirushi Team',
    public: 0,
    has_content: false,
    image_url: '',
    created_at: '2024-01-15T03:00:00Z',
    modified_at: null,
    tags,
  },
  {
    id: 102,
    url: 'https://example.test/css-notes',
    title: 'CSS設計メモ',
    excerpt: 'フォーム、カード、レスポンシブ表示を小さなトークンで揃えるためのメモです。',
    author: '',
    public: 0,
    has_content: false,
    image_url: '',
    created_at: '2024-01-14T03:00:00Z',
    modified_at: null,
    tags: [],
  },
];

async function installAuthenticatedFixtures(page) {
  await page.route('**/api/**', async (route) => {
    const request = route.request();
    const url = new URL(request.url());

    if (request.method() !== 'GET') {
      await route.continue();
      return;
    }

    if (url.pathname === '/api/bookmarks/check-404') {
      await route.fulfill({ json: { status: 'idle', checked: 0, total: 0, not_found: 0, failed: 0 } });
      return;
    }
    if (url.pathname === '/api/capabilities') {
      await route.fulfill({ json: { henji_summary: false } });
      return;
    }
    if (url.pathname === '/api/tags') {
      await route.fulfill({ json: tags });
      return;
    }
    if (url.pathname === '/api/bookmarks') {
      await route.fulfill({ json: { bookmarks, total: bookmarks.length } });
      return;
    }
    if (/^\/api\/bookmarks\/\d+$/.test(url.pathname)) {
      const id = Number(url.pathname.split('/').pop());
      const bookmark = bookmarks.find((item) => item.id === id);
      await route.fulfill({ status: bookmark ? 200 : 404, json: bookmark ?? { error: 'not found' } });
      return;
    }

    await route.continue();
  });
}

async function openAuthenticatedUI(page) {
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
  await page.getByPlaceholder('パスワード').fill('playwright-test-password');
  await Promise.all([
    page.waitForResponse((response) => response.url().endsWith('/api/login') && response.status() === 200),
    page.getByRole('button', { name: 'ログイン' }).click(),
  ]);
  await expect(page.locator('#main-screen')).toBeVisible();

  await installAuthenticatedFixtures(page);
  await page.reload();
  await expect(page.locator('.bookmark-item')).toHaveCount(bookmarks.length);
  await expect(page.locator('#tag-filter-area')).toBeVisible();
  await page.evaluate(() => document.fonts.ready);

  return { pageErrors, failedResponses };
}

async function expectNoBrowserErrors(errors) {
  expect(errors.pageErrors).toEqual([]);
  expect(errors.failedResponses).toEqual([]);
}

test.use({ locale: 'ja-JP', timezoneId: 'Asia/Tokyo' });

test('認証後のブックマーク一覧を表示する', async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 720 });
  const errors = await openAuthenticatedUI(page);

  await expect(page.getByRole('heading', { name: 'Shirushi' })).toBeVisible();
  await expect(page.getByPlaceholder('タイトル・URL・メモで検索...')).toBeVisible();
  await expect(page.locator('.bookmark-item')).toHaveCount(2);
  await expect(page).toHaveScreenshot('authenticated-list.png', {
    animations: 'disabled',
    fullPage: true,
  });
  await expectNoBrowserErrors(errors);
});

test('追加モーダルのフォームを表示する', async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 720 });
  const errors = await openAuthenticatedUI(page);

  await page.getByRole('button', { name: '＋ 追加' }).click();
  await expect(page.locator('#modal-overlay')).toHaveClass(/open/);
  await expect(page.getByRole('heading', { name: 'ブックマークを追加' })).toBeVisible();
  await expect(page.locator('#modal-box')).toHaveScreenshot('add-modal.png', {
    animations: 'disabled',
  });
  await expectNoBrowserErrors(errors);
});

test('バルクタグドロップダウンを表示する', async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 720 });
  const errors = await openAuthenticatedUI(page);

  await page.locator('.bookmark-checkbox').first().check();
  await expect(page.locator('#bulk-bar')).toHaveClass(/visible/);
  await page.getByRole('button', { name: '＋ タグを追加' }).click();
  await expect(page.locator('#bulk-tag-dropdown')).toBeVisible();
  await expect(page).toHaveScreenshot('bulk-tag-dropdown.png', {
    animations: 'disabled',
    fullPage: true,
  });
  await expectNoBrowserErrors(errors);
});

test('モバイル幅で認証後一覧を表示する', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'chromium', 'モバイルbaselineはChromiumで記録する');
  await page.setViewportSize({ width: 375, height: 812 });
  const errors = await openAuthenticatedUI(page);

  const hasHorizontalOverflow = await page.evaluate(
    () => document.documentElement.scrollWidth > document.documentElement.clientWidth,
  );
  expect(hasHorizontalOverflow).toBe(false);
  await expect(page).toHaveScreenshot('authenticated-list-mobile.png', {
    animations: 'disabled',
    fullPage: true,
  });
  await expectNoBrowserErrors(errors);
});
