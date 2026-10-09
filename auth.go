package main

// auth.go：認証・セッション管理・ログイン制限を担当するファイルです。
// authMiddleware で全APIに認証チェックを適用し、handleLogin / handleLogout で
// セッションの発行・削除を行います。ブルートフォース対策のIP制限も含みます。

import (
	"crypto/rand"   // 暗号学的に安全な乱数を生成するパッケージ
	"crypto/subtle" // タイミング攻撃を防ぐ一定時間比較のためのパッケージ
	"encoding/hex"  // バイト列を16進数文字列に変換するパッケージ
	"encoding/json" // レスポンスをJSON形式で返すパッケージ
	"fmt"           // プロキシ設定の不正なIPを説明します
	"net"           // IPアドレスの判定や SplitHostPort に使うパッケージ
	"net/http"      // Webハンドラ・Cookie操作に使うパッケージ
	"net/netip"     // IPv6のインターフェース識別子を含む接続元を解析します
	"os"            // 環境変数を読み取るパッケージ
	"strings"       // authMiddleware のパス判定に使うパッケージ
	"time"          // セッション有効期限・ロック時間の管理に使うパッケージ
)

// cleanupExpiredSessions：期限切れセッションを定期的に掃除するバックグラウンド処理です。
// 認証チェック時の削除だけでは「二度と使われないトークン」がマップに残り続けるため、
// 1時間ごとに全エントリを確認して期限切れを削除します。
// main から `go cleanupExpiredSessions()` と呼ぶことで、
// サーバー本体とは別のゴルーチン（軽量スレッド）として並行に動き続けます。
func cleanupExpiredSessions() {
	// time.Tick は指定間隔ごとに値が届くチャネルを返します。
	// for range で受け取ることで「1時間ごとに1回ループが回る」動きになります。
	for range time.Tick(1 * time.Hour) {
		now := nowFunc()
		sessionsMu.Lock()
		for token, expiry := range sessions {
			if now.After(expiry) {
				delete(sessions, token)
			}
		}
		sessionsMu.Unlock()
	}
}

// authMiddleware：全リクエストに認証チェックを適用するミドルウェアです。
// ミドルウェアとは「ハンドラの前後に処理を挟む仕組み」のことです。
// http.Handler を受け取り、認証チェックを追加した新しい http.Handler を返します。
func authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// ログイン・ログアウトAPIと静的ファイルは認証不要です。
		if r.URL.Path == "/api/login" ||
			r.URL.Path == "/api/logout" ||
			!strings.HasPrefix(r.URL.Path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}

		// --- セッション Cookie チェック（Web UI 用） ---
		// Cookieからセッショントークンを取り出します。
		cookie, err := r.Cookie("session")
		if err == nil {
			// トークンが有効かどうかを確認します。
			sessionsMu.Lock()
			expiry, ok := sessions[cookie.Value]
			expired := ok && nowFunc().After(expiry)
			if expired {
				// 期限切れのトークンは見つけた時点でマップから削除します。
				// 放置するとメモリに溜まり続けるためです。
				delete(sessions, cookie.Value)
			}
			sessionsMu.Unlock()

			if ok && !expired {
				// Cookie 認証OK：次のハンドラに処理を渡します。
				next.ServeHTTP(w, r)
				return
			}
		}

		// --- Bearer トークンチェック（ブラウザ拡張用） ---
		// SHIRUSHI_API_TOKEN が設定されている場合のみ Bearer 認証を試みます。
		// Authorization ヘッダから "Bearer " プレフィックスを除いたトークンを取り出します。
		// strings.CutPrefix は第2引数のプレフィックスが存在する場合だけ true を返します。
		// プレフィックスは大文字小文字を厳密に区別します（"bearer " は不一致）。
		if apiToken != "" {
			if rawToken, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok {
				// タイミング攻撃を防ぐために定数時間比較を使います（1 = 一致）。
				// []byte 変換が必要なのは ConstantTimeCompare がバイトスライスを引数に取るためです。
				if subtle.ConstantTimeCompare([]byte(rawToken), []byte(apiToken)) == 1 {
					// Bearer 認証OK：次のハンドラに処理を渡します。
					next.ServeHTTP(w, r)
					return
				}
			}
		}

		// Cookie も Bearer も通過しなかった場合は 401 を返します。
		http.Error(w, "認証が必要です", http.StatusUnauthorized)
	})
}

// handleLogin：パスワードを受け取り、正しければセッショントークンを発行するAPIです。
func handleLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Password   string `json:"password"`
		RememberMe bool   `json:"rememberMe"` // true のとき30日間セッションを維持します。
	}
	if err := decodeJSONBody(w, r, &body); err != nil {
		http.Error(w, "リクエスト解析エラー", http.StatusBadRequest)
		return
	}

	clientIP := getClientIP(r)
	if isLoginLocked(clientIP) {
		http.Error(w, "ログイン試行が多すぎます。しばらく待ってから再試行してください", http.StatusTooManyRequests)
		return
	}

	// 環境変数からパスワードを取得して照合します。
	// パスワードをコードに直接書かず環境変数にする理由は、
	// ソースコードをgitで管理しても漏洩しないようにするためです。
	correctPassword := os.Getenv("SHIRUSHI_PASSWORD")
	// パスワードの比較には subtle.ConstantTimeCompare を使います。
	// 通常の == や != は「先頭から比較して違いが見つかった時点で終了」するため、
	// 応答時間のわずかな差から正解のパスワードを1文字ずつ推測される
	// 「タイミング攻撃」の余地があります。この関数は内容に関わらず
	// 常に同じ時間で比較するため、その手がかりを与えません（一致すると1を返します）。
	if correctPassword == "" ||
		subtle.ConstantTimeCompare([]byte(body.Password), []byte(correctPassword)) != 1 {
		if recordLoginFailure(clientIP) {
			http.Error(w, "ログイン試行が多すぎます。しばらく待ってから再試行してください", http.StatusTooManyRequests)
			return
		}
		http.Error(w, "パスワードが違います", http.StatusUnauthorized)
		return
	}
	clearLoginFailures(clientIP)

	// crypto/rand で暗号学的に安全なランダムトークンを生成します。
	// math/rand と違い、予測不可能な値が生成されます。
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		http.Error(w, "トークン生成エラー", http.StatusInternalServerError)
		return
	}
	token := hex.EncodeToString(tokenBytes) // バイト列を16進数文字列に変換します。

	// rememberMe の有無でセッション有効期限とCookieの寿命を切り替えます。
	// rememberMe=true : 30日間（ブラウザを閉じても維持）
	// rememberMe=false: ブラウザを閉じると消えるセッションCookie（MaxAge=0 で指定しない）
	var sessionTTL time.Duration
	cookieMaxAge := 0 // 0 = MaxAge 属性を付けない → ブラウザセッション中のみ有効
	if body.RememberMe {
		sessionTTL = 30 * 24 * time.Hour
		cookieMaxAge = int(sessionTTL.Seconds()) // 30日（秒）
	} else {
		// サーバー側セッションは24時間で失効させます。
		// Cookieはブラウザが管理しますが、サーバー側に期限を設けることで
		// 長時間放置されたセッションをクリーンアップできます。
		sessionTTL = 24 * time.Hour
	}

	// セッションを保存します。
	sessionsMu.Lock()
	sessions[token] = nowFunc().Add(sessionTTL)
	sessionsMu.Unlock()

	// Cookieにトークンをセットします。
	// HttpOnly: JavaScriptからCookieを読めなくする（XSS対策）
	// SameSite: 別サイトからのリクエストにCookieを送らない（CSRF対策）
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   secureSessionCookie(),
		SameSite: http.SameSiteStrictMode,
		MaxAge:   cookieMaxAge,
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// handleLogout：セッションを削除してログアウトするAPIです。
func handleLogout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("session")
	if err == nil {
		// セッションマップからトークンを削除します。
		sessionsMu.Lock()
		delete(sessions, cookie.Value)
		sessionsMu.Unlock()
	}

	// Cookieを即座に無効化します（MaxAge=-1 で削除）。
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   secureSessionCookie(),
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// secureSessionCookie：セッションCookieに Secure 属性を付けるか判定します。
// Secure 属性が付いたCookieはHTTPS通信でしか送られません。
// CaddyでHTTPS終端する本番運用では有効にし、HTTPのローカル開発では未設定にします。
func secureSessionCookie() bool {
	return os.Getenv("SHIRUSHI_COOKIE_SECURE") == "1"
}

// getClientIP：ログイン制限に使うクライアントIPを取り出します。
// ループバックまたは設定済みのCaddyなどから接続している場合だけ、
// X-Forwarded-For / X-Real-IP を信頼します。直接アクセス時にこれらのヘッダーを
// 無条件に信じると、攻撃者が任意のIPを名乗れてしまうためです。
func getClientIP(r *http.Request) string {
	remoteHost, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		remoteHost = r.RemoteAddr
	}
	remoteAddr, err := netip.ParseAddr(remoteHost)
	if err != nil {
		return remoteHost
	}
	remoteIP := net.IP(remoteAddr.AsSlice())

	if isTrustedProxyIP(remoteIP) {
		if ip := lastForwardedIP(r.Header.Get("X-Forwarded-For")); ip != "" {
			return ip
		}
		if ip := validHeaderIP(r.Header.Get("X-Real-IP")); ip != "" {
			return ip
		}
		// Caddy の reverse_proxy は通常 X-Forwarded-For を自動付与します。
		// ヘッダーが届かない場合はプロキシIP（通常 127.0.0.1）のままになり、
		// 全クライアントのログイン試行が同じIPとしてカウントされます。
		// 攻撃者が5回失敗させると正規ユーザーも15分ロックされる可能性があります。
		// Caddy 構成では X-Forwarded-For を削除しないでください（_refs/caddy-deployment.md 参照）。
	}

	return remoteIP.String()
}

// isTrustedProxyIP：プロキシ用ヘッダーを信頼してよい接続元か判定します。
// 同じLXCのループバックと、別LXC用に明示されたIPだけを信頼します。
func isTrustedProxyIP(ip net.IP) bool {
	if ip.IsLoopback() {
		return true
	}
	for _, proxyIP := range trustedProxyIPs {
		if ip.Equal(proxyIP) {
			return true
		}
	}
	return false
}

// parseTrustedProxyIPs：カンマ区切りのIPv4/IPv6を起動時に検証します。
func parseTrustedProxyIPs(raw string) ([]net.IP, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var ips []net.IP
	for _, value := range strings.Split(raw, ",") {
		value = strings.TrimSpace(value)
		ip := net.ParseIP(value)
		if ip == nil {
			return nil, fmt.Errorf("SHIRUSHI_TRUSTED_PROXIES にはプロキシのIPアドレスを指定してください（不正な値: %q）", value)
		}
		ips = append(ips, ip)
	}
	return ips, nil
}

// lastForwardedIP：X-Forwarded-For の末尾IPを取り出します。
// X-Forwarded-For は「元のクライアント, プロキシ1, プロキシ2」のようにカンマ区切りで、
// プロキシは自分が受け取ったIPを末尾に追記します。
// 先頭はクライアントが自由に偽装できますが、末尾は直前の信頼できるプロキシ（Caddy）が
// 付加したIPなので、レートリミット用途では末尾を使うのが安全です。
func lastForwardedIP(header string) string {
	parts := strings.Split(header, ",")
	for i := len(parts) - 1; i >= 0; i-- {
		if ip := validHeaderIP(parts[i]); ip != "" {
			return ip
		}
	}
	return ""
}

// validHeaderIP：ヘッダー文字列がIPアドレスとして妥当なら正規化して返します。
func validHeaderIP(value string) string {
	ip := net.ParseIP(strings.TrimSpace(value))
	if ip == nil {
		return ""
	}
	return ip.String()
}

// isLoginLocked：対象IPがロック中か確認します。
// 判定のみを行い、マップの変更（副作用）は行いません。
// 期限切れレコードの削除は recordLoginFailure に一元化しています。
func isLoginLocked(clientIP string) bool {
	now := nowFunc()

	loginAttemptsMu.Lock()
	defer loginAttemptsMu.Unlock()

	attempt, ok := loginAttempts[clientIP]
	if !ok {
		return false
	}
	return attempt.LockedUntil.After(now)
}

// recordLoginFailure：ログイン失敗を記録し、上限到達時はロック状態にします。
// 期限切れレコードのリセットもここで一元管理します。
// isLoginLocked でロックと判定された場合はこの関数は呼ばれないため、
// LockedUntil が設定されているレコードに到達するのは「ロック期間が切れた直後」です。
func recordLoginFailure(clientIP string) bool {
	now := nowFunc()

	loginAttemptsMu.Lock()
	defer loginAttemptsMu.Unlock()

	attempt := loginAttempts[clientIP]

	// 次の2条件のいずれかでカウントをリセットします:
	//   1. ロック期間が切れた（LockedUntil が過去になった）
	//   2. 失敗ウィンドウが切れた（FirstFailure から loginFailureWindow 以上経過した）
	lockedButExpired := !attempt.LockedUntil.IsZero() && !attempt.LockedUntil.After(now)
	windowExpired := !attempt.FirstFailure.IsZero() && now.Sub(attempt.FirstFailure) > loginFailureWindow
	if lockedButExpired || windowExpired || attempt.FirstFailure.IsZero() {
		attempt = loginAttempt{FirstFailure: now}
	}

	attempt.Failures++
	if attempt.Failures >= maxLoginFailures {
		attempt.LockedUntil = now.Add(loginLockoutDuration)
		loginAttempts[clientIP] = attempt
		return true
	}

	loginAttempts[clientIP] = attempt
	return false
}

// clearLoginFailures：正しいログインに成功したIPの失敗記録を消します。
func clearLoginFailures(clientIP string) {
	loginAttemptsMu.Lock()
	delete(loginAttempts, clientIP)
	loginAttemptsMu.Unlock()
}
