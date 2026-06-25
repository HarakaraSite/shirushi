# shirushi 本体 改修要件（ブラウザ拡張対応）

Firefox 拡張（別オリジンのクライアント）から `POST /api/bookmarks` 等を叩けるようにするための本体改修要件。

- 対象リポジトリ: shirushi（Go）
- 関連: 認証は現状セッション Cookie のみ（`SameSite=Strict`）で別オリジン不可
- 運用: 本番 `https://shirushi.harakara.site`、開発 `http://localhost:8181`
- 前提: シングルユーザー構成

---

## v1（今回の改修スコープ）

### Bearer トークン認証の追加 ★これだけ

別オリジンの拡張から認証を通すため、Bearer トークン認証を追加する。CORS 改修は不要（拡張は background 経由で fetch するため、`host_permissions` で CORS をバイパスできる）。

#### 現状

- `authMiddleware` が全 API リクエストに適用（例外は `/api/login`・`/api/logout`・静的ファイル）
- 認証方式はセッション Cookie 一本。`SameSite=Strict` のため別オリジンの POST に Cookie が乗らず、拡張からは通過できない

#### 変更内容

`authMiddleware` を「**セッション Cookie が有効、または Bearer トークンが一致**」のどちらかで通すよう拡張する。

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
- 起動時にログで「Bearer 認証は無効（SHIRUSHI_API_TOKEN 未設定）」と警告を出すと親切

#### 適用範囲

- 現在 Cookie 認証が必要な全エンドポイントが、同様に Bearer でも通るようにする（`authMiddleware` の拡張なので自動的にそうなるはず）
- `/api/login`・`/api/logout` は対象外（従来通り）

#### レートリミットについて（やらないことの明記）

- **Bearer 認証にはレートリミット（ブルートフォース対策）を設けない**
  - `handleLogin` には 5 回失敗で 15 分ロックの IP ベースのロックがあるが、Bearer には適用しない
  - `openssl rand -hex 32` で生成した 256bit トークンは総当たりが非現実的であり、保護不要

#### CORS について（やらないことの明記）

- **CORS ミドルウェアは追加しない**。拡張は popup から直接 fetch せず、background(event page) 経由で fetch する設計のため、`host_permissions` 宣言で CORS をバイパスできる
- 将来 Vue+REST API 化で dev server（別オリジン）から叩く段になったら、その時に CORS を追加する

#### 受け入れ条件

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

### 1. タグ名でのブックマーク登録対応（A1 方式）

現状 `POST /api/bookmarks` の `tags` は既存タグの ID 指定のみ（`[{"id":3}]`）。これを **タグ名指定**（`[{"name":"go"}]`）でも受け付け、サーバー側で「既存マッチ or 自動作成」して紐付けるようにする。

- 既存の ID 指定方式（`[{"id":3}]`）は **残す（後方互換）**。Web UI が ID 指定で動いているため
- ID 指定と名前指定の混在も許容できると望ましい
- メリット: 拡張が `POST /api/tags` を事前に叩いて ID 解決する必要がなくなり、登録が 1 リクエストで原子的に完結する
- 注: `POST /api/tags` は既に冪等（同名なら既存タグを返す）なので、v1 ではこの機能を**拡張側**で代替している（下記 extension 要件の A2 方式）

### 2. 登録済み判定エンドポイント（アイコンバッジ用）

拡張のツールバーアイコンに「このページは登録済み」を表示する機能（v2）のために、URL 完全一致で登録有無を返す軽量エンドポイントを追加する。

```
GET /api/bookmarks/exists?url=<URL>
→ 200 OK { "exists": true, "id": 42 }
       { "exists": false }
```

- 既存の `GET /api/bookmarks?q=` は**部分一致検索**のため誤判定する。専用に **URL 完全一致**（正規化込み）で boolean を返すエンドポイントを作る
- URL 正規化（末尾スラッシュ、`utm_*` 等のクエリ除去の方針）をどうするかは別途設計
- このエンドポイントは全タブで頻繁に呼ばれるため軽量に保つ
- **注意: v0.4.0 で `validateHTTPURL` にホスト名小文字化・デフォルトポート除去の正規化を追加した。
  exists の「正規化込みで完全一致」を実装する際、それ以前に保存された URL（正規化前）との
  不一致が発生する可能性がある。v2 着手時にデータマイグレーションの要否を検討すること。**

> v2 の本体改修は「タグ名対応」と「exists エンドポイント」をまとめて 1 回で入れると効率が良い。
