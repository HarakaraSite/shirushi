# Shirushi API リファレンス

ベース URL: `http://localhost:8181`（`SHIRUSHI_ADDR` で変更可）

すべての API は JSON を返します（`Content-Type: application/json`）。
`/api/login` と `/api/logout` 以外は認証が必要です。
未認証リクエストには `401 Unauthorized` を返します。

### 認証方式

| 方式 | 用途 | 設定 |
|------|------|------|
| セッション Cookie | Web UI | `POST /api/login` でログイン |
| Bearer トークン | ブラウザ拡張など Cookie を使えないクライアント | 環境変数 `SHIRUSHI_API_TOKEN` を設定して起動 |

どちらか一方が有効であればリクエストは通過します。

**Bearer トークンの使い方**

```
Authorization: Bearer <SHIRUSHI_API_TOKEN の値>
```

- `SHIRUSHI_API_TOKEN` が未設定の場合、Bearer 認証は無効（Cookie のみ有効）
- 起動時に `"SHIRUSHI_API_TOKEN が設定されていません"` という警告ログが出る
- トークンは `openssl rand -hex 32` で生成した 256bit 文字列を推奨
- `Bearer ` プレフィックスは大文字小文字を厳密に区別（`bearer ` は不一致）

---

## 認証 API

### ログイン

```
POST /api/login
```

**リクエスト**

```json
{ "password": "yourpassword" }
```

**レスポンス** `200 OK`

```json
{ "status": "ok" }
```

`session` Cookie が発行されます（有効期限 24 時間、`HttpOnly` + `SameSite=Strict`）。
`SHIRUSHI_COOKIE_SECURE=1` の場合は `Secure` 属性も付与されます。

**エラー**

| ステータス | 内容 |
|-----------|------|
| `401` | パスワードが違う |
| `429` | 同一 IP から 15 分以内に 5 回失敗（15 分ロック） |

---

### ログアウト

```
POST /api/logout
```

**レスポンス** `200 OK`

```json
{ "status": "ok" }
```

`session` Cookie を削除します。リクエストボディは不要です。

---

## ブックマーク

### 一覧取得

```
GET /api/bookmarks
```

**クエリパラメータ**

| パラメータ | 型 | デフォルト | 説明 |
|-----------|-----|-----------|------|
| `q` | string | — | タイトル・URL・抜粋の部分一致検索。数字のみの場合は日付検索（後述） |
| `tag` | string | — | タグ名で絞り込み。`__untagged__` でタグなしのみ表示 |
| `date_from` | string | — | 登録日の開始月（`YYYY-MM` 形式） |
| `date_to` | string | — | 登録日の終了月（`YYYY-MM` 形式、その月末まで含む） |
| `page` | int | `1` | ページ番号（1 始まり） |
| `limit` | int | `50` | 1 ページあたりの件数（最大 `200`） |

**日付検索（`q` パラメータ）**

| 入力例 | 動作 |
|--------|------|
| `202507` | 2025 年 7 月に登録したブックマーク |
| `2025` | 2025 年に登録したブックマーク |

**レスポンス** `200 OK`

```json
{
  "bookmarks": [
    {
      "id": 1,
      "url": "https://example.com",
      "title": "Example",
      "excerpt": "説明文",
      "author": "",
      "public": 0,
      "has_content": false,
      "image_url": "https://example.com/og.png",
      "created_at": "2025-07-01T12:00:00Z",
      "modified_at": null,
      "tags": [
        { "id": 3, "name": "go" }
      ]
    }
  ],
  "total": 477
}
```

`total` は絞り込み条件込みの総件数です。ページネーションのページ数計算に使います。
タグなしのブックマークは `"tags": []` になります（`null` にはなりません）。

---

### 作成

```
POST /api/bookmarks
```

**リクエスト**

```json
{
  "url": "https://example.com",
  "title": "Example",
  "excerpt": "説明文",
  "author": "",
  "image_url": "",
  "tags": [{ "id": 3 }]
}
```

`tags` は省略可能です。省略した場合はタグなしで作成されます。
`url` は `http` または `https` で始まる必要があります。

**レスポンス** `201 Created`

作成されたブックマークを返します（`created_at` / `modified_at` は DB の値）。

```json
{
  "id": 42,
  "url": "https://example.com",
  "title": "Example",
  "excerpt": "説明文",
  "author": "",
  "public": 0,
  "has_content": false,
  "image_url": "",
  "created_at": "2025-07-01T12:00:00Z",
  "modified_at": null,
  "tags": [{ "id": 3, "name": "go" }]
}
```

**エラー**

| ステータス | 内容 |
|-----------|------|
| `400` | `url` が空、または `http`/`https` 以外のスキーム |
| `409` | 同じ URL が既に登録されている |

---

### 更新

```
PUT /api/bookmarks/{id}
```

**リクエスト**

```json
{
  "url": "https://example.com",
  "title": "新しいタイトル",
  "excerpt": "新しい説明",
  "author": "",
  "image_url": "",
  "tags": [{ "id": 3 }, { "id": 5 }]
}
```

`tags` を**省略**するとタグは変更されません。
`tags` を**空配列 `[]`** にすると全タグが削除されます。

**レスポンス** `200 OK`

更新後のブックマークを返します（`modified_at` に更新日時が入ります）。

**エラー**

| ステータス | 内容 |
|-----------|------|
| `400` | `url` が不正 |
| `404` | 指定した ID が存在しない |
| `409` | 別のブックマークが同じ URL を使っている |

---

### 削除（1 件）

```
DELETE /api/bookmarks/{id}
```

**レスポンス** `204 No Content`

**エラー**

| ステータス | 内容 |
|-----------|------|
| `404` | 指定した ID が存在しない |

---

### 一括削除

```
DELETE /api/bookmarks
```

**リクエスト**

```json
{ "ids": [1, 2, 3] }
```

最大 1000 件まで。

**レスポンス** `200 OK`

```json
{ "deleted": 3 }
```

---

## タグ

### 一覧取得

```
GET /api/tags
```

**クエリパラメータ**

| パラメータ | 説明 |
|-----------|------|
| `all=1` | 未使用タグも含めた全タグを返す（省略時はブックマークに使われているタグのみ） |

**レスポンス** `200 OK`

```json
[
  { "id": 1, "name": "go" },
  { "id": 2, "name": "tech" }
]
```

タグが 0 件のときは `[]` を返します（`null` にはなりません）。

---

### 作成

```
POST /api/tags
```

**リクエスト**

```json
{ "name": "newtagname" }
```

同名タグが既に存在する場合は新規作成せず既存タグを返します（エラーになりません）。

**レスポンス** `201 Created`

```json
{ "id": 5, "name": "newtagname" }
```

---

### 更新（タグ名変更）

```
PUT /api/tags/{id}
```

**リクエスト**

```json
{ "name": "renamed" }
```

**レスポンス** `200 OK`

```json
{ "id": 5, "name": "renamed" }
```

**エラー**

| ステータス | 内容 |
|-----------|------|
| `404` | 指定した ID が存在しない |

---

### 削除

```
DELETE /api/tags/{id}
```

タグに紐付いた `bookmark_tags` は CASCADE で自動削除されます。

**レスポンス** `204 No Content`

**エラー**

| ステータス | 内容 |
|-----------|------|
| `404` | 指定した ID が存在しない |

---

## ブックマークとタグの紐付け

### タグを追加（1 件）

```
POST /api/bookmarks/{id}/tags
```

**リクエスト**

```json
{ "tag_id": 3 }
```

**レスポンス** `204 No Content`

既に紐付いている場合も `204` を返します（重複追加は無視されます）。

**エラー**

| ステータス | 内容 |
|-----------|------|
| `404` | ブックマークまたはタグが存在しない |

---

### タグを削除（1 件）

```
DELETE /api/bookmarks/{id}/tags
```

**リクエスト**

```json
{ "tag_id": 3 }
```

**レスポンス** `204 No Content`

---

### タグを一括追加

```
POST /api/bookmarks/bulk/tags
```

複数のブックマークに複数のタグをまとめて付与します。

**リクエスト**

```json
{
  "bookmark_ids": [1, 2, 3],
  "tag_ids": [10, 11]
}
```

- `bookmark_ids` / `tag_ids` それぞれ最大 1000 件
- `bookmark_ids × tag_ids` の組み合わせ数は最大 5000
- 既に紐付いているペアはスキップされます

**レスポンス** `204 No Content`

**エラー**

| ステータス | 内容 |
|-----------|------|
| `404` | 指定した bookmark_id または tag_id の一部が存在しない |

---

### タグを一括削除

```
DELETE /api/bookmarks/bulk/tags
```

**リクエスト**

```json
{
  "bookmark_ids": [1, 2, 3],
  "tag_ids": [10, 11]
}
```

**レスポンス** `204 No Content`

---

## メタデータ取得

```
POST /api/fetch-metadata
```

指定した URL の OGP メタデータを取得して返します。ブックマークの保存は行いません。

**リクエスト**

```json
{ "url": "https://example.com" }
```

**レスポンス** `200 OK`

```json
{
  "title": "Example Domain",
  "excerpt": "ページの説明文",
  "author": "",
  "image_url": "https://example.com/og.png"
}
```

| フィールド | 取得元 |
|-----------|--------|
| `title` | `og:title` → `<title>` の順にフォールバック |
| `excerpt` | `og:description` → `meta[name=description]` |
| `author` | `og:author` → `meta[name=author]` |
| `image_url` | `og:image` |

**エラー**

| ステータス | 内容 |
|-----------|------|
| `400` | URL が空、または `http`/`https` 以外 |
| `502` | 外部 URL へのアクセス失敗（タイムアウト・SSRF ブロック・非 HTML レスポンスなど） |

---

## インポート・エクスポート

### エクスポート

```
GET /api/export
```

全ブックマークを Netscape Bookmark 形式（Chrome / Firefox のエクスポート形式）で返します。

**レスポンス** `200 OK`

`Content-Type: text/html` のファイルダウンロード（`shirushi-bookmarks.html`）。

---

### インポート

```
POST /api/import
```

Netscape Bookmark 形式の HTML ファイルをアップロードして登録します。

**リクエスト**

`multipart/form-data` で `file` フィールドに HTML ファイルを添付します（最大 10 MB）。

```bash
curl -b 'session=<token>' \
     -F 'file=@bookmarks.html' \
     http://localhost:8181/api/import
```

**レスポンス** `200 OK`

```json
{ "imported": 42, "skipped": 5 }
```

`skipped` は重複 URL でスキップされた件数です。
インポート後、新規登録されたブックマークのサムネイル（OG 画像）をバックグラウンドで順次取得します（レスポンス返却後に非同期で実行）。

---

## 共通仕様

### リクエストサイズ上限

| API | 上限 |
|-----|------|
| JSON API（ログイン・ブックマーク・タグ等） | 1 MB |
| インポート（`/api/import`） | 10 MB |

### 共通エラー

| ステータス | 内容 |
|-----------|------|
| `400 Bad Request` | リクエスト形式の誤り・必須フィールド欠如・バリデーション失敗 |
| `401 Unauthorized` | セッション Cookie がない・期限切れ、または Bearer トークンが不正 |
| `404 Not Found` | 指定した ID が存在しない |
| `409 Conflict` | URL の重複 |
| `429 Too Many Requests` | ログイン失敗によるロック |
| `500 Internal Server Error` | サーバー内部エラー |
| `502 Bad Gateway` | 外部 URL へのアクセス失敗（メタデータ取得のみ） |
