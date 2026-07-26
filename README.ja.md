# Shirushi（しるし）

English documentation: [README.md](README.md)

個人用のブックマーク管理アプリです。URLを保存・タグ管理・検索するだけでなく、
検索 → 複数選択 → バルクタグ付けという流れで**関心の記録を育てる**ことを重視しています。

Go + SQLite のシングルバイナリで動き、自宅サーバー（Proxmox / Alpine Linux）に置いて
Caddy 経由で外出先からも使えます。

> **インスピレーション**: [Shiori](https://github.com/go-shiori/shiori) をベースに、
> アーカイブや複数ユーザー管理は省き、バルク操作・タグ管理・個人の研究記録に特化して作り直しました。

---

## 機能

- ブックマークの追加・編集・削除
- OGP メタデータ（タイトル・説明・サムネイル）の自動取得
- タグの作成・編集・削除、ブックマークへの紐付け
- キーワード検索・タグフィルター・日付検索（`202507` で2025年7月など）
- 1ページの表示件数切り替え（50・100・200件）と一覧上下のページネーション
- チェックボックスで複数選択 → バルクタグ追加・削除・一括削除
- Netscape Bookmark 形式（Chrome/Firefox のエクスポート形式）でインポート・エクスポート
- パスワード認証（シングルユーザー、30日間のログイン維持に対応）
- Bearer トークン認証（ブラウザ拡張などのAPIクライアント向け）
- 任意のHenji連携による、保存済みブックマーク本文の日本語要約
- ダークテーマ固定

---

![メイン画面](docs/screenshot.png)

---

## ビルド

```bash
# 手元の環境で実行する場合
go build -o shirushi .

# Alpine Linux / Proxmox LXC 向け静的バイナリ（CGO 不要）
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o shirushi .
```

Go 1.26.4 以上が必要です。依存ライブラリは `modernc.org/sqlite`（CGO 不要の純粋 Go 実装）のみです。

---

## 起動

```bash
SHIRUSHI_PASSWORD='yourpassword' ./shirushi
```

ブラウザで `http://localhost:8181` を開いてください。

`SHIRUSHI_PASSWORD` が未設定の場合は起動時にエラーになります。

### 任意: Henji による本文要約

Shirushi v1.0.0では、保存済みブックマークの静的HTMLを[Henji](https://forge.harakara.site/littleisland/henji)を通じて生成AIモデルで日本語要約できます。Henjiは任意の外部コマンドです。providerの認証情報をHenji側で設定したうえで、ShirushiはPATH上の`henji`（または`--henji-path`で指定したパス）を使います。Henjiが見つからない場合はカードの「AI」ボタンを表示せず、通常のブックマーク機能はそのまま使えます。

設定方法、起動引数、安全性の境界、上限、非同期UIの仕様は[Henji本文要約](docs/henji-summary.ja.md)を参照してください。ShirushiはHenjiのAPIキーやprovider設定画面を持ちません。

```bash
# PATH上のhenjiを使う
SHIRUSHI_PASSWORD='yourpassword' ./shirushi
```

---

## 環境変数

| 変数名 | デフォルト | 説明 |
|--------|-----------|------|
| `SHIRUSHI_PASSWORD` | （必須） | ログインパスワード。未設定だと起動しません |
| `SHIRUSHI_API_TOKEN` | （未設定） | Bearer認証用トークン。未設定の場合はCookie認証のみ有効 |
| `SHIRUSHI_ADDR` | `:8181` | 待ち受けアドレス。Caddy 同一ホスト構成では `127.0.0.1:8181` を推奨 |
| `SHIRUSHI_COOKIE_SECURE` | （未設定） | `1` にするとセッション Cookie に `Secure` 属性を付与。HTTPS 運用時に設定 |
| `SHIRUSHI_ALLOW_PRIVATE_FETCH` | （未設定） | `1` にするとメタデータ取得時の SSRF チェックを無効化。社内ツールなど限定用途向け |

---

## Caddy を使った HTTPS 運用

Caddy を別の LXC コンテナで動かし、Shirushi を別の LXC コンテナに置く構成を想定しています。

```
インターネット
  → Caddy LXC（HTTPS 終端）
  → Proxmox ブリッジ
  → Shirushi LXC（:8181）
```

**Caddyfile**（Caddy LXC 側）:

```caddyfile
shirushi.example.com {
    reverse_proxy <Shirushi LXC の LAN IP>:8181
}
```

**Shirushi の起動**（Shirushi LXC 側）:

```bash
SHIRUSHI_PASSWORD='長くランダムなパスワード' \
SHIRUSHI_COOKIE_SECURE=1 \
./shirushi
```

待ち受けアドレスはデフォルトの `:8181`（全インターフェース）のままで構いません。
80/443 番ポートのみ外部公開し Caddy にルーティングする構成であれば、8181 はインターネットに露出しません。

> **注意**: `header_up -X-Forwarded-For` で XFF ヘッダーを削除しないでください。
> XFF がない状態で全リクエストが Caddy LXC の IP として扱われ、ログイン失敗カウントを
> 全クライアントで共有します。Caddy の `reverse_proxy` はデフォルトで XFF を自動付与するため、
> 通常は設定不要です。

---

## バックアップとリストア

データベースは実行ファイルと同じディレクトリの `shirushi.db` に保存されます。

```bash
# バックアップ（サーバー停止不要。WAL モードのため安全にコピーできます）
cp shirushi.db shirushi.db.backup-$(date +%Y%m%d)

# リストア
cp shirushi.db.backup-20260614 shirushi.db
```

WAL モードでは `shirushi.db-wal` と `shirushi.db-shm` という補助ファイルも作られます。
バックアップ時はこれらもあわせてコピーするか、`VACUUM` コマンドで統合してからコピーしてください。

```bash
# WAL を統合してからバックアップ（よりシンプル）
sqlite3 shirushi.db "VACUUM;"
cp shirushi.db shirushi.db.backup-$(date +%Y%m%d)
```

---

## テスト

```bash
go test -v -cover ./...
```

---

## 既知の制限

- **シングルユーザーのみ**: 複数アカウントには対応していません
- **セッションはメモリ管理**: サーバーを再起動するとログアウト状態になります
- **インポート時のサムネイル取得は非同期**: Netscape Bookmark 形式でインポートした場合、OG 画像はバックグラウンドで順次取得されます。件数が多い場合はしばらく待ってからリロードしてください
- **Henji要約は非同期・無通知**: 開始後の進捗や完了は画面に表示しません。要約結果は後で一覧を再読み込みして確認してください
- **アーカイブ機能なし**: ページのオフラインコピーは作成しません

---

## API

エンドポイント・リクエスト・レスポンス・エラーコードの詳細は [docs/api.ja.md](docs/api.ja.md) を参照してください。

---

## ミラー

このリポジトリは [Codeberg](https://codeberg.org/littleisland/shirushi) にミラーされています。
正リポジトリは [forge.harakara.site/littleisland/shirushi](https://forge.harakara.site/littleisland/shirushi) です。

---

## ライセンス

[MIT](LICENSE)

## 変更履歴

[CHANGELOG.ja.md](CHANGELOG.ja.md)
