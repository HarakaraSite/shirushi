package main

// metadata.go：URLにアクセスしてOGPメタデータ（タイトル・説明・画像）を取得する処理をまとめたファイルです。
// SSRF（内部ネットワークへの不正アクセス）対策として、接続先IPの検査も行います。

import (
	"context"       // safeDialContext でタイムアウトを伝えるパッケージ
	"encoding/json" // レスポンスをJSON形式で返すパッケージ
	"errors"        // エラー生成・判定に使うパッケージ
	"fmt"           // HTTPステータスコードを含むエラーメッセージ生成に使うパッケージ
	"io"            // レスポンスボディを読み取るパッケージ
	"net"           // IPアドレスの判定・DNS解決・接続に使うパッケージ
	"net/http"      // HTTPリクエストの送信・ハンドラに使うパッケージ
	"net/url"       // リダイレクト後のURLを画像URLの解決に使うパッケージ
	"os"            // 環境変数を読み取るパッケージ
	"regexp"        // HTMLのメタタグを正規表現で抽出するパッケージ
	"strings"       // Content-Type 判定・テキスト処理に使うパッケージ
	"time"          // HTTPクライアントのタイムアウト設定に使うパッケージ
)

// handleFetchMetadata：指定されたURLにアクセスしてメタデータを返すAPIです。
func handleFetchMetadata(w http.ResponseWriter, r *http.Request) {
	var body struct {
		URL string `json:"url"`
	}
	if err := decodeJSONBody(w, r, &body); err != nil || body.URL == "" {
		http.Error(w, "URLは必須です", http.StatusBadRequest)
		return
	}
	validatedURL, err := validateHTTPURL(body.URL)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	meta, err := fetchMetadata(validatedURL)
	if err != nil {
		http.Error(w, "メタデータの取得に失敗しました", http.StatusBadGateway)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(meta)
}

// isPrivateIP：プライベート・内部ネットワーク向けのIPアドレスかどうかを判定します。
// SSRF（Server-Side Request Forgery）対策に使います。
// SSRFとは「サーバーに内部ネットワークへのアクセスを代行させる攻撃」のことで、
// 例えば http://192.168.1.1/ を渡してルーターの管理画面を取得させる、といった悪用です。
func isPrivateIP(ip net.IP) bool {
	return ip.IsLoopback() || // 127.0.0.1 など自分自身
		ip.IsPrivate() || // 10.x / 172.16-31.x / 192.168.x のLAN内アドレス
		ip.IsLinkLocalUnicast() || // 169.254.x（クラウドのメタデータAPIで悪用されがち）
		ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified() // 0.0.0.0
}

// safeDialContext：接続先のIPを検査してから接続する、安全なダイヤル関数です。
// URLの文字列ではなく「実際に接続する瞬間のIP」を検査するのがポイントで、
// これによりリダイレクトやDNSの再解決を使ったすり抜けも防げます。
func safeDialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	// addr は "example.com:443" のような形式なので、ホスト名とポートに分けます。
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}

	// ホスト名をIPアドレスに解決します（既にIPならそのまま返ります）。
	ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
	if err != nil {
		return nil, err
	}
	for _, ip := range ips {
		if isPrivateIP(ip) {
			return nil, errors.New("内部ネットワークへのアクセスは禁止されています: " + ip.String())
		}
	}

	// 検査をパスした解決済みIPに対して直接接続します。
	// ホスト名のまま接続すると、接続時にDNSが再解決されて
	// 別の（内部の）IPに繋がる恐れがあるためです。
	var d net.Dialer
	return d.DialContext(ctx, network, net.JoinHostPort(ips[0].String(), port))
}

// fetchMetadata：URLにHTTPアクセスしてHTMLからメタデータを抽出する関数です。
func fetchMetadata(url string) (*Metadata, error) {
	html, responseURL, err := fetchHTML(url, 1024*1024)
	if err != nil {
		return nil, err
	}

	meta := &Metadata{}
	// OGタグを優先し、なければ通常のメタタグ・titleタグを使います。
	meta.Title = firstNonEmpty(extractOGTag(html, "og:title"), extractTitle(html))
	meta.Excerpt = firstNonEmpty(extractOGTag(html, "og:description"), extractMetaTag(html, "description"))
	meta.Author = firstNonEmpty(extractOGTag(html, "og:author"), extractMetaTag(html, "author"))
	// og:image は相対URLで返すサイトがあるため、リクエスト先URLを基準に絶対URL化します。
	// 例: "/og.png" → "https://example.com/og.png"
	// http/https 以外（data: URI 等）は保存しません。
	if raw := extractOGTag(html, "og:image"); raw != "" {
		if abs, err := responseURL.Parse(raw); err == nil &&
			(abs.Scheme == "http" || abs.Scheme == "https") {
			meta.ImageURL = abs.String()
		}
	}

	return meta, nil
}

// fetchHTML：既存のSSRF対策を保ったまま、指定サイズまでHTMLを取得します。
// metadata と本文抽出で同じ取得境界を使うため、HTTP通信の処理をここへ集約します。
func fetchHTML(rawURL string, maxBytes int64) (string, *url.URL, error) {
	client := newSafeHTTPClient()

	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return "", nil, err
	}
	// User-Agentを設定しないとアクセスを弾くサイトがあるため設定します。
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; Shirushi/1.0)")

	resp, err := client.Do(req)
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return "", nil, fmt.Errorf("HTTPステータスがエラーです: %d", resp.StatusCode)
	}
	contentType := strings.ToLower(resp.Header.Get("Content-Type"))
	if contentType != "" &&
		!strings.Contains(contentType, "text/html") &&
		!strings.Contains(contentType, "application/xhtml+xml") {
		return "", nil, errors.New("HTMLではないレスポンスです")
	}

	// HTMLが大きいサイトでも安全に処理できるよう、最大1MBだけ読み込みます。
	// メタタグは通常 <head> 内にあるので先頭部分で十分です。
	limitedBody, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes))
	if err != nil {
		return "", nil, err
	}
	return string(limitedBody), resp.Request.URL, nil
}

// newSafeHTTPClient：外部URL取得で共通利用する、安全設定済みHTTPクライアントを作ります。
// メタデータ取得と404確認で同じタイムアウト・リダイレクト・SSRF対策を使うことで、
// 一方だけ安全設定を忘れる事故を防ぎます。
func newSafeHTTPClient() *http.Client {
	// 10秒でタイムアウトするHTTPクライアントを作ります。
	// デフォルトのクライアントはタイムアウトがないため、自前で設定するのが定石です。
	client := &http.Client{
		Timeout: 10 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("リダイレクト回数が多すぎます")
			}
			if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
				return errors.New("http/https 以外へのリダイレクトは禁止されています")
			}
			return nil
		},
	}

	// SSRF対策：接続先のIPを検査するダイヤル関数を組み込みます。
	// 自宅LAN内のサーバーのメタデータを取得したい場合は、
	// 環境変数 SHIRUSHI_ALLOW_PRIVATE_FETCH=1 を設定すると検査を無効化できます。
	if os.Getenv("SHIRUSHI_ALLOW_PRIVATE_FETCH") != "1" {
		client.Transport = &http.Transport{DialContext: safeDialContext}
	}
	return client
}

// extractTitle：HTMLの <title> タグからテキストを取り出します。
func extractTitle(html string) string {
	// (?i) は大文字小文字を区別しないオプションです。
	re := regexp.MustCompile(`(?i)<title[^>]*>([^<]+)</title>`)
	if m := re.FindStringSubmatch(html); len(m) > 1 {
		return strings.TrimSpace(m[1])
	}
	return ""
}

// extractOGTag：Open Graph プロトコルのメタタグから content を取り出します。
// <meta property="og:title" content="..."> のような形式を対象にします。
func extractOGTag(html, property string) string {
	// property と content の順序が逆でも対応できるよう2パターン用意します。
	patterns := []string{
		`(?i)<meta[^>]*property=["\']` + regexp.QuoteMeta(property) + `["\'][^>]*content=["\']([^"\']+)["\']`,
		`(?i)<meta[^>]*content=["\']([^"\']+)["\'][^>]*property=["\']` + regexp.QuoteMeta(property) + `["\']`,
	}
	for _, p := range patterns {
		re := regexp.MustCompile(p)
		if m := re.FindStringSubmatch(html); len(m) > 1 {
			return strings.TrimSpace(m[1])
		}
	}
	return ""
}

// extractMetaTag：通常の <meta name="..." content="..."> からcontent を取り出します。
func extractMetaTag(html, name string) string {
	patterns := []string{
		`(?i)<meta[^>]*name=["\']` + regexp.QuoteMeta(name) + `["\'][^>]*content=["\']([^"\']+)["\']`,
		`(?i)<meta[^>]*content=["\']([^"\']+)["\'][^>]*name=["\']` + regexp.QuoteMeta(name) + `["\']`,
	}
	for _, p := range patterns {
		re := regexp.MustCompile(p)
		if m := re.FindStringSubmatch(html); len(m) > 1 {
			return strings.TrimSpace(m[1])
		}
	}
	return ""
}

// firstNonEmpty：引数の中で最初の空でない文字列を返します。
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
