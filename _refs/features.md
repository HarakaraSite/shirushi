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
| データベース | SQLite (`modernc.org/sqlite`、外部キー制約有効) |
| フロントエンド | Vanilla JS / HTML / Pico CSS（classless）+ 独自CSS |
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

> **外部キー制約**: 起動時に `PRAGMA foreign_keys = ON` を実行しており、  
> タグ削除時に `bookmark_tags` の関連行が自動削除（CASCADE）されます。
>
> **マイグレーション**: `PRAGMA table_info` で既存カラムを確認してから追加するため、  
> 既存DBへの適用も安全に行えます（冪等性）。

---

## 認証

- **シングルユーザー** のパスワード認証
- パスワードは環境変数 `SHIRUSHI_PASSWORD` で指定（ハードコード禁止）
- ログイン成功時に **セッショントークン**（`crypto/rand` で生成した64文字16進数）を発行
- トークンは `HttpOnly + SameSite=Strict` Cookie で保持
- セッションはサーバーメモリ上の `map[string]time.Time` で管理（有効期限: 24時間）
- `sync.Mutex` で並行アクセスを安全に処理

| メソッド | パス | 概要 |
|---------|------|------|
| POST | `/api/login` | ログイン（Cookie発行） |
| POST | `/api/logout` | ログアウト（Cookie削除） |

---

## ブックマーク API

| メソッド | パス | 概要 |
|---------|------|------|
| GET | `/api/bookmarks` | 一覧取得 |
| POST | `/api/bookmarks` | 新規登録 |
| PUT | `/api/bookmarks/{id}` | 更新 |
| DELETE | `/api/bookmarks/{id}` | 1件削除 |
| DELETE | `/api/bookmarks` | 一括削除 |

### GET /api/bookmarks クエリパラメータ

| パラメータ | 型 | デフォルト | 説明 |
|-----------|-----|-----------|------|
| `q` | string | - | タイトル・URL・抜粋・登録日の検索 |
| `tag` | string | - | タグ名で絞り込み。`__untagged__` でタグなし絞り込み |
| `date_from` | string | - | 登録日の開始月（YYYY-MM形式） |
| `date_to` | string | - | 登録日の終了月（YYYY-MM形式、その月末まで含む） |
| `page` | int | 1 | ページ番号（1始まり） |
| `limit` | int | 50 | 1ページあたりの取得件数（最大200） |

### 日付検索の仕様

`q` パラメータに数字のみを入力した場合、登録日と照合します。

| 入力例 | 動作 |
|--------|------|
| `202507` | 2025年7月に登録したブックマーク |
| `2025` | 2025年に登録したブックマーク |
| `go` | タイトル・URL・抜粋に「go」を含むブックマーク（通常のキーワード検索） |

日付は `created_at LIKE 'YYYY-MM%'` の文字列比較で実装しています。

### レスポンス形式

```json
{
  "bookmarks": [ { "id": 1, "url": "...", "title": "...", "tags": [...], ... } ],
  "total": 477
}
```

`total` は絞り込み条件込みの総件数。フロントエンドはこれを使ってページ数を計算します。

### クエリビルダー方式

WHERE句を動的に組み立てます（条件スライスを `AND` で結合）。  
タグ絞り込みが必要な場合のみ `INNER JOIN bookmark_tags` を追加します。

### N+1クエリ対策

タグ取得を **2クエリ固定** で処理しています。

```
クエリ①: ブックマーク一覧を取得（LIMIT / OFFSET付き）
クエリ②: 全ブックマークIDを IN句 に渡してタグを一括取得
         → Go側でマップ（O(1)）を使って各ブックマークに振り分け
```

---

## タグ API

| メソッド | パス | 概要 |
|---------|------|------|
| GET | `/api/tags` | 使用中タグ一覧（`?all=1` で未使用含む全タグ） |
| POST | `/api/tags` | タグ新規作成（同名が既存でも競合しない） |
| PUT | `/api/tags/{id}` | タグ名変更 |
| DELETE | `/api/tags/{id}` | タグ削除（bookmark_tags も CASCADE で自動削除） |
| POST | `/api/bookmarks/{id}/tags` | ブックマークにタグを追加 |
| DELETE | `/api/bookmarks/{id}/tags` | ブックマークからタグを削除 |
| POST | `/api/bookmarks/bulk/tags` | 複数ブックマークにタグを一括追加 |
| DELETE | `/api/bookmarks/bulk/tags` | 複数ブックマークからタグを一括削除 |

### GET /api/tags の仕様

| パラメータ | 動作 |
|-----------|------|
| （なし） | `INNER JOIN bookmark_tags` で使用中のタグのみ返す |
| `?all=1` | 未使用タグも含めた全タグを返す（タグ管理画面用） |

### POST /api/tags の仕様

`INSERT OR IGNORE` + `SELECT` の2ステップで処理。  
同名タグが既に存在する場合（未使用タグ含む）も競合エラーにならず正しいIDを返します。

### 一括タグ API

```json
// POST /api/bookmarks/bulk/tags
{ "bookmark_ids": [1, 2, 3], "tag_ids": [10, 11] }
```

一括追加は `INSERT OR IGNORE` でトランザクション処理（重複はスキップ）。  
一括削除は `WHERE bookmark_id IN (...) AND tag_id IN (...)` の1クエリで処理。

---

## メタデータ自動取得 API

| メソッド | パス | 概要 |
|---------|------|------|
| POST | `/api/fetch-metadata` | URLからOGタグを取得して返す |

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

---

## フロントエンド機能

### 画面構成

```
ログイン画面  →（認証成功）→  メイン画面
                               ├── ヘッダー（追加/エクスポート/インポート/タグ管理/ログアウト）
                               ├── 検索バー（キーワード・日付検索）
                               ├── タグフィルターチップ（タグなし含む）
                               ├── バッチ操作バー（選択時のみ表示）
                               ├── ブックマーク一覧（グリッド）
                               └── ページネーション
                                     モーダル（追加/編集）
                                     タグ管理モーダル
```

### グリッドレイアウト

- CSS Grid（`auto-fill, minmax(260px, 1fr)`）でレスポンシブ対応
- 画面幅に応じて自動で2〜4列に変化
- カード上部にサムネイル（160px高）、下部にタイトル・URL・タグ・日付・ボタン
- 同一行のカードは高さが揃う（`align-items: stretch`）
- フッター（日付・ボタン）は `margin-top: auto` で常に下端に配置

### ページネーション

- 1ページ50件、`?page=N` でサーバーから切り出し
- 「← 前へ」「次へ →」ボタン + ページ番号ボタン
- ページ数が多い場合は現在ページ前後2ページ＋先頭・末尾を表示し、間を `…` で省略
- ページ移動時は画面上部にスクロール
- 検索・タグフィルターが変わった場合は自動で1ページ目にリセット

### 検索

- 検索バーへの入力を **debounce（300ms）** して API を呼び出し
- タイトル・URL・抜粋の部分一致（LIKE）
- **6桁の数字**（例: `202507`）→ 2025年7月に登録したブックマークを絞り込み
- **4桁の数字**（例: `2025`）→ 2025年に登録したブックマークを絞り込み
- タグフィルターとの同時適用可能

### ブックマーク追加・編集（モーダル）

- ヘッダーの「＋ 追加」ボタン → 追加モードでモーダルを開く
- カードの「編集」ボタン → 編集モードで既存データを展開
- `editingId` 変数でモード判定（`null` = 追加、数値 = 編集）
- URLフィールドからフォーカスが外れると OG メタデータを自動取得
  - 追加モード：タイトル・著者・抜粋・画像URLをすべて入力（未入力のみ）
  - 編集モード：`image_url` が未設定の場合のみ取得

### タグ入力（オートコンプリート）

- 入力文字で使用中タグを絞り込んでドロップダウン表示
- **Enter キー**で確定（未登録タグは自動作成、未使用タグと同名でも競合しない）
- バッジ表示・×ボタンで取り消し可能

### タグフィルターチップ

- 使用中のタグをチップとして一覧表示
- 先頭に「**タグなし**」チップ（タグが付いていないブックマークを絞り込む）
- クリックで絞り込み → 選択中チップは**青ベタ**にハイライト
- 再クリックで絞り込み解除

### タグ管理モーダル

- ヘッダーの「タグ管理」ボタンで開く
- 全タグ（未使用含む）を一覧表示
- 未使用タグは「(未使用)」とグレーで区別表示
- 「削除」ボタンで確認後に削除
  - `bookmark_tags` の関連行は CASCADE で自動削除
  - フィルターチップを即時更新
  - 削除したタグがアクティブフィルター中なら自動解除

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
| 全て選択 | 現在ページの全カードを選択 |
| ＋ タグを追加 | ドロップダウンから選択（新規タグ作成も可） |
| － タグを削除 | 選択中ブックマークが持つタグのみ候補表示 |
| 一括削除 | 確認ダイアログ後に `DELETE /api/bookmarks` |
| ✕ 選択解除 | 全チェックを外してバーを閉じる |

選択状態は `Set<id>` で管理し、一覧再描画後もチェック状態を維持します。

### XSS対策

- ユーザーデータをHTMLに埋め込む際は `escapeHtml()` でエスケープ
- `onclick` 属性への JSON 直接埋め込みを避け、`bookmarkMap`（JS の `Map`）でデータ管理

---

## テスト

`net/http/httptest` と インメモリSQLite（`:memory:`）を使ったユニットテスト。

```bash
go test -v -cover ./...
```

| テスト関数 | 確認内容 |
|-----------|---------|
| `TestHandleGetBookmarks_Empty` | 0件のとき空レスポンスが返る |
| `TestHandleGetBookmarks_WithData` | 登録済みデータが正しく返る |
| `TestHandleDeleteBookmark_Success` | 存在するIDで204が返る |
| `TestHandleDeleteBookmark_NotFound` | 存在しないIDで404が返る |
| `TestHandleCreateBookmark_Success` | 正しいJSONで201とデータが返る |
| `TestHandleCreateBookmark_MissingURL` | URL未指定で400が返る |
| `TestHandleUpdateBookmark_Success` | 存在するIDで200と更新後データが返る |
| `TestHandleUpdateBookmark_NotFound` | 存在しないIDで404が返る |

---

## 未実装・今後の課題

| 項目 | 内容 |
|------|------|
| インポート時のサムネイル取得 | インポート後はOG画像が取得されないためプレースホルダーになる |

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
