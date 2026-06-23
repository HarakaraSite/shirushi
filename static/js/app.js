let allTags = [];        // サーバーから取得した全タグ
let selectedTags = [];   // モーダルフォームで選択中のタグ
let editingId = null;    // null = 追加モード、数値 = 編集対象のID
let activeTag = null;    // 現在選択中のフィルタータグ名（null = 絞り込みなし）
let selectedIds = new Set(); // バッチ選択中のブックマークID集合
let pendingImageUrl = ''; // メタデータ取得で得たOG画像URL（フォーム送信まで保持）
let currentPage = 1;     // 現在表示しているページ番号（1始まり）
let tagInputInitialized = false; // タグ入力欄のイベント登録が済んでいるか
const PAGE_SIZE = 50;    // 1ページあたりの表示件数

// OG画像がない・読み込み失敗時に使うSVGプレースホルダーです。
// data URI にすることでファイル不要でインラインに埋め込めます。
// SVGの中でブックマークアイコンを描いています。
const THUMB_PLACEHOLDER = "data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='80' height='60'%3E%3Crect width='80' height='60' fill='%23f0f4f8'/%3E%3Cpath d='M32 16h16v28l-8-5-8 5z' fill='%23c7d2e0'/%3E%3C/svg%3E";

// ブックマークデータを ID で引けるように保持します。
// onclick に JSON.stringify を直接埋め込むと、タイトルや抜粋に含まれる
// シングルクォートや特殊文字でHTML属性が壊れるため、この方式を使います。
const bookmarkMap = new Map(); // Map<id: number, bookmark: object>

// ページ読み込み時に認証状態を確認します。
document.addEventListener('DOMContentLoaded', async () => {
  await checkAuth();
});

// ===== 認証 =====

async function checkAuth() {
  let res;
  try {
    res = await fetch('/api/bookmarks');
  } catch (err) {
    console.error('認証確認に失敗しました:', err);
    showLoginScreen('サーバーに接続できません');
    return;
  }

  if (res.status === 401) {
    showLoginScreen();
  } else if (!res.ok) {
    console.error('認証確認APIがエラーを返しました:', res.status);
    showLoginScreen('サーバーエラーが発生しました');
  } else {
    // 認証確認に使ったレスポンスは一覧データそのものなので、
    // showMainScreen に渡して再利用します（同じAPIを2回呼ぶ無駄を省く）。
    showMainScreen(res);
  }
}

function showLoginScreen(message = '') {
  // 検索語・タグフィルター・選択状態をリセットします。
  // ログアウト（またはセッション切れ）後に別の人がログインしたとき、
  // 前の利用状態が画面に残らないようにするためです。
  // ※この関数はログアウトと apiFetch の401検知の両方から呼ばれます。
  document.getElementById('search-input').value = '';
  activeTag = null;
  selectedIds.clear();
  updateBulkBar();
  // 開いたままのモーダルやドロップダウンも閉じます。
  closeModal();
  closeTagManager();
  closeBulkTagDropdown();
  closeBulkTagRemoveDropdown();

  document.getElementById('login-screen').style.display = 'block';
  document.getElementById('main-screen').style.display = 'none';
  const loginError = document.getElementById('login-error');
  if (message) {
    loginError.textContent = message;
    loginError.style.display = 'block';
  } else {
    loginError.textContent = 'パスワードが違います';
    loginError.style.display = 'none';
  }
}

// preloadedRes：checkAuth が取得済みの一覧レスポンス（あれば再利用します）。
async function showMainScreen(preloadedRes = null) {
  document.getElementById('login-screen').style.display = 'none';
  document.getElementById('main-screen').style.display = 'block';
  await loadTags();
  await loadBookmarks('', null, 1, preloadedRes);
  setupTagInput();
}

document.getElementById('login-form').addEventListener('submit', async (e) => {
  e.preventDefault();
  const password = document.getElementById('input-password').value;
  const res = await fetch('/api/login', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ password }),
  });
  if (res.ok) {
    document.getElementById('input-password').value = '';
    document.getElementById('login-error').style.display = 'none';
    showMainScreen();
  } else {
    const loginError = document.getElementById('login-error');
    loginError.textContent = 'パスワードが違います';
    loginError.style.display = 'block';
  }
});

async function logout() {
  await fetch('/api/logout', { method: 'POST' });
  showLoginScreen();
}

// apiFetch：fetch のラッパーで、セッション切れ（401）を一元処理します。
// セッションの有効期限は24時間なので、使っている途中で切れることがあります。
// 401のとき各処理が res.json() の失敗などで静かに壊れるのを防ぐため、
// ここでログイン画面に戻し、呼び出し元には null を返します。
// 呼び出し側は「if (!res) return;」と書くだけで中断できます。
async function apiFetch(url, options) {
  const res = await fetch(url, options);
  if (res.status === 401) {
    showLoginScreen();
    return null;
  }
  return res;
}

// ===== 検索 =====

// debounce: 入力が止まって300ms後に検索します。
// activeTag（タグフィルター）を維持したまま検索します。
let searchTimer = null;
document.getElementById('search-input').addEventListener('input', (e) => {
  clearTimeout(searchTimer);
  searchTimer = setTimeout(() => {
    loadBookmarks(e.target.value.trim(), activeTag);
  }, 300);
});

// ===== データ取得 =====

async function loadTags() {
  const res = await apiFetch('/api/tags');
  if (!res) return; // セッション切れ：ログイン画面に戻っているので中断します
  allTags = await res.json() ?? [];
  renderTagFilterChips();
}

// タグフィルターチップを描画します。
// タグが1件もない場合はエリアごと非表示にします。
function renderTagFilterChips() {
  const area = document.getElementById('tag-filter-area');
  if (allTags.length === 0) {
    area.style.display = 'none';
    return;
  }
  area.style.display = 'flex';

  // 「タグなし」チップを先頭に固定表示します。
  // "__untagged__" はバックエンドと合わせた特殊値です。
  const untaggedChip = `
    <span
      class="tag-filter-chip ${activeTag === '__untagged__' ? 'active' : ''}"
      onclick="toggleTagFilter('__untagged__')"
    >タグなし</span>
  `;

  // onclick にタグ名を文字列として埋め込むと、名前に ' や \ が含まれる場合に
  // JS構文エラーになるため、ID を渡して toggleTagFilterById で名前を引きます。
  // （bookmarkMap と同じ「ID経由で引く」方式です）
  const tagChips = allTags.map(t => `
    <span
      class="tag-filter-chip ${activeTag === t.name ? 'active' : ''}"
      onclick="toggleTagFilterById(${t.id})"
    >${escapeHtml(t.name)}</span>
  `).join('');

  area.innerHTML = untaggedChip + tagChips;
}

// IDからタグを引いてフィルターを切り替えます（onclick用の安全な入口）。
function toggleTagFilterById(id) {
  const t = allTags.find(t => t.id === id);
  if (t) toggleTagFilter(t.name);
}

// タグチップをクリックしたときの処理です。
// 選択中のタグと同じものをクリックすると絞り込みを解除します。
function toggleTagFilter(tagName) {
  activeTag = (activeTag === tagName) ? null : tagName;
  renderTagFilterChips(); // チップのアクティブ状態を更新します
  const q = document.getElementById('search-input').value.trim();
  loadBookmarks(q, activeTag);
}

// q（キーワード）・tag（タグ名）・page（ページ番号）を組み合わせてブックマークを取得します。
// 検索やタグフィルターが変わった場合は page=1 にリセットして呼び出します。
// preloadedRes が渡された場合はAPIを呼ばず、そのレスポンスを使います（起動時の再利用）。
async function loadBookmarks(q = '', tag = null, page = 1, preloadedRes = null) {
  const list = document.getElementById('bookmark-list');
  currentPage = page; // 現在ページを記録します

  // クエリパラメータを組み立てます。
  const params = new URLSearchParams();
  if (q)        params.set('q',    q);
  if (tag)      params.set('tag',  tag);
  if (page > 1) params.set('page', page);
  const qs = params.toString();
  const url = qs ? `/api/bookmarks?${qs}` : '/api/bookmarks';

  const res = preloadedRes ?? await apiFetch(url);
  if (!res) return; // セッション切れ
  // レスポンスは { bookmarks: [...], total: N } の形式です。
  const resp = await res.json();
  const bookmarks = resp.bookmarks ?? [];
  const total     = resp.total     ?? 0;

  // 削除などで総ページ数が減り、存在しないページを開いてしまった場合は
  // 最終ページに移動し直します（例: 3ページ目を表示中にタグ削除で2ページに減った）。
  // これがないと、データはあるのに「ブックマークはまだありません」と表示されます。
  if (bookmarks.length === 0 && total > 0 && page > 1) {
    return loadBookmarks(q, tag, Math.ceil(total / PAGE_SIZE));
  }

  if (bookmarks.length === 0) {
    list.innerHTML = '<p>ブックマークはまだありません。</p>';
    document.getElementById('pagination').innerHTML = '';
    // 表示が0件になったら選択もすべて解除します。
    selectedIds.clear();
    updateBulkBar();
    return;
  }

  // 最新のブックマーク一覧で Map を更新します。
  // これにより、編集ボタン押下時に ID だけで元データを取得できます。
  bookmarkMap.clear();
  bookmarks.forEach(b => bookmarkMap.set(b.id, b));

  // 選択状態を「いま表示されている項目」だけに絞り込みます。
  // 検索・フィルター・ページ移動で画面から消えた項目が選択されたまま残ると、
  // 「見えていないものまで一括削除される」事故につながるためです。
  for (const id of [...selectedIds]) {
    if (!bookmarkMap.has(id)) selectedIds.delete(id);
  }
  updateBulkBar();

  // ページネーションUIを描画します。
  renderPagination(total, page);

  list.innerHTML = bookmarks.map(b => `
    <div class="bookmark-item" data-id="${b.id}">

      <!-- ── サムネイル（カード上部全幅）＋チェックボックス ── -->
      <div class="bookmark-thumb-wrapper">
        <!-- チェックボックス：サムネイル左上にオーバーレイ表示します -->
        <input
          type="checkbox"
          class="bookmark-checkbox"
          onchange="toggleSelect(${b.id}, this.checked)"
          ${selectedIds.has(b.id) ? 'checked' : ''}
        >
        <!-- サムネイルをクリックしてもURLを開けるよう a タグで包みます -->
        <a href="${escapeHtml(safeHref(b.url))}" target="_blank" rel="noopener">
          <img
            class="bookmark-thumb"
            src="${b.image_url ? escapeHtml(b.image_url) : THUMB_PLACEHOLDER}"
            alt=""
            loading="lazy"
            onerror="this.src=THUMB_PLACEHOLDER"
          >
        </a>
      </div>

      <!-- ── カード本体（テキスト情報） ── -->
      <div class="bookmark-body">
        <a class="title-link" href="${escapeHtml(safeHref(b.url))}" target="_blank" rel="noopener">${escapeHtml(b.title || b.url)}</a>
        <div class="bookmark-url">${escapeHtml(b.url)}</div>
        ${b.author  ? `<div class="author">${escapeHtml(b.author)}</div>` : ''}
        ${b.excerpt ? `<div class="excerpt">${escapeHtml(b.excerpt)}</div>` : ''}
        ${b.tags && b.tags.length > 0 ? `
          <div class="tag-list">
            ${b.tags.map(t => `<span class="tag-badge">${escapeHtml(t.name)}</span>`).join('')}
          </div>
        ` : ''}
        <!-- フッター：日付（左）と編集・削除ボタン（右） -->
        <div class="bookmark-footer">
          <div class="date">${new Date(b.created_at).toLocaleDateString('ja-JP')}</div>
          <div class="bookmark-actions">
            <button class="edit-btn"   onclick="openEditModal(${b.id})">編集</button>
            <button class="delete-btn" onclick="deleteBookmark(${b.id})">削除</button>
          </div>
        </div>
      </div>

    </div>
  `).join('');
}

// ===== タグ管理 =====

// タグ管理モーダルを開きます。全タグ（未使用含む）を取得して表示します。
async function openTagManager() {
  document.getElementById('tag-manager-overlay').classList.add('open');
  await renderTagManagerList();
}

function closeTagManager() {
  document.getElementById('tag-manager-overlay').classList.remove('open');
}

function onTagManagerOverlayClick(e) {
  if (e.target === document.getElementById('tag-manager-overlay')) closeTagManager();
}

// タグ管理モーダルに表示中のタグ名を ID で引くためのマップです。
// onclick にタグ名を文字列として埋め込むと ' や \ で壊れるため、ID経由で引きます。
// （未使用タグは allTags に含まれないため、bookmarkMap と同様に専用のMapを持ちます）
const tagManagerMap = new Map(); // Map<id: number, name: string>

// 全タグ一覧を描画します。
// ?all=1 で未使用タグも取得します。使用中タグとの区別は usedIds で判定します。
async function renderTagManagerList() {
  const listEl = document.getElementById('tag-manager-list');
  listEl.innerHTML = '<p style="color:#888; font-size:0.85rem;">読み込み中...</p>';

  // 全タグ（未使用含む）と使用中タグを並行取得します。
  const [allRes, usedRes] = await Promise.all([
    apiFetch('/api/tags?all=1'),
    apiFetch('/api/tags'),
  ]);
  if (!allRes || !usedRes) return; // セッション切れ
  const allTagsList  = await allRes.json()  ?? [];
  const usedTagsList = await usedRes.json() ?? [];

  // 使用中タグのIDをSetに入れてO(1)で参照できるようにします。
  const usedIds = new Set(usedTagsList.map(t => t.id));

  if (allTagsList.length === 0) {
    listEl.innerHTML = '<p style="color:#888; font-size:0.85rem;">タグがありません。</p>';
    return;
  }

  // 表示するタグを Map に記録します（deleteTag が ID から名前を引けるように）。
  tagManagerMap.clear();
  allTagsList.forEach(t => tagManagerMap.set(t.id, t.name));

  listEl.innerHTML = allTagsList.map(t => {
    const isUsed = usedIds.has(t.id);
    return `
      <div class="tag-manager-item ${isUsed ? '' : 'unused'}">
        <span>${escapeHtml(t.name)}${isUsed ? '' : ' <small>(未使用)</small>'}</span>
        <button class="tag-delete-btn" onclick="deleteTag(${t.id})">削除</button>
      </div>
    `;
  }).join('');
}

// タグを削除します。削除後は一覧を再描画します。
// タグ名は onclick の引数ではなく tagManagerMap から ID で引きます。
async function deleteTag(id) {
  const name = tagManagerMap.get(id) ?? '';
  if (!confirm(`タグ「${name}」を削除しますか？\n※このタグが付いたブックマークからも外れます。`)) return;

  const res = await apiFetch(`/api/tags/${id}`, { method: 'DELETE' });
  if (!res) return; // セッション切れ
  if (!res.ok) {
    alert('削除に失敗しました');
    return;
  }

  // タグ一覧とフィルターチップを更新します。
  await Promise.all([
    renderTagManagerList(),
    loadTags(),
  ]);
  // 現在の検索・フィルター状態を維持したまま一覧を再読み込みします。
  const q = document.getElementById('search-input').value.trim();
  // 削除したタグがアクティブフィルターだった場合は解除します。
  if (activeTag === name) activeTag = null;
  loadBookmarks(q, activeTag, currentPage);
}

// ===== ページネーション =====

// 指定ページに移動します。検索・タグフィルターの状態を維持します。
function goToPage(page) {
  const q = document.getElementById('search-input').value.trim();
  loadBookmarks(q, activeTag, page);
  // ページ移動時は画面上部に戻します。
  window.scrollTo({ top: 0, behavior: 'smooth' });
}

// 表示するページ番号の配列を生成します。
// 「…」は文字列 '...' として含めます。
// 例: current=7, total=20 → [1, '...', 5, 6, 7, 8, 9, '...', 20]
function getPageNumbers(current, total) {
  // ページ数が少ない場合はすべて表示します。
  if (total <= 7) {
    return Array.from({ length: total }, (_, i) => i + 1);
  }

  const delta = 2; // 現在ページの前後に何ページ表示するか
  const pages = [];

  // 先頭ページは常に表示します。
  pages.push(1);

  const left  = current - delta;
  const right = current + delta;

  // 先頭ページと左端の間が離れていれば「…」を入れます。
  if (left > 2) pages.push('...');

  // 現在ページ周辺を表示します（先頭・末尾と重複しないよう範囲を絞ります）。
  for (let p = Math.max(2, left); p <= Math.min(total - 1, right); p++) {
    pages.push(p);
  }

  // 右端と末尾ページの間が離れていれば「…」を入れます。
  if (right < total - 1) pages.push('...');

  // 末尾ページは常に表示します。
  pages.push(total);

  return pages;
}

// ページネーションUIを描画します。
// total（総件数）と currentPage から表示内容を決めます。
function renderPagination(total, page) {
  const el = document.getElementById('pagination');
  const totalPages = Math.ceil(total / PAGE_SIZE);

  // 1ページに収まる場合は表示しません。
  if (totalPages <= 1) {
    el.innerHTML = '';
    return;
  }

  const pageNums = getPageNumbers(page, totalPages);

  // 「前へ」ボタン
  let html = '<div class="pagination">';
  html += `<button ${page <= 1 ? 'disabled' : `onclick="goToPage(${page - 1})"`}>← 前へ</button>`;

  // ページ番号ボタン（または「…」）
  for (const p of pageNums) {
    if (p === '...') {
      html += '<span class="pagination-ellipsis">…</span>';
    } else if (p === page) {
      html += `<button class="page-current">${p}</button>`;
    } else {
      html += `<button onclick="goToPage(${p})">${p}</button>`;
    }
  }

  // 「次へ」ボタン
  html += `<button ${page >= totalPages ? 'disabled' : `onclick="goToPage(${page + 1})"`}>次へ →</button>`;
  html += '</div>';

  el.innerHTML = html;
}

// safeHref：リンクにして安全なURLだけをそのまま返します。
// "javascript:alert(1)" のようなURLを href に入れるとクリックでスクリプトが
// 実行されてしまうため、http / https 以外は無害な "#" に置き換えます。
// （インポート機能で他人が作ったブックマークファイルを取り込む経路があるため）
function safeHref(url) {
  return /^https?:\/\//i.test(url) ? url : '#';
}

// XSS対策：ユーザーデータをHTMLに埋め込む前にエスケープします。
// < > & " ' などの特殊文字をHTMLエンティティに変換することで、
// 悪意あるスクリプトが実行されるのを防ぎます。
function escapeHtml(str) {
  if (!str) return '';
  return str
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;');
}

// ===== モーダル =====

// 「追加モード」でモーダルを開きます。フォームを空にします。
function openModal() {
  editingId = null;
  pendingImageUrl = ''; // 画像URLをリセットします
  document.getElementById('modal-heading').textContent = 'ブックマークを追加';
  document.getElementById('modal-submit-btn').textContent = '登録';
  document.getElementById('bookmark-form').reset();
  selectedTags = [];
  renderSelectedTags();
  document.getElementById('modal-overlay').classList.add('open');
  document.getElementById('input-url').focus();
}

// 「編集モード」でモーダルを開きます。既存データをフォームに入れます。
// 引数は bookmark の ID です。データは bookmarkMap から取得します。
// ※ onclick に JSON.stringify を直接埋め込むと、タイトル・抜粋に含まれる
//    シングルクォート等でHTML属性が壊れるため、ID経由でMapを引く方式にしています。
function openEditModal(id) {
  const b = bookmarkMap.get(id);
  if (!b) return; // 念のため存在チェック

  editingId = b.id;
  document.getElementById('modal-heading').textContent = 'ブックマークを編集';
  document.getElementById('modal-submit-btn').textContent = '更新';

  // フォームに既存の値をセットします。
  document.getElementById('input-url').value     = b.url     ?? '';
  document.getElementById('input-title').value   = b.title   ?? '';
  document.getElementById('input-author').value  = b.author  ?? '';
  document.getElementById('input-excerpt').value = b.excerpt ?? '';

  // 既存のタグを選択済みにします。
  // b.tags が null の場合（タグなし）は空配列にします。
  selectedTags = b.tags ? b.tags.map(t => ({ id: t.id, name: t.name })) : [];
  renderSelectedTags();

  // 編集モードでは既存の image_url を引き継ぎます。
  pendingImageUrl = b.image_url ?? '';

  document.getElementById('modal-overlay').classList.add('open');
  document.getElementById('input-title').focus();
}

// モーダルを閉じます。
function closeModal() {
  document.getElementById('modal-overlay').classList.remove('open');
}

// オーバーレイ（背景の暗い部分）をクリックしたときだけ閉じます。
// モーダルボックス内のクリックは伝播しますが、ここで除外します。
function onOverlayClick(e) {
  if (e.target === document.getElementById('modal-overlay')) {
    closeModal();
  }
}

// ===== フォーム送信 =====

// URLのblurでメタデータを自動取得します。
// 追加モード：タイトル・著者・抜粋・画像URLをすべて取得します。
// 編集モード：画像URLが未設定の場合のみ取得します（既存テキストは上書きしません）。
document.getElementById('input-url').addEventListener('blur', async () => {
  const url = document.getElementById('input-url').value.trim();
  if (!url) return;

  const isEditMode = editingId !== null;
  const titleInput = document.getElementById('input-title');

  // 編集モードでタイトルが入力済み、かつ画像も取得済みなら何もしません。
  if (isEditMode && pendingImageUrl) return;
  // 追加モードでタイトルが入力済みなら上書きしません（画像は取得します）。
  const skipText = isEditMode || titleInput.value.trim() !== '';

  titleInput.placeholder = '取得中...';
  const res = await apiFetch('/api/fetch-metadata', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ url }),
  });
  titleInput.placeholder = 'タイトル';
  if (!res || !res.ok) return; // セッション切れ or 取得失敗

  const meta = await res.json();

  // テキスト系フィールドは追加モード・未入力のときだけ自動入力します。
  if (!skipText) {
    if (meta.title   && !titleInput.value)
      titleInput.value = meta.title;
    if (meta.excerpt && !document.getElementById('input-excerpt').value)
      document.getElementById('input-excerpt').value = meta.excerpt;
    if (meta.author  && !document.getElementById('input-author').value)
      document.getElementById('input-author').value = meta.author;
  }

  // 画像URLは追加・編集モードともに未設定のときだけ保存します。
  if (meta.image_url && !pendingImageUrl)
    pendingImageUrl = meta.image_url;
});

// フォーム送信：editingId の有無で POST / PUT を切り替えます。
document.getElementById('bookmark-form').addEventListener('submit', async (e) => {
  e.preventDefault();

  const payload = {
    url:       document.getElementById('input-url').value,
    title:     document.getElementById('input-title').value,
    author:    document.getElementById('input-author').value,
    excerpt:   document.getElementById('input-excerpt').value,
    image_url: pendingImageUrl, // メタデータ取得 or 編集で引き継いだOG画像URL
    tags:      selectedTags.map(t => ({ id: t.id })),
  };

  let res;
  if (editingId === null) {
    // 追加モード：POST /api/bookmarks
    res = await apiFetch('/api/bookmarks', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload),
    });
  } else {
    // 編集モード：PUT /api/bookmarks/:id
    res = await apiFetch(`/api/bookmarks/${editingId}`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload),
    });
  }
  if (!res) return; // セッション切れ

  if (res.ok) {
    closeModal();
    // 検索ワードとタグフィルターを維持したまま一覧を再読み込みします。
    const q = document.getElementById('search-input').value.trim();
    await loadBookmarks(q, activeTag, currentPage);
    await loadTags(); // 新規タグが追加された場合にフィルターチップを更新します
  } else {
    // サーバーが返すエラーメッセージ（例:「このURLは既に登録されています」）を
    // そのまま表示します。本文が空の場合は従来の汎用メッセージを使います。
    const message = (await res.text()).trim();
    alert(message || (editingId === null ? '登録に失敗しました' : '更新に失敗しました'));
  }
});

// ===== タグオートコンプリート =====

function setupTagInput() {
  if (tagInputInitialized) return;
  tagInputInitialized = true;

  const input    = document.getElementById('tag-input');
  const dropdown = document.getElementById('tag-dropdown');

  // 入力のたびにドロップダウン候補を更新します。
  input.addEventListener('input', () => {
    renderTagDropdown(input.value.trim());
  });

  // Enterキーでタグを確定します。
  // ドロップダウンに完全一致があれば選択、なければ新規作成します。
  input.addEventListener('keydown', async (e) => {
    if (e.key !== 'Enter') return;
    e.preventDefault(); // フォーム送信を阻止します

    const name = input.value.trim();
    if (!name) return;

    // すでに選択済みのタグは追加しません。
    if (selectedTags.some(t => t.name.toLowerCase() === name.toLowerCase())) {
      input.value = '';
      dropdown.style.display = 'none';
      return;
    }

    // allTags に同名のタグがあれば選択、なければ新規作成します。
    const existing = allTags.find(t => t.name.toLowerCase() === name.toLowerCase());
    if (existing) {
      selectTag(existing.id, existing.name);
    } else {
      // POST /api/tags で新規タグを作成します。
      const res = await apiFetch('/api/tags', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name }),
      });
      if (!res) return; // セッション切れ
      if (!res.ok) {
        alert('タグの作成に失敗しました');
        return;
      }
      const newTag = await res.json();
      // 作成したタグを allTags にも追加して次回以降の候補に出るようにします。
      allTags.push(newTag);
      selectTag(newTag.id, newTag.name);
    }
  });

  // モーダル外をクリックしたらドロップダウンを閉じます。
  document.addEventListener('click', (e) => {
    if (!e.target.closest('#tag-input-area')) {
      dropdown.style.display = 'none';
    }
  });
}

// タグ候補のドロップダウンを描画します。
function renderTagDropdown(query) {
  const dropdown = document.getElementById('tag-dropdown');
  if (!query) {
    dropdown.style.display = 'none';
    return;
  }

  const selectedTagIds = selectedTags.map(t => t.id);
  const filtered = allTags.filter(t =>
    t.name.toLowerCase().includes(query.toLowerCase()) && !selectedTagIds.includes(t.id)
  );

  if (filtered.length === 0) {
    // 候補がなくても「Enterで新規作成できる」ヒントを表示します。
    dropdown.innerHTML = `<div class="tag-dropdown-item" style="color:#888;">Enter で「${escapeHtml(query)}」を新規作成</div>`;
    dropdown.style.display = 'block';
    return;
  }

  // タグ名を onclick に直接埋め込まず、IDから引く方式にします（' や \ 対策）。
  dropdown.innerHTML = filtered.map(t =>
    `<div class="tag-dropdown-item" onclick="selectTagById(${t.id})">${escapeHtml(t.name)}</div>`
  ).join('');
  dropdown.style.display = 'block';
}

// IDからタグを引いて選択します（onclick用の安全な入口）。
function selectTagById(id) {
  const t = allTags.find(t => t.id === id);
  if (t) selectTag(t.id, t.name);
}

function selectTag(id, name) {
  selectedTags.push({ id, name });
  renderSelectedTags();
  document.getElementById('tag-input').value = '';
  document.getElementById('tag-dropdown').style.display = 'none';
}

function renderSelectedTags() {
  const area  = document.getElementById('tag-input-area');
  const input = document.getElementById('tag-input');
  area.querySelectorAll('.tag-selected-badge').forEach(el => el.remove());
  selectedTags.forEach(t => {
    const badge = document.createElement('span');
    badge.className = 'tag-selected-badge';
    badge.innerHTML = `${escapeHtml(t.name)}<button type="button" onclick="removeTag(${t.id})">×</button>`;
    area.insertBefore(badge, input);
  });
}

function removeTag(id) {
  selectedTags = selectedTags.filter(t => t.id !== id);
  renderSelectedTags();
}

// ===== バッチ選択・一括削除 =====

// チェックボックスの変化を受け取り、selectedIds（Set）を更新します。
// Set は同じ値を重複して持たないため、選択管理に適しています。
function toggleSelect(id, checked) {
  if (checked) {
    selectedIds.add(id);
  } else {
    selectedIds.delete(id);
  }
  updateBulkBar();
}

// バッチ操作バーの表示・件数テキストを更新します。
function updateBulkBar() {
  const bar = document.getElementById('bulk-bar');
  const count = selectedIds.size;
  if (count > 0) {
    bar.classList.add('visible');
    document.getElementById('bulk-count').textContent = `${count}件選択中`;
  } else {
    bar.classList.remove('visible');
  }
}

// 選択をすべて解除してバッチ操作バーを非表示にします。
function clearSelection() {
  selectedIds.clear();
  // チェックボックスの見た目もリセットします。
  document.querySelectorAll('.bookmark-checkbox').forEach(cb => cb.checked = false);
  updateBulkBar();
  closeBulkTagDropdown();
  closeBulkTagRemoveDropdown();
}

// 現在表示中のブックマークをすべて選択します。
// data-id 属性からIDを取得して selectedIds に追加します。
function selectAll() {
  document.querySelectorAll('.bookmark-item[data-id]').forEach(el => {
    const id = parseInt(el.dataset.id, 10);
    selectedIds.add(id);
    const cb = el.querySelector('.bookmark-checkbox');
    if (cb) cb.checked = true;
  });
  updateBulkBar();
}

// ===== 一括タグ追加 =====

// 「タグを追加」ボタンでドロップダウンの表示・非表示を切り替えます。
function toggleBulkTagDropdown() {
  const dropdown = document.getElementById('bulk-tag-dropdown');
  if (dropdown.style.display === 'none') {
    dropdown.style.display = 'block';
    const input = document.getElementById('bulk-tag-input');
    input.value = '';
    renderBulkTagList(''); // 全タグを初期表示します
    input.focus();
  } else {
    closeBulkTagDropdown();
  }
}

// 入力に応じてタグ候補リストを絞り込みます。
function renderBulkTagList(query) {
  const list = document.getElementById('bulk-tag-list');
  const q = query.toLowerCase();

  const filtered = allTags.filter(t =>
    t.name.toLowerCase().includes(q)
  );

  if (filtered.length === 0 && query === '') {
    list.innerHTML = '<div class="bulk-tag-item" style="color:#999; cursor:default;">タグがありません</div>';
    return;
  }

  let html = filtered.map(t =>
    `<div class="bulk-tag-item" onclick="bulkAddTag(${t.id}, null)">${escapeHtml(t.name)}</div>`
  ).join('');

  // 完全一致するタグがなければ「Enterで新規作成」のヒントを出します。
  const exactMatch = allTags.some(t => t.name.toLowerCase() === q);
  if (query && !exactMatch) {
    html += `<div class="bulk-tag-item" style="color:#888; border-top:1px solid #eee;">
      Enter で「${escapeHtml(query)}」を新規作成
    </div>`;
  }

  list.innerHTML = html;
}

function closeBulkTagDropdown() {
  document.getElementById('bulk-tag-dropdown').style.display = 'none';
}

// ドロップダウン内の入力欄のイベントを設定します。
// DOMContentLoaded 後にまとめて登録します。
document.addEventListener('DOMContentLoaded', () => {
  const input = document.getElementById('bulk-tag-input');

  // 文字を入力するたびに候補を絞り込みます。
  input.addEventListener('input', () => {
    renderBulkTagList(input.value.trim());
  });

  // Enter キーで確定します（新規作成 or 既存タグを選択）。
  input.addEventListener('keydown', async (e) => {
    if (e.key !== 'Enter') return;
    e.preventDefault();

    const name = input.value.trim();
    if (!name) return;

    const existing = allTags.find(t => t.name.toLowerCase() === name.toLowerCase());
    if (existing) {
      await bulkAddTag(existing.id, null);
    } else {
      const res = await apiFetch('/api/tags', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name }),
      });
      if (!res) return; // セッション切れ
      if (!res.ok) { alert('タグの作成に失敗しました'); return; }
      const newTag = await res.json();
      allTags.push(newTag);
      await bulkAddTag(newTag.id, null);
    }
  });
});

// ===== タグ一括削除ドロップダウン =====

function toggleBulkTagRemoveDropdown() {
  const dropdown = document.getElementById('bulk-tag-remove-dropdown');
  if (dropdown.style.display === 'none') {
    dropdown.style.display = 'block';
    const input = document.getElementById('bulk-tag-remove-input');
    input.value = '';
    renderBulkTagRemoveList('');
    input.focus();
  } else {
    dropdown.style.display = 'none';
  }
}

function closeBulkTagRemoveDropdown() {
  document.getElementById('bulk-tag-remove-dropdown').style.display = 'none';
}

// 削除候補は「選択中のブックマークが持つタグ」だけ表示します。
function renderBulkTagRemoveList(query) {
  const list = document.getElementById('bulk-tag-remove-list');
  const q = query.toLowerCase();

  // 選択中ブックマークが持つタグIDを集めます（重複なし）。
  const tagIdSet = new Set();
  for (const id of selectedIds) {
    const b = bookmarkMap.get(id);
    if (b && b.tags) b.tags.forEach(t => tagIdSet.add(t.id));
  }

  // そのタグIDに対応するタグオブジェクトを allTags から引きます。
  const removable = allTags.filter(t =>
    tagIdSet.has(t.id) && t.name.toLowerCase().includes(q)
  );

  if (removable.length === 0) {
    list.innerHTML = '<div class="bulk-tag-item" style="color:#999; cursor:default;">対象タグがありません</div>';
    return;
  }

  list.innerHTML = removable.map(t =>
    `<div class="bulk-tag-item" onclick="bulkRemoveTag(${t.id})">${escapeHtml(t.name)}</div>`
  ).join('');
}

// 削除候補の入力欄イベントは DOMContentLoaded でまとめて登録します。
document.addEventListener('DOMContentLoaded', () => {
  document.getElementById('bulk-tag-remove-input').addEventListener('input', (e) => {
    renderBulkTagRemoveList(e.target.value.trim());
  });
});

// 選択中の全ブックマークから指定タグを一括削除します。
async function bulkRemoveTag(tagId) {
  closeBulkTagRemoveDropdown();

  const res = await apiFetch('/api/bookmarks/bulk/tags', {
    method: 'DELETE',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      bookmark_ids: [...selectedIds],
      tag_ids: [tagId],
    }),
  });
  if (!res) return; // セッション切れ

  if (res.ok) {
    const q = document.getElementById('search-input').value.trim();
    await loadBookmarks(q, activeTag, currentPage);
  } else {
    alert('タグの一括削除に失敗しました');
  }
}

// ドロップダウン以外をクリックしたら両方閉じます。
document.addEventListener('click', (e) => {
  if (!e.target.closest('#bulk-tag-dropdown-wrapper')) {
    closeBulkTagDropdown();
  }
  if (!e.target.closest('#bulk-tag-remove-dropdown-wrapper')) {
    closeBulkTagRemoveDropdown();
  }
});

// 選択中の全ブックマークに指定タグを一括付与します。
async function bulkAddTag(tagId, _unused) {
  closeBulkTagDropdown();

  const res = await apiFetch('/api/bookmarks/bulk/tags', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      bookmark_ids: [...selectedIds], // Set を配列に変換して送ります
      tag_ids: [tagId],
    }),
  });
  if (!res) return; // セッション切れ

  if (res.ok) {
    const q = document.getElementById('search-input').value.trim();
    await loadBookmarks(q, activeTag, currentPage);
    await loadTags(); // 新規タグが増えたときフィルターチップを更新します
  } else {
    alert('タグの一括追加に失敗しました');
  }
}

// 選択中のブックマークをまとめて削除します。
async function bulkDelete() {
  const count = selectedIds.size;
  if (!confirm(`選択中の ${count} 件を削除しますか？`)) return;

  const res = await apiFetch('/api/bookmarks', {
    method: 'DELETE',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ ids: [...selectedIds] }),
  });
  if (!res) return; // セッション切れ

  if (res.ok) {
    selectedIds.clear();
    updateBulkBar();
    const q = document.getElementById('search-input').value.trim();
    await loadBookmarks(q, activeTag, currentPage);
  } else {
    alert('一括削除に失敗しました');
  }
}

// ===== インポート・削除 =====

async function importBookmarks(input) {
  const file = input.files[0];
  if (!file) return;
  const formData = new FormData();
  formData.append('file', file);
  const res = await apiFetch('/api/import', { method: 'POST', body: formData });
  if (!res) return; // セッション切れ
  if (res.ok) {
    const result = await res.json();
    alert(`インポート完了: ${result.imported}件追加、${result.skipped}件スキップ`);
    // インポートで新しいタグが作られた場合に備えてタグ一覧も更新します。
    // （これがないとフィルターチップに新タグが表示されません）
    await loadTags();
    // 検索・タグフィルターの状態は他の操作と同様に維持します。
    const q = document.getElementById('search-input').value.trim();
    await loadBookmarks(q, activeTag);
  } else {
    alert('インポートに失敗しました');
  }
  input.value = '';
}

async function deleteBookmark(id) {
  if (!confirm('削除しますか？')) return;
  const res = await apiFetch(`/api/bookmarks/${id}`, { method: 'DELETE' });
  if (!res) return; // セッション切れ
  if (res.ok) {
    const q = document.getElementById('search-input').value.trim();
    loadBookmarks(q, activeTag, currentPage);
  } else {
    alert('削除に失敗しました');
  }
}
