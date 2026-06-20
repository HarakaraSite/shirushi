# Shirushi Caddy運用メモ

> 作成日: 2026-06-14

## 想定構成

外出先からHTTPSでアクセスし、CaddyでTLS終端してからShirushiへ転送します。

```text
Browser
  -> HTTPS
  -> Caddy
  -> HTTP http://127.0.0.1:8181
  -> Shirushi
```

Shirushi本体はCaddyの背後に置き、`8181` 番ポートをインターネットへ直接公開しない前提です。

## Shirushiの起動

```bash
SHIRUSHI_PASSWORD='長くランダムなパスワード' ./shirushi
```

現状のShirushiは `:8181` で待ち受けます。
Caddy経由だけで使うなら、将来的には `127.0.0.1:8181` に限定できる環境変数を追加するとより安全です。

検討例:

```bash
SHIRUSHI_ADDR=127.0.0.1:8181 SHIRUSHI_PASSWORD='...' ./shirushi
```

## Caddyfile例

```caddyfile
shirushi.example.com {
	reverse_proxy 127.0.0.1:8181
}
```

Caddyは通常、`reverse_proxy` 時に `X-Forwarded-For` / `X-Forwarded-Proto` / `X-Forwarded-Host` などを自動で付与します。
Shirushi側では、接続元がループバックの場合だけ `X-Forwarded-For` / `X-Real-IP` を信頼します。

> **注意**: `header_up -X-Forwarded-For` などで XFF を削除しないでください。
> XFF がない状態で Caddy 経由アクセスが来ると、すべてのクライアントが
> `127.0.0.1` 扱いになり、ログイン失敗カウントを共有します。
> 攻撃者が5回失敗させると正規ユーザーも15分ログインできなくなります。

## LAN内だけで使う場合

Caddyを使わず、宅内LANから直接 `http://192.168.x.x:8181` にアクセスする使い方も可能です。
この場合、Shirushiは `X-Forwarded-For` / `X-Real-IP` を無視し、実際の接続元IPでログイン失敗回数を数えます。

## 別ホスト・別コンテナのCaddy

Caddyを別コンテナ、別LXC、別ホストで動かす場合、Shirushiから見る接続元は `127.0.0.1` ではなく、Caddy側のLAN IPやコンテナIPになることがあります。
現状ではこの場合、転送ヘッダーを信頼しないため、ログイン失敗制限はプロキシIP単位になります。

これは安全側の挙動です。
公開コードとして幅広い構成に対応するなら、将来的に以下のような環境変数を追加する案があります。

```bash
SHIRUSHI_TRUSTED_PROXIES=127.0.0.1,::1,192.168.1.10
```

CIDR指定を許可する案もありますが、設定ミスで広く信頼しすぎるリスクがあります。

```bash
SHIRUSHI_TRUSTED_PROXIES=127.0.0.1/32,::1/128,172.16.0.0/12
```

## Caddy側で追加するとよい防御

Shirushi本体にもログイン失敗制限がありますが、Caddy側でも軽い制限を足すと前段で余分な負荷を落とせます。

候補:

- Caddyのアクセスログを有効にする
- Caddyプラグインや前段FWで `/api/login` への過剰アクセスを制限する
- 管理用途ならVPNやTailscaleと組み合わせる

ただし、Shirushiは標準Caddyだけでも動く構成を優先します。

## Cookie設定の今後

現状のCookie:

- `HttpOnly`
- `SameSite=Strict`
- `MaxAge=86400`

CaddyでHTTPS運用する場合、セッションCookieに `Secure` を付けると、ブラウザはHTTPS通信でしかCookieを送らなくなります。
ただし、ローカル開発やLAN内HTTP利用では `Secure` Cookie が送信されなくなるため、環境変数で切り替えるのが無難です。

検討例:

```bash
SHIRUSHI_COOKIE_SECURE=1
```

本番Caddy運用では `SHIRUSHI_COOKIE_SECURE=1`、ローカルHTTP確認では未設定にする想定です。
