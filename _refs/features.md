# Shirushi 機能詳細ドキュメント

> 更新日: 2026-06-16

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
- パスワード照合は `crypto/subtle.ConstantTimeCompare` でタイミング攻撃を防止
- ログイン成功時に **セッショントークン**（`crypto/rand` で生成した64文字16進数）を発行
- トークンは `HttpOnly + SameSite=Strict` Cookie で保持
  - `SHIRUSHI_COOKIE_SECURE=1` を設定すると `Secure` 属性も付与（CaddyでHTTPS終端する本番運用向け）
  - ログアウト時の削除Cookieもログイン時と同じ属性で発行（属性不一致による削除漏れを防止）
- セッションはサーバーメモリ上の `map[string]time.Time` で管理（有効期限: 24時間）
  - `cleanupExpiredSessions()` ゴルーチンが1時間ごとに期限切れセッションを掃除
- `sync.Mutex` で並行アクセスを安全に処理

| メソッド | パス | 概要 |
|---------|------|------|
| POST | `/api/login` | ログイン（Cookie発行） |
| POST | `/api/logout` | ログアウト（Cookie削除） |

### ログインのブルートフォース対策

- 同じIPから **15分以内に5回** ログイン失敗すると、そのIPを **15分間ロック**（HTTP `429 Too Many Requests`）
- 正しいパスワードでログインできた時点で、そのIPの失敗記録を削除
- 接続元がループバックの場合のみ `X-Forwarded-For` / `X-Real-IP` を信頼（Caddyが同一ホストで動く前提）。直接接続時は転送ヘッダーを無視し、ヘッダー偽装でロックを回避できないようにする
- 別ホスト・別コンテナのCaddy構成では、将来的に `SHIRUSHI_TRUSTED_PROXIES` 環境変数で信頼するプロキシIPを明示する案がある（未実装、詳細は `_refs/caddy-deployment.md`）

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

### SSRF対策

- `http.Transport.DialContext` をカスタム実装し、接続先IPを解決後に検査
  - ループバック・プライベートIP・リンクローカル・未指定アドレスへの接続を拒否（DNSリバインディング対策として、ホスト名解決ではなく実際に接続するIPで判定）
  - 環境変数 `SHIRUSHI_ALLOW_PRIVATE_FETCH=1` で検査を無効化できる（社内ツールなど用途限定）
- `CheckRedirect` でリダイレクトを検査（リダイレクト回数制限、許可スキーム外への遷移を拒否）
- レスポンスの `Content-Type` がHTML系でない場合は本文を読まずに中断

### サーバー側URLバリデーション

- ブックマーク作成・更新・メタデータ取得時、`validateHTTPURL()` で `net/url` パースを行い、`http` / `https` 以外のスキームを拒否
- `javascript:` / `file:` などをDBに保存させない（フロント側の `safeHref()` と二重に防御）

---

## インポート・エクスポート

| メソッド | パス | 概要 |
|---------|------|------|
| POST | `/api/import` | Netscape Bookmark形式HTMLをインポート |
| GET | `/api/export` | Netscape Bookmark形式HTMLでエクスポート |

### インポート仕様

- `multipart/form-data` でHTMLファイルを受け取る（最大 **10MB**、`http.MaxBytesReader` で制限）
- `<a href="..." add_date="...">タイトル</a>` の形式を正規表現でパース
- 説明文（`<DD>`）の抜粋取得は、対象の `<A>` から次の `<DT><A` までの範囲に限定（次のブックマークの説明文を誤って拾わないようにするため）
- タグ（`TAGS="..."` 属性）も読み込み、存在しないタグは自動作成
- 重複URL（UNIQUE制約）は `INSERT OR IGNORE` でスキップ
- インポート全体をトランザクション化し、タグ作成・紐付け時のDBエラーも確認してレスポンスに反映
- レスポンス: `{"imported": 5, "skipped": 2}`
- ※ インポート時のサムネイル（OG画像）取得は未対応

---

## サーバー全般の安全対策

- **リクエストサイズ上限**: JSON API は `http.MaxBytesReader` で1MBに制限
- **一括操作の件数上限**: 一括削除・一括タグ追加/削除のID配列は最大 **1000件**（`maxBulkIDs`）まで
- **HTTPサーバーのタイムアウト**: `http.Server` を明示的に作成し、`ReadHeaderTimeout=5s` / `ReadTimeout=15s` / `WriteTimeout=30s` / `IdleTimeout=60s` を設定（低速クライアントによるリソース占有を防止）
- **タグ紐付けAPIの存在確認**: 存在しない `bookmark_id` / `tag_id` を指定した場合は `404` を返す（以前は外部キー制約違反による `500` だった）
- **DBマイグレーション安全性**: 起動時マイグレーションで、親ブックマークが存在しない孤児 `bookmark_tags` 行を削除するクリーンアップを実行（外部キー制約を有効にした際の不整合を解消）

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
- `onclick` 属性への JSON 直接埋め込みを避け、`bookmarkMap` / `tagManagerMap`（JS の `Map`）でID経由のデータ管理
- リンクURLは `safeHref()` で `http` / `https` のみ許可（`javascript:` などを無害化）

### 認証エラーの一元処理

- `apiFetch()` ラッパーで全API呼び出しを統一し、`401` 応答時はログイン画面へ自動遷移
- `checkAuth()` は `401` 以外の異常系（サーバーエラー・不正なJSONなど）でもログイン画面へ戻す
- ログアウト時は検索条件・選択状態・開いているモーダルをすべてリセット

### ダークテーマ固定

- `<html data-theme="dark">` を指定し、OS側のライト/ダーク設定に関わらず常にダークテーマで表示（Pico CSSの `:root:not([data-theme=dark])` がデフォルトテーマを上書きする問題への対処）

### ヘッダー・ロゴ

- ヘッダーとログイン画面に `favicon.svg` を使ったロゴアイコンを表示（`.app-title` / `.app-title-icon`）

### レスポンシブ対応

- ヘッダーのボタン群（`.header-actions`）と一括操作バー（`#bulk-bar`）に `flex-wrap` を設定し、スマホ幅でもボタンがはみ出さないようにした

### パスワード入力

- ログイン画面のパスワード入力に `autocomplete="current-password"` を指定（ブラウザのパスワードマネージャー対応）

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
| ~~インポート時のサムネイル取得~~ | ~~インポート後はOG画像が取得されないためプレースホルダーになる~~ → **対応済み**（v0.2.0: バックグラウンドで自動取得） |
| ~~サムネイルの軽量化~~ | ~~サーバー側でリサイズ・ローカル保存・配信する仕組みを検討中~~ → **対応不要**（ブラウザキャッシュで実用上問題なし） |
| ~~サムネイル画像URLの相対パス~~ | ~~相対 `og:image` がShirushi自身のパスとして解釈される~~ → **対応済み**（v0.5.2: ページURL基準で絶対URLへ変換） |
| Shiori/Shirushi間のメタデータ取得差分 | `og:image` 優先のみのため、`twitter:image` 等しか持たないページでサムネイル・抜粋が欠ける場合がある（優先度: 低） |
| ~~ページサイズ切り替え~~ | ~~1ページ50件固定~~ → **対応済み**（v0.5.4: 50/100/200件、設定保存、上下ページネーション） |
| SHIRUSHI_TRUSTED_PROXIES | 別ホスト・別コンテナのCaddy構成向けに、信頼するプロキシIPを明示する環境変数（未実装） |
| Henji要約の出力言語指定 | 現在は固定の日本語指示文。将来はprovider/modelとは独立して出力言語を指定できるようにする。既定は日本語を維持し、選択可能な言語値を明示して、固定指示文・出力検証・受入テストに反映する。未対応または不正な言語を暗黙に日本語へフォールバックさせない。 |
| UIの多言語化 | 現在のUI・利用者向けエラーは日本語固定。将来は表示言語を選択できるようにし、初期値は日本語を維持する。画面文言、確認ダイアログ、認証・APIエラー、日付表示、静的文書への導線を対象にし、選択言語の保存と未翻訳文字列の検出を受入条件に含める。bookmark本文・タグなど利用者データは翻訳しない。 |

v1公開に向けた計画（README/LICENSE/デプロイ手順整備など）は `_refs/roadmap.md` を参照。

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
