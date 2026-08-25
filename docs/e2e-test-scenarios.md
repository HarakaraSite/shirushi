# Shirushi 簡易E2Eテストシナリオ

ブックマーク管理アプリ **Shirushi** の「実際の操作フロー全体」を検証するためのE2Eテストシナリオ集です。
ユニットテスト（`main_test.go`）が各ハンドラ単体の挙動を検証しているのに対し、ここでは
**ログイン → 操作 → 確認 → ログアウト** のような一連の流れを通しで確認します。

---

## このドキュメントの使い方

### 対象と前提

- アプリは **シングルバイナリ** で、ポート **8181** で起動します。
- 認証は **セッションCookie**（`session`、`HttpOnly`、`SameSite=Strict`）です。
  curl では Cookie を保存・送信するため **クッキーjar（`-c` / `-b`）** を使います。
- すべてのAPIは `/api/...` 配下にあり、`/api/login` と `/api/logout` 以外は **認証必須**（未認証は 401）です。

### 起動方法

```bash
# パスワードを環境変数で渡して起動（ローカル開発はHTTPなので Secure Cookie は無効のまま）
SHIRUSHI_PASSWORD='test-password' ./shirushi
# => ":8181" で待ち受け開始
```

> ローカルでHTTP越しにテストする場合、`SHIRUSHI_COOKIE_SECURE` は **設定しない**でください。
> 設定すると Cookie に Secure 属性が付き、HTTP では Cookie が送られずログインが維持できません。

### 共通の準備（curl）

各シナリオの冒頭で以下を前提とします。コピペでそのまま使えます。

```bash
export BASE_URL="http://localhost:8181"
export PASSWORD="test-password"   # 起動時の SHIRUSHI_PASSWORD と一致させる
COOKIE="$(mktemp)"                # セッションCookieの保存先（シナリオごとに新規が望ましい）
```

ログインしてCookieを取得する基本形:

```bash
curl -s -c "$COOKIE" -X POST "$BASE_URL/api/login" \
  -H 'Content-Type: application/json' \
  -d "{\"password\":\"$PASSWORD\"}"
# => {"status":"ok"}  （Cookie jar に session が保存される）
```

以降の認証付きリクエストは `-b "$COOKIE"` を付けます。

### テストの方針

- **ハッピーパス（正常系）中心**。重要な異常系（認証失敗・重複）も含みます。
- 各シナリオは **独立実行可能**。テスト開始時に必要なデータを自分で作り、できれば後始末まで行います。
- 検証は HTTP ステータスコード／JSONレスポンス／件数で行います。JSONの確認には `jq` があると便利です。

### 主要なAPIエンドポイント早見表

| メソッド・パス | 用途 |
| --- | --- |
| `POST /api/login` | ログイン（`{"password":"..."}`） |
| `POST /api/logout` | ログアウト |
| `GET /api/bookmarks` | 一覧（`?q=` `?tag=` `?date_from=` `?date_to=` `?page=` `?limit=`） |
| `GET /api/bookmarks/by-url` | URL完全一致で1件取得（`?url=`、拡張の登録済み判定向け） |
| `GET /api/bookmarks/{id}` | 1件取得（編集前の最新値読込） |
| `POST /api/bookmarks` | 登録（`{"url","title","excerpt","tags":[{"id":N}]}`） |
| `PUT /api/bookmarks/{id}` | 更新 |
| `DELETE /api/bookmarks/{id}` | 単体削除（204） |
| `DELETE /api/bookmarks` | 一括削除（`{"ids":[...]}` → `{"deleted":N}`） |
| `GET /api/tags` | タグ一覧（`?all=1` で未使用含む全件） |
| `POST /api/tags` | タグ作成（`{"name":"..."}`、同名は既存を返す） |
| `PUT /api/tags/{id}` | タグ名更新 |
| `DELETE /api/tags/{id}` | タグ削除（204、紐付けはCASCADE削除） |
| `POST /api/bookmarks/{id}/tags` | 単体タグ付与（`{"tag_id":N}` → 204） |
| `DELETE /api/bookmarks/{id}/tags` | 単体タグ解除（`{"tag_id":N}` → 204） |
| `POST /api/bookmarks/bulk/tags` | 一括タグ付与（`{"bookmark_ids":[...],"tag_ids":[...]}` → 204） |
| `DELETE /api/bookmarks/bulk/tags` | 一括タグ解除（204） |
| `GET /api/export` | Netscape Bookmark形式でエクスポート |
| `POST /api/import` | インポート（multipart `file=@...`） |
| `POST /api/fetch-metadata` | OGPメタデータ取得（`{"url":"..."}`） |
| `GET /api/capabilities` | Henji本文要約の利用可否（`{"henji_summary":true|false}`） |
| `POST /api/bookmarks/{id}/summary` | 保存済みブックマークの本文要約を非同期開始（202） |

---

## シナリオ1: メインフロー（ログイン→登録→タグ付け→検索→ログアウト）【必須・最優先】

アプリの中核となる一連の流れを通しで確認します。

### 前提条件

- アプリが `SHIRUSHI_PASSWORD='test-password'` で起動済み。
- 共通の準備（`BASE_URL` / `PASSWORD` / `COOKIE`）が済んでいる。

### 操作手順

1. 正しいパスワードでログインし、セッションCookieを取得する。
2. タグ「e2e-main」を作成し、返ってきた `id` を控える。
3. ブックマークを作成時にそのタグを紐付けて登録する。
4. キーワード検索で登録したブックマークが見つかることを確認する。
5. タグフィルターで同じブックマークが見つかることを確認する。
6. ログアウトする。
7. ログアウト後はブックマーク一覧が **401** になることを確認する。

### 期待結果

- ログイン: `{"status":"ok"}`、Cookie jar に `session` が保存される。
- タグ作成: `201`、`{"id":N,"name":"e2e-main"}`。
- ブックマーク登録: `201`、レスポンスの `tags` に「e2e-main」が含まれる。
- キーワード検索 / タグフィルター: `total >= 1`、対象URLが結果に含まれる。
- ログアウト: `{"status":"ok"}`、以降の `/api/bookmarks` は `401 Unauthorized`。

### curl 例

```bash
export BASE_URL="http://localhost:8181"
export PASSWORD="test-password"
COOKIE="$(mktemp)"

# 1. ログイン
curl -s -c "$COOKIE" -X POST "$BASE_URL/api/login" \
  -H 'Content-Type: application/json' -d "{\"password\":\"$PASSWORD\"}"

# 2. タグ作成（id を控える）
TAG_ID=$(curl -s -b "$COOKIE" -X POST "$BASE_URL/api/tags" \
  -H 'Content-Type: application/json' -d '{"name":"e2e-main"}' | jq .id)
echo "TAG_ID=$TAG_ID"

# 3. タグ付きでブックマーク登録
curl -s -b "$COOKIE" -X POST "$BASE_URL/api/bookmarks" \
  -H 'Content-Type: application/json' \
  -d "{\"url\":\"https://example.com/e2e-main\",\"title\":\"E2E メインフロー記事\",\"excerpt\":\"検証用メモ\",\"tags\":[{\"id\":$TAG_ID}]}"
# => 201, tags に e2e-main が含まれる

# 4. キーワード検索（タイトルの一部で）
curl -s -b "$COOKIE" "$BASE_URL/api/bookmarks?q=メインフロー" | jq '{total, urls: [.bookmarks[].url]}'

# 5. タグフィルター
curl -s -b "$COOKIE" "$BASE_URL/api/bookmarks?tag=e2e-main" | jq '{total, urls: [.bookmarks[].url]}'

# 6. ログアウト
curl -s -b "$COOKIE" -X POST "$BASE_URL/api/logout"

# 7. ログアウト後は 401
curl -s -o /dev/null -w "%{http_code}\n" -b "$COOKIE" "$BASE_URL/api/bookmarks"
# => 401
```

---

## シナリオ2: インポートと重複スキップ【必須】

Netscape Bookmark形式のHTMLをインポートし、件数とタグ生成、再インポート時のスキップを確認します。

### 前提条件

- ログイン済み（`$COOKIE` 取得済み）。
- 一意なURLを使うため、テスト用ドメインを使う（既存DBと衝突しないURLにする）。

### 操作手順

1. インポート用のHTMLファイルを2件分のブックマークで作成する（`TAGS=` と `ADD_DATE=` を含める）。
2. `POST /api/import` でアップロードする。
3. レスポンスの `imported` / `skipped` を確認する。
4. 同じファイルをもう一度インポートする。
5. 2回目は全件 `skipped` になることを確認する。
6. インポートで生成されたタグが一覧に出ることを確認する。

### 期待結果

- 1回目: `{"imported":2,"skipped":0}`。
- 2回目（同一URL）: `{"imported":0,"skipped":2}`（URLのUNIQUE制約で重複スキップ）。
- `GET /api/tags?all=1` にHTML内の `TAGS=` で指定したタグが含まれる。
- インポートされたブックマークの `created_at` が `ADD_DATE`（Unix秒）由来の日時になっている。

### curl 例

```bash
# 1. インポート用HTMLを作成（Netscape Bookmark形式）
IMPORT_HTML="$(mktemp --suffix=.html)"
cat > "$IMPORT_HTML" <<'EOF'
<!DOCTYPE NETSCAPE-Bookmark-file-1>
<META HTTP-EQUIV="Content-Type" CONTENT="text/html; charset=UTF-8">
<TITLE>Bookmarks</TITLE>
<H1>Bookmarks</H1>
<DL><p>
    <DT><A HREF="https://example.com/e2e-import-1" ADD_DATE="1700000000" TAGS="e2e-import,go">インポート記事1</A>
    <DD>1件目の説明文
    <DT><A HREF="https://example.com/e2e-import-2" ADD_DATE="1700001000" TAGS="e2e-import">インポート記事2</A>
</DL><p>
EOF

# 2-3. 1回目のインポート
curl -s -b "$COOKIE" -X POST "$BASE_URL/api/import" -F "file=@$IMPORT_HTML"
# => {"imported":2,"skipped":0}

# 4-5. 2回目のインポート（同じファイル＝重複）
curl -s -b "$COOKIE" -X POST "$BASE_URL/api/import" -F "file=@$IMPORT_HTML"
# => {"imported":0,"skipped":2}

# 6. 生成されたタグの確認
curl -s -b "$COOKIE" "$BASE_URL/api/tags?all=1" | jq '[.[].name] | map(select(. == "e2e-import" or . == "go"))'

# created_at の確認（ADD_DATE=1700000000 は 2023-11-14 UTC 前後）
curl -s -b "$COOKIE" "$BASE_URL/api/bookmarks?q=インポート記事1" | jq '.bookmarks[0] | {url, created_at, tags: [.tags[].name]}'
```

---

## シナリオ3: 一括操作（複数選択→バルクタグ追加→バルク削除）【必須】

複数ブックマークへのまとめてのタグ付与と、まとめて削除を確認します。

### 前提条件

- ログイン済み（`$COOKIE` 取得済み）。

### 操作手順

1. ブックマークを3件登録し、それぞれの `id` を控える。
2. タグ「e2e-bulk」を作成し `id` を控える。
3. `POST /api/bookmarks/bulk/tags` で3件すべてにタグを付与する。
4. タグフィルターで3件すべてが見えることを確認する。
5. `DELETE /api/bookmarks`（一括削除）で3件まとめて削除する。
6. 削除件数が3であること、フィルター結果が0件になることを確認する。

### 期待結果

- バルクタグ付与: `204 No Content`。
- 付与後 `GET /api/bookmarks?tag=e2e-bulk` の `total` が `3`。
- 一括削除: `{"deleted":3}`。
- 削除後 `GET /api/bookmarks?tag=e2e-bulk` の `total` が `0`。

### curl 例

```bash
# 1. ブックマーク3件登録
ID1=$(curl -s -b "$COOKIE" -X POST "$BASE_URL/api/bookmarks" -H 'Content-Type: application/json' \
  -d '{"url":"https://example.com/e2e-bulk-1","title":"一括1"}' | jq .id)
ID2=$(curl -s -b "$COOKIE" -X POST "$BASE_URL/api/bookmarks" -H 'Content-Type: application/json' \
  -d '{"url":"https://example.com/e2e-bulk-2","title":"一括2"}' | jq .id)
ID3=$(curl -s -b "$COOKIE" -X POST "$BASE_URL/api/bookmarks" -H 'Content-Type: application/json' \
  -d '{"url":"https://example.com/e2e-bulk-3","title":"一括3"}' | jq .id)
echo "IDs: $ID1 $ID2 $ID3"

# 2. タグ作成
TID=$(curl -s -b "$COOKIE" -X POST "$BASE_URL/api/tags" -H 'Content-Type: application/json' \
  -d '{"name":"e2e-bulk"}' | jq .id)

# 3. バルクタグ付与（204）
curl -s -o /dev/null -w "%{http_code}\n" -b "$COOKIE" -X POST "$BASE_URL/api/bookmarks/bulk/tags" \
  -H 'Content-Type: application/json' \
  -d "{\"bookmark_ids\":[$ID1,$ID2,$ID3],\"tag_ids\":[$TID]}"
# => 204

# 4. 付与確認（total が 3）
curl -s -b "$COOKIE" "$BASE_URL/api/bookmarks?tag=e2e-bulk" | jq .total

# 5. 一括削除
curl -s -b "$COOKIE" -X DELETE "$BASE_URL/api/bookmarks" \
  -H 'Content-Type: application/json' -d "{\"ids\":[$ID1,$ID2,$ID3]}"
# => {"deleted":3}

# 6. 削除確認（total が 0）
curl -s -b "$COOKIE" "$BASE_URL/api/bookmarks?tag=e2e-bulk" | jq .total
```

---

## シナリオ4: ログイン失敗と再ログイン成功【必須】

誤ったパスワードでの失敗と、正しいパスワードでの成功を確認します。

### 前提条件

- アプリ起動済み。**注意:** 同一IPから連続5回失敗すると **15分間ロック** されます（`maxLoginFailures=5`）。
  このシナリオは失敗を **1回だけ** に留めるため、ロックは発生しません。

### 操作手順

1. 誤ったパスワードでログインを試みる。
2. `401`（「パスワードが違います」）が返り、Cookieが発行されないことを確認する。
3. 続けて正しいパスワードでログインする。
4. `{"status":"ok"}` が返り、Cookieで一覧APIにアクセスできることを確認する。

### 期待結果

- 誤りログイン: HTTP `401`、Cookie jar に `session` が **保存されない**。
- 正しいログイン: `{"status":"ok"}`、`session` が保存される。
- ログイン後 `GET /api/bookmarks` が `200`。

### curl 例

```bash
export BASE_URL="http://localhost:8181"
export PASSWORD="test-password"
COOKIE="$(mktemp)"

# 1-2. 誤ったパスワード（401、Cookie発行なし）
curl -s -o /dev/null -w "誤りログイン: %{http_code}\n" -c "$COOKIE" \
  -X POST "$BASE_URL/api/login" -H 'Content-Type: application/json' \
  -d '{"password":"wrong-password"}'
# => 誤りログイン: 401
grep -q session "$COOKIE" && echo "NG: Cookieが発行された" || echo "OK: Cookie未発行"

# 3-4. 正しいパスワードで再ログイン
curl -s -c "$COOKIE" -X POST "$BASE_URL/api/login" \
  -H 'Content-Type: application/json' -d "{\"password\":\"$PASSWORD\"}"
# => {"status":"ok"}
curl -s -o /dev/null -w "認証付きアクセス: %{http_code}\n" -b "$COOKIE" "$BASE_URL/api/bookmarks"
# => 認証付きアクセス: 200
```

> **ロックの検証を行いたい場合:** 連続6回誤ると6回目以降は `429 Too Many Requests` になります。
> ただし15分ロックされ後続シナリオに影響するため、ロック検証は独立したテスト枠で、
> かつ専用に起動したインスタンス（または再起動でメモリ上のロック状態をクリア）で実施してください。

---

## シナリオ5: ブックマーク編集（URL・タグ・memo更新）【あると良い】

既存ブックマークの更新で各フィールドが反映されることを確認します。

### 前提条件

- ログイン済み。

### 操作手順

1. ブックマークを1件登録し `id` を控える。
2. タグを2つ作成する（更新後に付け替える先）。
3. `PUT /api/bookmarks/{id}` でURL・タイトル・excerpt（メモ）・tags をまとめて変更する。
4. レスポンスと再取得で変更が反映されていることを確認する。

### 期待結果

- 更新レスポンスが `200` で、`url` / `title` / `excerpt` が新しい値。
- `tags` が新しく指定したタグ集合に置き換わっている（`syncBookmarkTags` による置換方式）。
- `modified_at` が `null` ではなく日時になっている。

> 補足: 更新リクエストで `tags` を **省略** するとタグは変更されません。`"tags":[]` を明示すると全解除です。

### curl 例

```bash
# 1. 登録
BID=$(curl -s -b "$COOKIE" -X POST "$BASE_URL/api/bookmarks" -H 'Content-Type: application/json' \
  -d '{"url":"https://example.com/e2e-edit-before","title":"編集前タイトル","excerpt":"古いメモ"}' | jq .id)

# 2. タグ2つ作成
T1=$(curl -s -b "$COOKIE" -X POST "$BASE_URL/api/tags" -H 'Content-Type: application/json' -d '{"name":"e2e-edit-a"}' | jq .id)
T2=$(curl -s -b "$COOKIE" -X POST "$BASE_URL/api/tags" -H 'Content-Type: application/json' -d '{"name":"e2e-edit-b"}' | jq .id)

# 3. 更新（URL・タイトル・memo・tags をまとめて変更）
curl -s -b "$COOKIE" -X PUT "$BASE_URL/api/bookmarks/$BID" -H 'Content-Type: application/json' \
  -d "{\"url\":\"https://example.com/e2e-edit-after\",\"title\":\"編集後タイトル\",\"excerpt\":\"新しいメモ\",\"tags\":[{\"id\":$T1},{\"id\":$T2}]}" \
  | jq '{url, title, excerpt, modified_at, tags: [.tags[].name]}'
# => url/title/excerpt が更新、tags=["e2e-edit-a","e2e-edit-b"]、modified_at が非null

# 4. 再取得で確認
curl -s -b "$COOKIE" "$BASE_URL/api/bookmarks?q=編集後" | jq '.bookmarks[0] | {url, excerpt, tags: [.tags[].name]}'
```

---

## シナリオ6: タグ削除で紐付けが消えることの確認【あると良い】

タグを削除すると、紐付いていたブックマークから該当タグが消えること（CASCADE）を確認します。

### 前提条件

- ログイン済み。

### 操作手順

1. ブックマークを1件登録する。
2. タグを作成し、そのブックマークに付与する。
3. ブックマークにタグが付いていることを確認する。
4. `DELETE /api/tags/{id}` でタグを削除する。
5. ブックマークからそのタグが消えていることを確認する（ブックマーク自体は残る）。

### 期待結果

- タグ削除: `204 No Content`。
- 削除後、対象ブックマークの `tags` から該当タグが消える。
- ブックマーク自体は存在し続ける（URLで検索すれば見つかる）。
- `GET /api/bookmarks?tag=<削除したタグ名>` の `total` は `0`。

### curl 例

```bash
# 1. ブックマーク登録
BID=$(curl -s -b "$COOKIE" -X POST "$BASE_URL/api/bookmarks" -H 'Content-Type: application/json' \
  -d '{"url":"https://example.com/e2e-tagdel","title":"タグ削除検証"}' | jq .id)

# 2. タグ作成＆付与
TID=$(curl -s -b "$COOKIE" -X POST "$BASE_URL/api/tags" -H 'Content-Type: application/json' -d '{"name":"e2e-tagdel"}' | jq .id)
curl -s -o /dev/null -w "付与: %{http_code}\n" -b "$COOKIE" -X POST "$BASE_URL/api/bookmarks/$BID/tags" \
  -H 'Content-Type: application/json' -d "{\"tag_id\":$TID}"
# => 付与: 204

# 3. 付与確認
curl -s -b "$COOKIE" "$BASE_URL/api/bookmarks?q=タグ削除検証" | jq '.bookmarks[0].tags | map(.name)'
# => ["e2e-tagdel"]

# 4. タグ削除
curl -s -o /dev/null -w "削除: %{http_code}\n" -b "$COOKIE" -X DELETE "$BASE_URL/api/tags/$TID"
# => 削除: 204

# 5. ブックマークからタグが消えたこと＆ブックマークは残ること
curl -s -b "$COOKIE" "$BASE_URL/api/bookmarks?q=タグ削除検証" | jq '.bookmarks[0] | {url, tags: (.tags // [] | map(.name))}'
# => tags が [] になり、url は残っている
curl -s -b "$COOKIE" "$BASE_URL/api/bookmarks?tag=e2e-tagdel" | jq .total
# => 0
```

---

## シナリオ7: エクスポート内容検証（件数・タグ・ADD_DATE）【あると良い】

エクスポートしたHTMLが Netscape Bookmark形式として正しく、必要な属性を含むことを確認します。

### 前提条件

- ログイン済み。
- 検証しやすいよう、タグ付きブックマークを1件以上登録しておく（シナリオ1や2の後でも可）。

### 操作手順

1. 検証用にタグ付きブックマークを1件登録する。
2. `GET /api/export` でHTMLを取得し、ファイルに保存する。
3. ヘッダー行・件数・`TAGS=`・`ADD_DATE=`・対象URLが含まれることを確認する。

### 期待結果

- レスポンスは `200`、`Content-Type: text/html`、`Content-Disposition: attachment`。
- 先頭に `<!DOCTYPE NETSCAPE-Bookmark-file-1>` がある。
- 登録した各ブックマークが `<DT><A HREF="..." ADD_DATE="..." TAGS="...">タイトル</A>` 形式で出力される。
- 付与したタグが該当行の `TAGS=` に含まれる。`ADD_DATE` はUnix秒の数値。
- `<A ...>` の出現回数が登録件数と一致する。

### curl 例

```bash
# 1. タグ付きブックマーク登録
TID=$(curl -s -b "$COOKIE" -X POST "$BASE_URL/api/tags" -H 'Content-Type: application/json' -d '{"name":"e2e-export"}' | jq .id)
curl -s -o /dev/null -b "$COOKIE" -X POST "$BASE_URL/api/bookmarks" -H 'Content-Type: application/json' \
  -d "{\"url\":\"https://example.com/e2e-export\",\"title\":\"エクスポート検証\",\"excerpt\":\"説明\",\"tags\":[{\"id\":$TID}]}"

# 2. エクスポート（ヘッダーも確認）
EXPORT_HTML="$(mktemp --suffix=.html)"
curl -s -D - -b "$COOKIE" "$BASE_URL/api/export" -o "$EXPORT_HTML" | grep -i -E 'content-type|content-disposition'
# => Content-Type: text/html; charset=UTF-8 / Content-Disposition: attachment; filename="shirushi-bookmarks.html"

# 3. 内容検証
head -1 "$EXPORT_HTML"                              # => <!DOCTYPE NETSCAPE-Bookmark-file-1>
grep -c '<DT><A ' "$EXPORT_HTML"                    # => 登録件数（>=1）
grep 'e2e-export' "$EXPORT_HTML"                    # => 該当行に URL/TAGS が含まれる
grep -oE 'ADD_DATE="[0-9]+"' "$EXPORT_HTML" | head  # => ADD_DATE が Unix秒の数値
```

---

## シナリオ8: Bearer トークン認証【必須】

Cookie を使わないクライアント（ブラウザ拡張など）が `Authorization: Bearer <token>` で
APIを利用できることを確認します。Cookie 認証との共存も検証します。

### 前提条件

- アプリを `SHIRUSHI_API_TOKEN` **あり** で起動していること。
- Cookie 認証は従来通り動くこと（共存確認のため）。

### 起動方法

```bash
SHIRUSHI_PASSWORD='test-password' SHIRUSHI_API_TOKEN='e2e-bearer-token' ./shirushi
```

未設定で起動した場合は起動ログに
「警告: SHIRUSHI_API_TOKEN が設定されていません。Bearer 認証は無効です」と出ます。

### 操作手順

1. 正しい Bearer トークンでブックマーク登録 → 201（認証通過）
2. Bearer トークンなし → 401
3. Bearer トークン誤り → 401
4. `bearer ` 小文字（プレフィックスが厳密不一致）→ 401
5. Cookie 認証と Bearer 認証の共存確認（どちらかで通る）

### 期待結果

- 正しいトークン: `201`（または URL 重複なら `409`）— いずれも認証は通過している
- トークンなし: `401`
- 誤りトークン: `401`
- 小文字プレフィックス: `401`
- Cookie 認証: `SHIRUSHI_API_TOKEN` の有無に関わらず従来通り `200` / `201`

### curl 例

```bash
export BASE_URL="http://localhost:8181"
export API_TOKEN="e2e-bearer-token"   # 起動時の SHIRUSHI_API_TOKEN と一致させる
export PASSWORD="test-password"

# --- Bearer 認証 ---

# 1. 正しいトークンで登録（201 or 409）
curl -s -o /dev/null -w "正しいトークン: %{http_code}\n" \
  -X POST "$BASE_URL/api/bookmarks" \
  -H "Authorization: Bearer $API_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"url":"https://example.com/e2e-bearer","title":"Bearer 認証テスト"}'
# => 201（初回）または 409（URL重複） — どちらも認証は通過

# 2. トークンなし（401）
curl -s -o /dev/null -w "トークンなし: %{http_code}\n" \
  -X POST "$BASE_URL/api/bookmarks" \
  -H 'Content-Type: application/json' \
  -d '{"url":"https://example.com/e2e-bearer-notoken","title":"トークンなし"}'
# => 401

# 3. 誤ったトークン（401）
curl -s -o /dev/null -w "誤りトークン: %{http_code}\n" \
  -X POST "$BASE_URL/api/bookmarks" \
  -H 'Authorization: Bearer wrong-token' \
  -H 'Content-Type: application/json' \
  -d '{"url":"https://example.com/e2e-bearer-wrong","title":"誤りトークン"}'
# => 401

# 4. 小文字プレフィックス "bearer "（401）
curl -s -o /dev/null -w "小文字bearer: %{http_code}\n" \
  -X POST "$BASE_URL/api/bookmarks" \
  -H "Authorization: bearer $API_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"url":"https://example.com/e2e-bearer-lower","title":"小文字bearer"}'
# => 401

# --- Cookie 認証との共存確認 ---

# 5a. Cookie でログイン→一覧取得（200）— SHIRUSHI_API_TOKEN 有無に関わらず動く
COOKIE="$(mktemp)"
curl -s -c "$COOKIE" -X POST "$BASE_URL/api/login" \
  -H 'Content-Type: application/json' -d "{\"password\":\"$PASSWORD\"}" > /dev/null
curl -s -o /dev/null -w "Cookie認証(GET): %{http_code}\n" \
  -b "$COOKIE" "$BASE_URL/api/bookmarks"
# => 200

# 5b. Cookie でブックマーク登録（201 or 409）
curl -s -o /dev/null -w "Cookie認証(POST): %{http_code}\n" \
  -b "$COOKIE" -X POST "$BASE_URL/api/bookmarks" \
  -H 'Content-Type: application/json' \
  -d '{"url":"https://example.com/e2e-bearer-cookie","title":"Cookie 共存確認"}'
# => 201 or 409
```

---

## シナリオ9: Henji 本文要約【任意・外部provider利用】

保存済みブックマークだけで要約を開始し、Henji未導入時には機能を見せないことを確認します。このシナリオはHenjiとproviderの利用料金が発生し得るため、テスト専用のbookmarkとHenji設定で実行してください。

### 前提条件

- Shirushiを起動するOS userでHenjiが設定済みで、`henji`を実行できること。HenjiのAPIキーやprovider設定はShirushiではなくHenji側で管理します。
- 外部アクセスできるテスト環境であること。本文成立の例には `https://fil-c.org/`、本文不足の例には `https://sakana.ai/` を使います。
- ログイン済み（`$COOKIE`取得済み）。

### 操作手順

1. `GET /api/capabilities` が `{"henji_summary":true}` を返すことを確認する。
2. Fil-Cを保存してIDを控え、`POST /api/bookmarks/{id}/summary` がすぐに`202`を返すことを確認する。
3. しばらく待ってから一覧を手動で再読み込みし、Excerptが日本語1〜5行・400文字以内に更新されたことを確認する。
4. bookmarkカードには「AI」が見え、編集モーダルと新規登録モーダルには見えないことを確認する。確認ダイアログを取り消すと、ブラウザのNetworkにsummary POSTが出ないことも確認する。
5. Sakana AIを保存して開始し、`202`の後もExcerptと`modified_at`が変わらないことを確認する。
6. Henjiが存在しないパスを`--henji-path`に指定した別プロセスでは、capabilityがfalse、編集モーダルのボタンが非表示、summary POSTが`204`であることを確認する。

### 期待結果

- Fil-C: 開始APIはrunner完了を待たず`202`を返し、成功すれば後の手動再読み込みでExcerptだけが要約へ置き換わる。
- Sakana AI: 本文候補不足ではHenjiを起動せず、既存Excerptと`modified_at`は不変。
- 未導入: 利用者向けエラー・stderr表示はなく、通常のブックマーク操作は継続できる。
- 処理中表示、完了通知、ポーリング、自動再試行、再起動後のジョブ再開はない。同じIDの複数開始は許可され、最後に完了した結果が残る。

### curl 例

```bash
# 1. Henjiの存在確認。APIキーやprovider到達性までは検査しません。
curl -s -b "$COOKIE" "$BASE_URL/api/capabilities"
# => {"henji_summary":true}

# 2. Fil-Cを保存し、要約を非同期開始する
SUMMARY_ID=$(curl -s -b "$COOKIE" -X POST "$BASE_URL/api/bookmarks" \
  -H 'Content-Type: application/json' \
  -d '{"url":"https://fil-c.org/","title":"Fil-C 本文要約E2E","excerpt":"要約前のメモ"}' | jq .id)

curl -s -o /dev/null -w "%{http_code}\n" -b "$COOKIE" \
  -X POST "$BASE_URL/api/bookmarks/$SUMMARY_ID/summary"
# => 202

# 完了を通知するAPIはありません。待機後に手動で一覧を取得して確認します。
curl -s -b "$COOKIE" "$BASE_URL/api/bookmarks?q=Fil-C%20%E6%9C%AC%E6%96%87%E8%A6%81%E7%B4%84E2E" \
  | jq '.bookmarks[0] | {excerpt, modified_at}'

# 5. 本文不足の例。開始は受理されても、Excerptを上書きしません。
INSUFFICIENT_ID=$(curl -s -b "$COOKIE" -X POST "$BASE_URL/api/bookmarks" \
  -H 'Content-Type: application/json' \
  -d '{"url":"https://sakana.ai/","title":"本文不足E2E","excerpt":"保持するメモ"}' | jq .id)
curl -s -o /dev/null -w "%{http_code}\n" -b "$COOKIE" \
  -X POST "$BASE_URL/api/bookmarks/$INSUFFICIENT_ID/summary"
# => 202
curl -s -b "$COOKIE" "$BASE_URL/api/bookmarks?q=%E6%9C%AC%E6%96%87%E4%B8%8D%E8%B6%B3E2E" \
  | jq '.bookmarks[0] | {excerpt, modified_at}'
# => excerpt は "保持するメモ" のまま、modified_at も要約開始前から変わらない
```

---

## 後始末（任意）

シナリオで作成したテストデータを消したい場合、URLや件数を確認のうえ削除します。
DBを直接初期化したい場合は、アプリ停止後に `shirushi.db*`（`-wal` / `-shm` 含む）を退避するのが確実です。
本番DBでは実行しないこと。テスト専用のDBファイル・パスワードで実施することを推奨します。
