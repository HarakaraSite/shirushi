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

async function installAuthenticatedFixtures(page, options = {}) {
  const {
    total = bookmarks.length,
    allTags = tags,
    requests = [],
    fulfillMutations = false,
  } = options;

  await page.route('**/api/**', async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    const method = request.method();

    if (method !== 'GET') {
      let body = null;
      if (request.headers()['content-type']?.includes('application/json')) {
        body = request.postDataJSON();
      }
      requests.push({ method, pathname: url.pathname, body });

      if (!fulfillMutations) {
        await route.continue();
        return;
      }
      if (url.pathname === '/api/import' && method === 'POST') {
        await route.fulfill({ json: { imported: 1, skipped: 0 } });
        return;
      }
      if (
        url.pathname === '/api/bookmarks/bulk/tags'
        || (url.pathname === '/api/bookmarks' && method === 'DELETE')
      ) {
        await route.fulfill({ json: {} });
        return;
      }

      await route.continue();
      return;
    }

    requests.push({ method, pathname: url.pathname, search: url.search });

    if (url.pathname === '/api/bookmarks/check-404') {
      await route.fulfill({ json: { status: 'idle', checked: 0, total: 0, not_found: 0, failed: 0 } });
      return;
    }
    if (url.pathname === '/api/capabilities') {
      await route.fulfill({ json: { henji_summary: false } });
      return;
    }
    if (url.pathname === '/api/tags') {
      await route.fulfill({ json: url.searchParams.get('all') === '1' ? allTags : tags });
      return;
    }
    if (url.pathname === '/api/bookmarks') {
      await route.fulfill({ json: { bookmarks, total } });
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

function trackBrowserErrors(page) {
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
  return { pageErrors, failedResponses };
}

async function openAuthenticatedUI(page, fixtureOptions = {}) {
  const errors = trackBrowserErrors(page);

  await page.goto('/');
  await page.getByPlaceholder('パスワード').fill('playwright-test-password');
  await Promise.all([
    page.waitForResponse((response) => response.url().endsWith('/api/login') && response.status() === 200),
    page.getByRole('button', { name: 'ログイン' }).click(),
  ]);
  await expect(page.locator('#main-screen')).toBeVisible();
  await expect(page.locator('#bookmark-list')).toContainText('ブックマークはまだありません。');
  await page.waitForLoadState('networkidle');

  await installAuthenticatedFixtures(page, fixtureOptions);
  await page.reload();
  await expect(page.locator('.bookmark-item')).toHaveCount(bookmarks.length);
  await expect(page.locator('#tag-filter-area')).toBeVisible();
  await page.evaluate(() => document.fonts.ready);

  return errors;
}

async function tabTo(page, locator, maxTabs = 12) {
  for (let i = 0; i < maxTabs; i += 1) {
    if (await locator.evaluate((element) => element === document.activeElement)) break;
    await page.keyboard.press('Tab');
  }
  await expect(locator).toBeFocused();
  await expect(locator).toHaveCSS('box-shadow', /rgba?\(/);
  expect(await locator.evaluate((element) => element.matches(':focus-visible'))).toBe(true);
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

test('独自CSSの読込とキーボード状態を維持する', async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 720 });
  const errors = trackBrowserErrors(page);

  await page.goto('/');
  await expect(page.locator('link[rel="stylesheet"]')).toHaveCount(1);
  await expect(page.locator('link[rel="stylesheet"]')).toHaveAttribute('href', '/css/style.css');
  await expect(page.locator('input[hidden]')).toBeHidden();

  await tabTo(page, page.getByPlaceholder('パスワード'));
  await tabTo(page, page.getByRole('checkbox', { name: 'ログイン状態を維持する' }));
  await tabTo(page, page.getByRole('button', { name: 'ログイン' }));

  await page.getByPlaceholder('パスワード').fill('playwright-test-password');
  await Promise.all([
    page.waitForResponse((response) => response.url().endsWith('/api/login') && response.status() === 200),
    page.getByRole('button', { name: 'ログイン' }).click(),
  ]);
  await expect(page.locator('#bookmark-list')).toContainText('ブックマークはまだありません。');
  await page.waitForLoadState('networkidle');
  await installAuthenticatedFixtures(page);
  await page.reload();
  await expect(page.locator('.bookmark-item')).toHaveCount(bookmarks.length);

  await tabTo(page, page.getByRole('button', { name: '＋ 追加' }));
  await tabTo(page, page.getByRole('link', { name: 'エクスポート' }));
  await tabTo(page, page.getByPlaceholder('タイトル・URL・メモで検索...'));
  await tabTo(page, page.locator('#page-size-select'));
  await expect(page.locator('#page-size-select')).toHaveCSS('background-image', /svg/);
  await tabTo(page, page.locator('.bookmark-checkbox').first());

  await page.locator('#check-404-btn').evaluate((button) => { button.disabled = true; });
  await expect(page.locator('#check-404-btn')).toHaveCSS('pointer-events', 'none');
  await expect(page.locator('#check-404-btn')).toHaveCSS('opacity', '0.55');

  const hasHorizontalOverflow = await page.evaluate(
    () => document.documentElement.scrollWidth > document.documentElement.clientWidth,
  );
  expect(hasHorizontalOverflow).toBe(false);
  await expectNoBrowserErrors(errors);
});

test('編集・タグ候補・タグ管理を操作する', async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 720 });
  const errors = await openAuthenticatedUI(page, {
    allTags: [...tags, { id: 13, name: '未使用' }],
  });

  await page.locator('.edit-btn').first().click();
  await expect(page.locator('#modal-box')).toHaveClass(/editing/);
  await expect(page.getByRole('heading', { name: 'ブックマークを編集' })).toBeVisible();
  await expect(page.locator('#input-url')).toHaveValue(bookmarks[0].url);
  await expect(page.locator('#input-title')).toHaveValue(bookmarks[0].title);
  await expect(page.locator('#input-author')).toHaveValue(bookmarks[0].author);
  await expect(page.locator('#input-excerpt')).toHaveValue(bookmarks[0].excerpt);
  await expect(page.locator('#input-excerpt')).toHaveCSS('resize', 'vertical');
  await expect(page.locator('.tag-selected-badge')).toHaveCount(2);

  await page.locator('.tag-selected-badge button').last().click();
  await page.locator('#tag-input').fill('資');
  await expect(page.locator('#tag-dropdown')).toBeVisible();
  await page.locator('#tag-dropdown .tag-dropdown-item').filter({ hasText: '資料' }).click();
  await expect(page.locator('.tag-selected-badge')).toHaveCount(2);
  await page.getByRole('button', { name: 'キャンセル' }).click();

  await page.getByRole('button', { name: 'タグ管理' }).click();
  await expect(page.locator('#tag-manager-overlay')).toHaveClass(/open/);
  await expect(page.locator('.tag-manager-item')).toHaveCount(3);
  await expect(page.locator('.tag-manager-item.unused')).toContainText('未使用 (未使用)');
  await page.getByRole('button', { name: '閉じる' }).click();
  await expect(page.locator('#tag-manager-overlay')).not.toHaveClass(/open/);
  await expectNoBrowserErrors(errors);
});

test('バルク操作・import・export・paginationを操作する', async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 720 });
  const requests = [];
  const errors = await openAuthenticatedUI(page, {
    total: 120,
    requests,
    fulfillMutations: true,
  });
  requests.length = 0;

  const exportLink = page.getByRole('link', { name: 'エクスポート' });
  await expect(exportLink).toHaveAttribute('href', '/api/export');
  await expect(exportLink).toHaveAttribute('download', 'shirushi-bookmarks.html');

  const topPagination = page.locator('.pagination').first();
  await expect(topPagination.getByRole('button', { name: '← 前へ' })).toBeDisabled();
  await expect(topPagination.getByRole('button', { name: '← 前へ' })).toHaveCSS('pointer-events', 'none');
  await topPagination.getByRole('button', { name: '次へ →' }).click();
  await expect.poll(() => requests.some(
    (request) => request.method === 'GET'
      && request.pathname === '/api/bookmarks'
      && request.search.includes('page=2'),
  )).toBe(true);
  await expect(page.locator('.pagination .page-current').first()).toHaveText('2');

  await page.locator('.bookmark-checkbox').first().check();
  await page.getByRole('button', { name: '＋ タグを追加' }).click();
  await page.locator('#bulk-tag-list .bulk-tag-item').filter({ hasText: 'Go' }).click();
  await expect.poll(() => requests.some(
    (request) => request.method === 'POST'
      && request.pathname === '/api/bookmarks/bulk/tags'
      && request.body.bookmark_ids.includes(101)
      && request.body.tag_ids.includes(11),
  )).toBe(true);

  await page.getByRole('button', { name: '－ タグを削除' }).click();
  await page.locator('#bulk-tag-remove-list .bulk-tag-item').filter({ hasText: 'Go' }).click();
  await expect.poll(() => requests.some(
    (request) => request.method === 'DELETE'
      && request.pathname === '/api/bookmarks/bulk/tags'
      && request.body.bookmark_ids.includes(101)
      && request.body.tag_ids.includes(11),
  )).toBe(true);

  await page.getByRole('button', { name: '全て選択' }).click();
  await expect(page.locator('#bulk-count')).toHaveText('2件選択中');
  page.once('dialog', (dialog) => dialog.accept());
  await page.getByRole('button', { name: '一括削除' }).click();
  await expect.poll(() => requests.some(
    (request) => request.method === 'DELETE'
      && request.pathname === '/api/bookmarks'
      && request.body.ids.length === 2,
  )).toBe(true);
  await expect(page.locator('#bulk-bar')).not.toHaveClass(/visible/);

  const importDialog = page.waitForEvent('dialog');
  await page.locator('#import-file').setInputFiles({
    name: 'bookmarks.html',
    mimeType: 'text/html',
    buffer: Buffer.from('<!doctype html><title>bookmarks</title>'),
  });
  const dialog = await importDialog;
  expect(dialog.message()).toBe('インポート完了: 1件追加、0件スキップ');
  await dialog.accept();
  await expect.poll(() => requests.some(
    (request) => request.method === 'POST' && request.pathname === '/api/import',
  )).toBe(true);

  const hasHorizontalOverflow = await page.evaluate(
    () => document.documentElement.scrollWidth > document.documentElement.clientWidth,
  );
  expect(hasHorizontalOverflow).toBe(false);
  await expectNoBrowserErrors(errors);
});
