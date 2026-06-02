# Shirushi 機能詳細ドキュメント

> 更新日: 2026-06-02

---

## 概要

**Shirushi** は Go + SQLite で構成されたシングルバイナリのブックマーク管理アプリです。  
サードパーティライブラリは `modernc.org/sqlite`（CGO不要）のみに絞り、  
ルーティングは Go 1.22 標準の `net/http` を使用しています。

```
実行環境: Proxmox 上の Alpine Linux (LXC)
ビルド:   CGO_ENABLED=0 go build
ポート:   8181
起動方法: SHIRUSHI_PASSWORD=xxx go run main.go
```

---

## 技術スタック

| レイヤー | 技術 |
|---------|------|
| バックエンド | Go 1.22 / `net/http` |
| データベース | SQLite (`modernc.org/sqlite`) |
| フロントエンド | Vanilla JS / HTML / Pico CSS（classless） + 独自CSS |
| 静的ファイル配信 | `//go:embed static` でバイナリに同梱 |

---

## データベーススキーマ

### bookmarks テーブル

```sql
CREATE TABLE bookmarks (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    url         TEXT NOT NULL UNIQUE,
    title       TEXT NOT NULL DEFAULT '',
    excerpt     TEXT NOT NULL DEFAULT '',
    author      TEXT NOT NULL DEFAULT '',
    public      INTEGER NOT NULL DEFAULT 0,
    has_content BOOLEAN NOT NULL DEFAULT FALSE,
    image_url   TEXT NOT NULL DEFAULT '',
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    modified_at DATETIME
);
```

### tags テーブル

```sql
CREATE TABLE tags (
    id   INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE
);
```

### bookmark_tags テーブル（中間テーブル）

```sql
CREATE TABLE bookmark_tags (
    bookmark_id INTEGER NOT NULL REFERENCES bookmarks(id) ON DELETE CASCADE,
    tag_id      INTEGER NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
    PRIMARY KEY (bookmark_id, tag_id)
);
```

> **マイグレーション**: `PRAGMA table_info` で既存カラムを確認してから追加するため、  
> 既存DBへの適用も安全に行えます（冪等性）。

---

## 認証

### 方式

- **シングルユーザー** のパスワード認証
- パスワードは環境変数 `SHIRUSHI_PASSWORD` で指定（ハードコード禁止）
- ログイン成功時に **セッショントークン**（`crypto/rand` で生成した64文字16進数）を発行
- トークンは `HttpOnly + SameSite=Strict` Cookie で保持
- セッションはサーバーメモリ上の `map[string]time.Time` で管理（有効期限: 24時間）
- `sync.Mutex` で並行アクセスを安全に処理

### 認証対象

`/api/` 以下のエンドポイントすべてに認証ミドルウェアを適用。  
`/api/login`・`/api/logout`・静的ファイルは除外。

### エンドポイント

| メソッド | パス | 概要 |
|---------|------|------|
| POST | `/api/login` | ログイン（Cookie発行） |
| POST | `/api/logout` | ログアウト（Cookie削除） |

---

## ブックマーク API

| メソッド | パス | 概要 |
|---------|------|------|
| GET | `/api/bookmarks` | 一覧取得（検索・タグ絞り込み・ページネーション対応） |
| POST | `/api/bookmarks` | 新規登録 |
| PUT | `/api/bookmarks/{id}` | 更新 |
| DELETE | `/api/bookmarks/{id}` | 1件削除 |
| DELETE | `/api/bookmarks` | 一括削除 |

### GET /api/bookmarks クエリパラメータ

| パラメータ | 型 | デフォルト | 説明 |
|-----------|-----|-----------|------|
| `q` | string | - | タイトル・URL・抜粋の部分一致検索（LIKE） |
| `tag` | string | - | タグ名で絞り込み（INNER JOIN）。`__untagged__` でタグなし絞り込み |
| `page` | int | 1 | ページ番号（1始まり） |
| `limit` | int | 50 | 1ページあたりの取得件数（最大200） |

`q` と `tag` は組み合わせ可能。

### レスポンス形式

```json
// GET /api/bookmarks レスポンス
{
  "bookmarks": [
    {
      "id": 42,
      "url": "https://example.com",
      "title": "例のサイト",
      "excerpt": "メモ",
      "author": "著者名",
      "image_url": "https://example.com/og.png",
      "created_at": "2026-05-29T12:00:00Z",
      "modified_at": null,
      "tags": [{"id": 1, "name": "go"}, {"id": 2, "name": "web"}]
    }
  ],
  "total": 477
}
```

`total` は絞り込み条件込みの総件数。フロントエンドはこれを使ってページ数を計算します。

### ページネーション

- OFFSET方式: `OFFSET = (page - 1) × limit`
- 総件数と一覧を1レスポンスで返す（別途COUNTクエリを発行）

### N+1クエリ対策

タグ取得を **2クエリ固定** で処理しています。

```
クエリ①: ブックマーク一覧を取得（LIMIT / OFFSET付き）
クエリ②: 全ブックマークIDを IN句 に渡してタグを一括取得
         → Go側でマップ（O(1)）を使って各ブックマークに振り分け
```

ループ内でタグを1件ずつ取得するN+1方式（1000件なら1001クエリ）を避けています。

---

## タグ API

| メソッド | パス | 概要 |
|---------|------|------|
| GET | `/api/tags` | 使用中タグ一覧取得（ブックマークに1件以上紐付くタグのみ） |
| POST | `/api/tags` | タグ新規作成（同名タグが既存なら既存を返す） |
| PUT | `/api/tags/{id}` | タグ名変更 |
| DELETE | `/api/tags/{id}` | タグ削除 |
| POST | `/api/bookmarks/{id}/tags` | ブックマークにタグを追加 |
| DELETE | `/api/bookmarks/{id}/tags` | ブックマークからタグを削除 |
| POST | `/api/bookmarks/bulk/tags` | 複数ブックマークにタグを一括追加 |
| DELETE | `/api/bookmarks/bulk/tags` | 複数ブックマークからタグを一括削除 |

### GET /api/tags の仕様

`INNER JOIN bookmark_tags` で**使用中のタグのみ**返します。  
ブックマークに1件も紐付いていないタグはフィルターチップ・オートコンプリートに表示されません。

### POST /api/tags の仕様

`INSERT OR IGNORE` + `SELECT` の2ステップで処理します。  
同名タグが既に存在する場合（未使用タグ含む）も競合エラーにならず、正しいIDを返します。

### 一括タグ API リクエスト例

```json
// POST /api/bookmarks/bulk/tags
{
  "bookmark_ids": [1, 2, 3],
  "tag_ids": [10, 11]
}
```

一括追加は `INSERT OR IGNORE` でトランザクション処理（重複は静かにスキップ）。  
一括削除は `WHERE bookmark_id IN (...) AND tag_id IN (...)` の1クエリで処理。

---

## メタデータ自動取得 API

| メソッド | パス | 概要 |
|---------|------|------|
| POST | `/api/fetch-metadata` | URLからOGタグを取得して返す |

URLにアクセスし、HTMLから正規表現で以下を抽出します。

| フィールド | 取得元 |
|-----------|--------|
| `title` | `<og:title>` → `<title>` の順でフォールバック |
| `excerpt` | `<og:description>` → `<meta name="description">` |
| `author` | `<meta name="author">` |
| `image_url` | `<og:image>` |

- レスポンスボディは最大 **1MB** に制限（`io.LimitReader`）
- User-Agent を設定してアクセス拒否を回避

---

## インポート・エクスポート

| メソッド | パス | 概要 |
|---------|------|------|
| POST | `/api/import` | Netscape Bookmark形式HTMLをインポート |
| GET | `/api/export` | Netscape Bookmark形式HTMLでエクスポート |

### インポート仕様

- `multipart/form-data` でHTMLファイルを受け取る
- `<a href="..." add_date="...">タイトル</a>` の形式を正規表現でパース
- タグ（`TAGS="..."` 属性）も読み込み、存在しないタグは自動作成
- 重複URL（UNIQUE制約）は `INSERT OR IGNORE` でスキップ
- レスポンス: `{"imported": 5, "skipped": 2}`
- ※ インポート時のサムネイル（OG画像）取得は未対応

### エクスポート仕様

Shiori互換の Netscape Bookmark HTML を生成して配信します。

```html
<!DOCTYPE NETSCAPE-Bookmark-file-1>
<META HTTP-EQUIV="Content-Type" CONTENT="text/html; charset=UTF-8">
<TITLE>Bookmarks</TITLE>
<H1>Bookmarks</H1>
<DL><p>
    <DT><A HREF="https://example.com" ADD_DATE="1748476800" TAGS="go,web">例のサイト</A>
    ...
</DL><p>
```

---

## フロントエンド機能

### 画面構成

```
ログイン画面  →（認証成功）→  メイン画面
                               ├── ヘッダー（追加/エクスポート/インポート/ログアウト）
                               ├── 検索バー
                               ├── タグフィルターチップ（タグなし含む）
                               ├── バッチ操作バー（選択時のみ表示）
                               ├── ブックマーク一覧（グリッド）
                               └── ページネーション
                                     モーダル（追加/編集）
```

### グリッドレイアウト

- CSS Grid（`auto-fill, minmax(260px, 1fr)`）でレスポンシブ対応
- 画面幅に応じて自動で2〜4列に変化
- カード上部にサムネイル（160px高）、下部にタイトル・URL・タグ・日付・ボタン
- 同一行のカードは高さが揃う（`align-items: stretch` デフォルト）
- フッター（日付・ボタン）は `margin-top: auto` で常に下端に配置

### ページネーション

- 1ページ50件、`?page=N` でサーバーから切り出し
- 「← 前へ」「次へ →」ボタン + ページ番号ボタン
- ページ数が多い場合は現在ページ前後2ページ＋先頭・末尾を表示し、間を `…` で省略
- ページ移動時は画面上部にスクロール
- 検索・タグフィルターが変わった場合は自動で1ページ目にリセット

### 検索

- 検索バーへの入力を **debounce（300ms）** して API を呼び出し
- サーバー側で `LIKE` による部分一致検索（タイトル・URL・抜粋）
- タグフィルターとの同時適用可能

### ブックマーク追加・編集（モーダル）

- ヘッダーの「＋ 追加」ボタン → 追加モードでモーダルを開く
- カードの「編集」ボタン → 編集モードで既存データを展開
- **同一モーダルを使い回し**、`editingId` 変数でモードを判定
  - `null` → POST /api/bookmarks
  - 数値 → PUT /api/bookmarks/:id
- オーバーレイ（背景）クリックでモーダルを閉じる

### メタデータ自動入力

- URLフィールドからフォーカスが外れると自動取得
- 追加モード：タイトル・著者・抜粋・画像URLをすべて入力（未入力フィールドのみ）
- 編集モード：`image_url` が未設定の場合のみ取得（既存テキストは上書きしない）

### タグ入力（オートコンプリート）

- 入力文字で既存タグ（使用中のもの）を絞り込んでドロップダウン表示
- **Enter キー**で確定
  - 既存タグ → 選択して追加
  - 未登録タグ → `POST /api/tags` で作成してから追加（未使用タグと同名でも競合しない）
- バッジ表示・×ボタンで取り消し可能

### タグフィルターチップ

- 使用中のタグをチップとして検索バー下に表示
- 先頭に「**タグなし**」チップ（タグが付いていないブックマークを絞り込む）
- クリックで絞り込み → 選択中チップは**青ベタ**にハイライト
- 再クリックで絞り込み解除
- 検索キーワードとの同時適用対応

### サムネイル表示

- OG画像があるサイト → カード上部全幅（160px高）にトリミング表示（`object-fit: cover`）
- OG画像がないサイト → グレーのブックマークアイコン（SVG data URI）を表示
- 画像URL読み込み失敗 → `onerror` で自動的にプレースホルダーへ切り替え
- `loading="lazy"` でスクロール時の遅延読み込み

### バッチ操作

各ブックマークカードのチェックボックス（サムネイル左上）で複数選択できます。  
1件以上選択すると青いバッチ操作バーが出現します。

| ボタン | 動作 |
|--------|------|
| ＋ タグを追加 | ドロップダウンから選択（新規タグ作成も可） |
| － タグを削除 | 選択中ブックマークが持つタグのみ候補表示 |
| 一括削除 | 確認ダイアログ後に `DELETE /api/bookmarks` |
| ✕ 選択解除 | 全チェックを外してバーを閉じる |

選択状態は `Set<id>` で管理し、一覧再描画後もチェック状態を維持します。

### XSS対策

タイトル・URL・抜粋などユーザーデータをHTMLに埋め込む際は `escapeHtml()` でエスケープしています。

```js
function escapeHtml(str) {
  return str
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;');
}
```

`onclick` 属性への `JSON.stringify` 直接埋め込みは避け、`bookmarkMap`（JS の `Map`）でデータを管理し、IDだけを属性に渡しています。

---

## テスト

`net/http/httptest` と インメモリSQLite（`:memory:`）を使ったユニットテスト。

```bash
go test -v -cover ./...
```

| テスト関数 | 確認内容 |
|-----------|---------|
| `TestHandleGetBookmarks_Empty` | 0件のとき空配列が返る |
| `TestHandleGetBookmarks_WithData` | 登録済みデータが正しく返る |
| `TestHandleDeleteBookmark_Success` | 存在するIDで204が返る |
| `TestHandleDeleteBookmark_NotFound` | 存在しないIDで404が返る |
| `TestHandleCreateBookmark_Success` | 正しいJSONで201とデータが返る |
| `TestHandleCreateBookmark_MissingURL` | URL未指定で400が返る |
| `TestHandleUpdateBookmark_Success` | 存在するIDで200と更新後データが返る |
| `TestHandleUpdateBookmark_NotFound` | 存在しないIDで404が返る |

> **注意**: テストは旧レスポンス形式（`[]Bookmark`）のままのため、ページネーション対応のレスポンス形式（`{bookmarks, total}`）への更新が必要です。

---

## 未実装・今後の課題

| 項目 | 内容 |
|------|------|
| インポート時のサムネイル取得 | インポート後はOG画像が取得されないためプレースホルダーになる |
| タグ管理画面 | 未使用タグの一覧・削除UI（APIは実装済み） |

---

## ビルド・デプロイ

```bash
# 開発実行
SHIRUSHI_PASSWORD=yourpassword go run main.go

# シングルバイナリビルド（Alpine Linux向け）
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o shirushi .

# テスト実行
go test -v -cover ./...
```

`.gitignore` で除外しているファイル：
- `shirushi.db`（データベース本体）
- `shirushi`（ビルド済みバイナリ）
- `_refs/shiori/`（参照用クローン）
