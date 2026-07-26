# Shirushi API リファレンス

English version: [api.md](api.md)

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
{
  "password": "yourpassword",
  "rememberMe": false
}
```

`rememberMe` は省略可能です。`true` にするとログイン状態を30日間維持します。
`false` または省略時のCookieはブラウザ終了時に削除され、サーバー側セッションは24時間で失効します。

**レスポンス** `200 OK`

```json
{ "status": "ok" }
```

`session` Cookie が発行されます（`HttpOnly` + `SameSite=Strict`）。
`rememberMe: true` の場合は有効期限30日、未指定または `false` の場合はセッションCookieです。
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

### 1件取得

```
GET /api/bookmarks/{id}
```

指定IDのbookmarkを、タグを含めて1件返します。Web UIは編集モーダルを開く直前にこのAPIを使い、Henjiによる非同期要約が完了済みなら、ページを再読み込みしていなくても最新Excerptをフォームへ表示します。

**レスポンス** `200 OK`

作成・更新APIと同じbookmark JSONを返します。

**エラー**

| ステータス | 内容 |
|-----------|------|
| `400` | `{id}` が整数ではない |
| `404` | 指定した ID が存在しない |

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
新規作成・既存返却いずれの場合も **`201 Created`** を返します。
クライアントは `status === 201` ではなく `res.ok`（200–299）で成否を判定してください。

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

## Henji 本文要約

Henji は任意の外部実行時依存です。ShirushiはHenjiのAPIキーや設定を管理せず、起動時に指定された実行ファイル（既定ではPATH上の`henji`）だけを使います。Henjiが利用できない環境ではWeb UIの要約ボタンは表示されません。

### 利用可否

```
GET /api/capabilities
```

**レスポンス** `200 OK`

```json
{ "henji_summary": true }
```

`henji_summary`は、起動設定で選ばれたHenji実行ファイルをPATH探索または明示パスで見つけられる場合だけ`true`です。APIキーの有効性やproviderへの到達性はこのAPIでは確認しません。

---

### 要約開始

```
POST /api/bookmarks/{id}/summary
```

保存済みブックマークのURLから静的HTMLを取得し、本文候補が十分な場合だけHenji要約ジョブを開始します。リクエストボディは不要です。モーダルに未保存のURLや「メモ・抜粋」があっても使いません。

**レスポンス** `202 Accepted`

ジョブはレスポンス後に非同期で実行されます。成功時だけ`excerpt`と`modified_at`を更新します。要約は日本語1〜5行、400 Unicode文字以内で、JSONの`summary`だけを受け付けます。

同じブックマークへの複数開始は許可され、最後に完了した要約が残ります。本文不足、外部URL取得、Henji実行、JSON検証のいずれかが失敗した場合、`excerpt`は変更されません。進捗取得、完了通知、ポーリング、再試行、再起動後のジョブ再開はありません。Henjiのstderrやproviderの詳細はレスポンスに含めません。

**その他のレスポンス**

| ステータス | 内容 |
|-----------|------|
| `204 No Content` | Henji実行ファイルを利用できない。ジョブ・URL取得・DB更新は行わない |
| `400 Bad Request` | `{id}` が整数ではない |
| `404 Not Found` | 指定したブックマークが存在しない |

本文候補は静的HTMLだけから抽出し、既存のSSRF防御を維持します。JavaScript実行、headless browser、認証済みページの取得は行いません。通常の抽出上限は40,000 Unicode文字ですが、実際のHenji入力は選定modelの`max-input-chars`から固定指示文、JSON Schema、framing、安全余裕を引いたUTF-8バイト予算に収めます。

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
