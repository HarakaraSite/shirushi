# Shiori 機能詳細リファレンス

> 参照元: https://github.com/go-shiori/shiori  
> 調査日: 2026-05-23

---

## 1. APIエンドポイント全一覧

### 認証（Auth）

| メソッド | パス | 概要 | 認証要件 |
|---------|------|------|--------|
| POST | `/api/v1/auth/login` | ログイン（ユーザー名＆パスワード） | 不要 |
| POST | `/api/v1/auth/refresh` | トークンのリフレッシュ | トークン必須 |
| GET | `/api/v1/auth/me` | 現在のログインユーザー情報取得 | トークン必須 |
| PATCH | `/api/v1/auth/account` | 自分のユーザー情報更新（パスワード変更含む） | トークン必須 |
| POST | `/api/v1/auth/logout` | ログアウト | トークン必須 |

**認証方式:**
- JWT トークン形式
- `Authorization: Bearer {token}` ヘッダー、またはクッキー `token={value}`
- 通常トークン: 1時間 / Remember Me: 30日

### アカウント管理（Accounts）

| メソッド | パス | 概要 | 認証要件 |
|---------|------|------|--------|
| GET | `/api/v1/accounts` | アカウント一覧取得 | 管理者必須 |
| POST | `/api/v1/accounts` | 新規アカウント作成 | 管理者必須 |
| PATCH | `/api/v1/accounts/{id}` | アカウント更新 | 管理者必須 |
| DELETE | `/api/v1/accounts/{id}` | アカウント削除 | 管理者必須 |

### タグ管理（Tags）

| メソッド | パス | 概要 | 認証要件 |
|---------|------|------|--------|
| GET | `/api/v1/tags` | タグ一覧取得 | ユーザー必須 |
| GET | `/api/v1/tags/{id}` | タグ詳細取得 | ユーザー必須 |
| POST | `/api/v1/tags` | 新規タグ作成 | ユーザー必須 |
| PUT | `/api/v1/tags/{id}` | タグ更新 | ユーザー必須 |
| DELETE | `/api/v1/tags/{id}` | タグ削除 | 管理者必須 |

クエリパラメータ:
- `with_bookmark_count=true` - 各タグのブックマーク数を含める
- `bookmark_id={id}` - 特定ブックマークのタグのみ取得
- `search={keyword}` - タグ名で検索

### ブックマーク（Bookmarks）

| メソッド | パス | 概要 | 認証要件 |
|---------|------|------|--------|
| GET | `/api/v1/bookmarks/{id}/readable` | 読み込み版コンテンツ取得 | ユーザー必須 |
| GET | `/api/v1/bookmarks/{id}/tags` | ブックマークのタグ一覧取得 | ユーザー必須 |
| POST | `/api/v1/bookmarks/{id}/tags` | ブックマークにタグ追加 | 管理者必須 |
| DELETE | `/api/v1/bookmarks/{id}/tags` | ブックマークからタグ削除 | ユーザー必須 |
| PUT | `/api/v1/bookmarks/cache` | キャッシュ更新（アーカイブ作成等） | 管理者必須 |
| PUT | `/api/v1/bookmarks/bulk/tags` | 複数ブックマークのタグ一括更新 | ユーザー必須 |

### コンテンツ配信

| メソッド | パス | 概要 |
|---------|------|------|
| GET | `/bookmark/{id}/content` | HTMLコンテンツのWebページ表示 |
| GET | `/bookmark/{id}/archive` | アーカイブページ表示 |
| GET | `/bookmark/{id}/archive/file/{path...}` | アーカイブ内ファイル取得 |
| GET | `/bookmark/{id}/thumb` | サムネイル画像取得 |
| GET | `/bookmark/{id}/ebook` | EPUB形式ダウンロード |

### システム

| メソッド | パス | 概要 |
|---------|------|------|
| GET | `/api/v1/system/info` | バージョン・DB種別・OS情報取得（管理者必須） |
| GET | `/system/liveness` | ヘルスチェック（認証不要） |

---

## 2. CLIコマンド全一覧

### グローバルフラグ

```
--portable                  ポータブルモード実行
--storage-directory {path}  データ保存ディレクトリ指定
--log-level {level}         ログレベル（debug/info/warn/error）
```

### ブックマーク操作

#### `add <URL>` - ブックマーク追加

```
-i, --title {title}       カスタムタイトル
-e, --excerpt {excerpt}   説明文
-t, --tags {tag1,tag2}    カンマ区切りのタグ
-o, --offline             オフラインで保存（コンテンツ取得なし）
-a, --no-archival         アーカイブ作成をスキップ
    --log-archival        アーカイブプロセスをログに記録
```

#### `print [indices]` (別名: `list`, `ls`) - 一覧表示

```
-j, --json                JSON形式で出力
-l, --latest              最新順でソート
-i, --index-only          インデックスのみ表示
-s, --search {keyword}    キーワードで検索
-t, --tags {tag1,tag2}    タグでフィルタ
-e, --exclude-tags {tag}  除外タグを指定
```

インデックス指定例: `5 6 23` / `100-200` / `1-3 7 9`

#### `update [indices]` - ブックマーク更新

```
-u, --url {url}           新しいURL
-i, --title {title}       新しいタイトル
-e, --excerpt {excerpt}   新しい説明文
-t, --tags {tag1,tag2}    タグ更新（-tagで削除）
-o, --offline             オフラインで更新
-y, --yes                 全更新時の確認をスキップ
    --keep-metadata       既存のメタデータ保持
-a, --no-archival         アーカイブ更新をスキップ
```

#### `delete [indices]` (別名: `rm`) - ブックマーク削除

```
-y, --yes  全削除時の確認をスキップ
```

#### `open [indices]` - ブックマークをブラウザで開く

```
-y, --yes                 全て開く場合の確認をスキップ
-a, --archive             アーカイブ版を開く
-p, --archive-port {port} アーカイブサーバーのポート番号
-t, --text-cache          テキストキャッシュをターミナルで表示
```

### インポート/エクスポート

#### `import <file>` - Netscape Bookmark形式HTMLからインポート

```
-t, --generate-tag  フォルダをタグとして自動生成
```

#### `export <file>` - Netscape Bookmark形式HTMLにエクスポート

#### `pocket <file>` - Pocketデータからインポート（.html / .csv）

### メンテナンス

#### `check [indices]` - ブックマークのリンク有効性チェック

```
-y, --yes  全チェック時の確認をスキップ
```

### サーバー起動

#### `server` - Webサーバー起動

```
-p, --port {port}           サーバーポート（デフォルト: 8080）
-a, --address {address}     バインドするアドレス
-r, --webroot {path}        Webルートパス（デフォルト: /）
    --access-log            アクセスログを出力
    --serve-web-ui          WebUIを配信（デフォルト: true）
    --secret-key {key}      セッション暗号化用の秘密鍵
```

---

## 3. データモデル詳細

### bookmarkテーブル（最終スキーマ）

| カラム | 型 | 説明 |
|-------|-----|------|
| `id` | INTEGER PRIMARY KEY | ブックマークID |
| `url` | TEXT NOT NULL UNIQUE | ブックマークURL |
| `title` | TEXT NOT NULL | タイトル |
| `excerpt` | TEXT DEFAULT '' | 説明文・抜粋 |
| `author` | TEXT DEFAULT '' | ページの著者 |
| `public` | INTEGER DEFAULT 0 | 公開フラグ（0=非公開, 1=公開） |
| `has_content` | BOOLEAN DEFAULT FALSE | コンテンツ取得済みフラグ |
| `image_url` | TEXT | OGイメージURL |
| `created_at` | DATETIME | 作成日時 |
| `modified_at` | DATETIME | 最終更新日時 |

### accountテーブル

| カラム | 型 | 説明 |
|-------|-----|------|
| `id` | INTEGER PRIMARY KEY | アカウントID |
| `username` | TEXT NOT NULL UNIQUE | ユーザー名 |
| `password` | BINARY(80) NOT NULL | パスワード（ハッシュ化） |
| `owner` | INTEGER DEFAULT 0 | 管理者フラグ（0=一般, 1=管理者） |
| `config` | JSON DEFAULT '{}' | ユーザー設定 |

**config JSONのスキーマ:**

```json
{
  "show_id": false,
  "list_mode": false,
  "hide_thumbnail": false,
  "hide_excerpt": false,
  "theme": "light",
  "keep_metadata": false,
  "use_archive": false,
  "create_ebook": false,
  "make_public": false
}
```

### tagテーブル

| カラム | 型 | 説明 |
|-------|-----|------|
| `id` | INTEGER PRIMARY KEY | タグID |
| `name` | TEXT NOT NULL UNIQUE | タグ名 |

### bookmark_tagテーブル（中間テーブル）

| カラム | 型 | 説明 |
|-------|-----|------|
| `bookmark_id` | INTEGER FK | ブックマークID |
| `tag_id` | INTEGER FK | タグID |

複合主キー: `(bookmark_id, tag_id)`

### bookmark_content（SQLite FTS5仮想テーブル）

フルテキスト検索用。カラム: `title`, `content`, `html`, `docid`

---

## 4. 認証・ユーザー管理

### ユーザータイプ

**管理者 (owner=1)**
- 全API操作可能
- アカウント管理（作成・削除）
- タグ削除
- キャッシュ更新
- 他ユーザーの情報変更

**一般ユーザー (owner=0)**
- 自分のブックマーク操作
- タグ管理（関連するもの）
- 自身のアカウント情報変更
- 読み取り操作全般

### 認証フロー

1. `POST /api/v1/auth/login` でJWTトークン取得
2. 以降のリクエストに `Authorization: Bearer {token}` を付与
3. 期限切れ前に `POST /api/v1/auth/refresh` でリフレッシュ
4. `POST /api/v1/auth/logout` でクッキーをクリア（サーバー側セッションなし）

---

## 5. ブックマーク取得・アーカイブ機能

### コンテンツ取得プロセス

ブックマーク追加・更新時に自動実行:

1. URLからHTMLをダウンロード
2. メタデータ抽出（タイトル・著者・OG画像・説明文）
3. テキスト版・HTML版を生成
4. サムネイル・アーカイブ・EPUBを作成

### アーカイブ構造

```
{data_dir}/
├── thumb/{id}           # サムネイル画像
├── archive/{id}/        # オフラインアーカイブ
│   ├── index.html
│   └── ...
└── ebook/{id}.epub      # EPUB形式
```

### キャッシュ更新API

```
PUT /api/v1/bookmarks/cache
{
  "ids": [1, 2, 3],
  "keep_metadata": false,
  "create_archive": true,
  "create_ebook": false,
  "skip_exist": false
}
```

- 最大10件の並列処理
- エラーが発生したブックマークはリストで返却

---

## 6. タグ機能詳細

### CLI でのタグ操作

```bash
# タグ付きで追加
shiori add "https://example.com" -t "go,programming"

# タグを追加
shiori update 1 -t "newtag"

# タグを削除（マイナス記号）
shiori update 1 -t "-oldtag"

# タグでフィルタして表示
shiori print -t "go"

# 特定タグを除外して表示
shiori print -e "excludetag"
```

### タグ取得クエリパラメータ

```
GET /api/v1/tags?with_bookmark_count=true  # ブックマーク数を含む
GET /api/v1/tags?bookmark_id=5             # 特定ブックマークのタグ
GET /api/v1/tags?search=go                 # タグ名で検索
```

### 複数ブックマークへのタグ一括付与

```
PUT /api/v1/bookmarks/bulk/tags
{
  "bookmark_ids": [1, 2, 3],
  "tag_ids": [10, 11]
}
```

---

## 7. データベース対応

| DB | バージョン |
|---|---|
| SQLite | 3.x（デフォルト） |
| MySQL | 5.7+ |
| PostgreSQL | 9.5+ |

---

## Shirushiへの実装候補（参考）

| 機能 | 難易度 | 優先度 |
|-----|--------|--------|
| タグ機能（tag / bookmark_tag テーブル） | 中 | 高 |
| ブラウザからのメタデータ自動取得（タイトル・OG画像） | 中 | 高 |
| インポート/エクスポート（Netscape形式） | 中 | 中 |
| 全文検索 | 中 | 中 |
| パスワード認証・ログイン | 高 | 中 |
| アーカイブ（オフラインコピー） | 高 | 低 |
| EPUB生成 | 高 | 低 |
| 複数ユーザー管理 | 高 | 低 |
