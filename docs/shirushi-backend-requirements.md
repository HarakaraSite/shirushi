# shirushi 本体の拡張 API 要件（v1 完了・v2 将来項目）

Firefox 拡張（別オリジンのクライアント）と連携するための本体 API 要件。v1 の Bearer 認証は Shirushi v1.0.0 で実装・公開済みであり、以下では現行仕様として記録する。

- 対象リポジトリ: shirushi（Go）
- 関連: 認証はセッション Cookie または `SHIRUSHI_API_TOKEN` による Bearer。拡張は background 経由で通信する
- 運用: 本番 `https://shirushi.harakara.site`、開発 `http://localhost:8181`
- 前提: シングルユーザー構成

---

## v1（完了済み・現行仕様）

### Bearer トークン認証（実装・公開済み）

別オリジンの拡張から認証を通すため、Bearer トークン認証を実装した。CORS 改修は不要である（拡張は background 経由で fetch し、`host_permissions` により通信する）。

#### 実装済みの仕様

- `authMiddleware` が全 API リクエストに適用（例外は `/api/login`・`/api/logout`・静的ファイル）
- 有効なセッション Cookie または一致する Bearer トークンで認証する。`SameSite=Strict` の Cookie に依存せず、拡張から API を利用できる

#### 認証仕様

`authMiddleware` は「**セッション Cookie が有効、または Bearer トークンが一致**」のどちらかで通過させる。

- トークンは環境変数 `SHIRUSHI_API_TOKEN` から読む
- リクエストの `Authorization: Bearer <token>` ヘッダと比較
- 比較は **必ず定数時間比較**（`crypto/subtle.ConstantTimeCompare`）を使う。素朴な `==` 文字列比較はタイミング攻撃の余地があるため不可
  - 参考: https://pkg.go.dev/crypto/subtle#ConstantTimeCompare
- 既存の Cookie 認証はそのまま維持する（Web UI は引き続き Cookie で動く）

#### トークンの読み出しタイミング

- `SHIRUSHI_API_TOKEN` は **起動時に1回だけ読んでグローバル変数に保持する**
  - 起動時に読むことで、未設定ログ（下記）と自然に整合する
  - 既存の `SHIRUSHI_PASSWORD` はリクエストごとに `os.Getenv` しているが、
    Bearer トークンは「未設定なら Bearer 無効」というフラグ的な役割もあるため起動時1回が適切

#### `Authorization` ヘッダのパース

- `Authorization: Bearer <token>` の `Bearer ` プレフィックスは**大文字小文字を厳密一致**で扱う
  - 自作拡張のみが送信元のため、RFC 6750 の大小区別なし規定への準拠は不要
  - `strings.CutPrefix(header, "Bearer ")` で剥がせばよい

#### 認証判定ロジック（擬似コード）

```
authMiddleware:
  // 1. 認証不要なパスは早期 return（現状維持）
  if path == "/api/login" || path == "/api/logout" || !hasPrefix(path, "/api/"):
    通過

  // 2. セッション Cookie チェック（Web UI 用）
  if 有効なセッション Cookie がある:
    通過

  // 3. Bearer トークンチェック（拡張用）
  if SHIRUSHI_API_TOKEN が設定済み かつ
     Authorization ヘッダが "Bearer <token>" 形式 かつ
     ConstantTimeCompare(リクエストトークン, SHIRUSHI_API_TOKEN) == 1:
    通過

  // 4. いずれも通過しなければ拒否
  else:
    401 Unauthorized
```

#### トークン未設定時の挙動

- `SHIRUSHI_API_TOKEN` が空の場合は Bearer 認証を無効化（Cookie 認証のみで従来通り動作）
- 起動時にログで「Bearer 認証は無効（SHIRUSHI_API_TOKEN 未設定）」と警告を出す

#### 適用範囲

- Cookie 認証が必要な全エンドポイントは、同様に Bearer でも通る（`authMiddleware` による共通認証）
- `/api/login`・`/api/logout` は対象外（従来通り）

#### レートリミットについて（やらないことの明記）

- **Bearer 認証にはレートリミット（ブルートフォース対策）を設けない**
  - `handleLogin` には 5 回失敗で 15 分ロックの IP ベースのロックがあるが、Bearer には適用しない
  - `openssl rand -hex 32` で生成した 256bit トークンは総当たりが非現実的であり、保護不要

#### CORS について（やらないことの明記）

- **CORS ミドルウェアは追加しない**。拡張は popup から直接 fetch せず、background(event page) 経由で fetch する設計のため、`host_permissions` 宣言で CORS をバイパスできる
- 将来 Vue+REST API 化で dev server（別オリジン）から叩く段になったら、その時に CORS を追加する

#### 確認済みの受け入れ条件

```bash
# Bearer トークンで登録できること
curl -X POST https://shirushi.harakara.site/api/bookmarks \
  -H "Authorization: Bearer $SHIRUSHI_API_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"url":"https://example.com","title":"test"}'
# → 201 Created（重複なら 409）

# トークンなし・不正トークンは 401
curl -X POST https://shirushi.harakara.site/api/bookmarks \
  -H "Content-Type: application/json" \
  -d '{"url":"https://example.com"}'
# → 401 Unauthorized

# Web UI（Cookie 認証）が従来通り動くこと
```

トークン生成例: `openssl rand -hex 32`

---

## v2（将来の方針・今回は実装しない）

拡張の UX 向上と引き換えに本体改修が必要になる項目。v1 が安定してから着手する。

### 1. タグ名でのブックマーク登録対応（A1 方式、保留）

現状 `POST /api/bookmarks` の `tags` は既存タグの ID 指定のみ（`[{"id":3}]`）。これを **タグ名指定**（`[{"name":"go"}]`）でも受け付け、サーバー側で「既存マッチ or 自動作成」して紐付けるようにする。

- 既存の ID 指定方式（`[{"id":3}]`）は **残す（後方互換）**。Web UI が ID 指定で動いているため
- ID 指定と名前指定の混在も許容できると望ましい
- メリット: 拡張が `POST /api/tags` を事前に叩いて ID 解決する必要がなくなり、登録が 1 リクエストで原子的に完結する
- 注: `POST /api/tags` は既に冪等（同名なら既存タグを返す）なので、v1 ではこの機能を**拡張側**で代替している（下記 extension 要件の A2 方式）
- 現在の拡張は既存タグの選択・新規タグ作成を問題なく行えているため、この本体改修は保留する

### 2. URL 完全一致の 1 件取得 API（拡張の登録状態・カード表示用）

Firefox 拡張が、現在開いているページの登録有無を判定し、保存済みならタイトル・メモ・タグを表示できるようにする。URL で完全一致するブックマークを 1 件取得する読み取り専用 API を追加する。

```
GET /api/bookmarks/by-url?url=<percent-encoded URL>
```

#### レスポンス

- 登録済み: `200 OK` と既存の `GET /api/bookmarks/{id}` と同一の `Bookmark` JSON を返す。`id`、`url`、`title`、`excerpt`、`tags` などを含む
- 未登録: `404 Not Found`。`exists: false` のような別レスポンスは設けない
- `url` が未指定、空、または `http`/`https` の絶対 URL として不正: `400 Bad Request`
- 認証なし・不正な Bearer トークン: 既存の認証ミドルウェアにより `401 Unauthorized`

#### URL の一致規則

- クエリの `url` は、登録・更新時と同じ `validateHTTPURL` を必ず通す
- 正規化後の文字列を `bookmarks.url` と `=` で比較する。部分一致の `LIKE`、タイトル・メモ・タグの検索は行わない
- 既存の `url` 列の `UNIQUE` 制約を利用し、該当カードは高々 1 件とする。スキーマ変更や新規インデックスは不要
- この改修では末尾スラッシュの追加・除去、`utm_*` などのクエリ除去、フラグメントの除去といった新しい正規化は追加しない。保存時と同じ「ホスト名小文字化・デフォルトポート除去」のみを適用する
- **注意:** `validateHTTPURL` の正規化導入前に保存した URL は、正規化後の検索文字列と一致しない可能性がある。実装前に既存 DB の該当データを調査し、必要なら個別のデータ移行を別作業として行う

#### 実装方針

- `main.go` に `GET /api/bookmarks/by-url` を登録する。`GET /api/bookmarks/{id}` と共存できる固定パスを使う
- URL で ID を 1 件取得した後、既存の `getBookmarkByID` を再利用してタグを含む `Bookmark` を返す。既存の ID 指定 1 件取得とレスポンス内容を分岐させない
- URL、認証ヘッダ、トークンをアプリケーションログに出力しない
- `GET /api/bookmarks?q=` は保存済みブックマークのキーワード検索・一覧表示に引き続き使う。この API で代替しない

#### 受け入れ条件

```bash
# 登録済み URL はタグを含むカードを 1 件返す
curl -sS -H "Authorization: Bearer $SHIRUSHI_API_TOKEN" \
  --get --data-urlencode 'url=https://example.com/article' \
  https://shirushi.harakara.site/api/bookmarks/by-url
# → 200。JSON の url は https://example.com/article、tags は [] またはタグ配列

# 未登録 URL は 404
curl -o /dev/null -sS -w '%{http_code}\n' \
  -H "Authorization: Bearer $SHIRUSHI_API_TOKEN" \
  --get --data-urlencode 'url=https://example.com/not-saved' \
  https://shirushi.harakara.site/api/bookmarks/by-url
# → 404

# 不正 URL は 400、トークンなしは 401
```

- `https://EXAMPLE.com:443/article` の検索が、`https://example.com/article` として保存したカードを返すことを単体テストで確認する
- URL がメモやタイトルに含まれる別カードがあっても、そのカードを返さないことを単体テストで確認する
- 既存の `GET /api/bookmarks/{id}`、`GET /api/bookmarks?q=`、Cookie 認証、Bearer 認証の挙動を変えない

#### 今回は行わないこと

- 拡張のアイコン切替・保存済みカード表示 UI の実装
- 登録済み URL の全件一覧 API、クライアント側の全件キャッシュ、オフライン同期
- URL 正規化ポリシーの拡張と既存データの一括書き換え

> タグ名対応は保留する。URL 完全一致の1件取得 API は、拡張の登録状態・カード表示に必要になった時点で個別に実装する。
