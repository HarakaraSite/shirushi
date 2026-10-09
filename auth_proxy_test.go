package main

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 別LXCのCaddyが伝えるクライアントIPごとに、ログイン失敗を分離します。
func TestHandleLogin_SeparatesClientsBehindTrustedProxy(t *testing.T) {
	resetSessions()
	t.Cleanup(resetSessions)
	t.Setenv("SHIRUSHI_PASSWORD", "secret")
	previous := trustedProxyIPs
	trustedProxyIPs = []net.IP{net.ParseIP("192.168.1.10")}
	t.Cleanup(func() { trustedProxyIPs = previous })

	login := func(client, password string) int {
		r := httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(`{"password":"`+password+`"}`))
		r.RemoteAddr = "192.168.1.10:12345"
		r.Header.Set("X-Forwarded-For", client)
		w := httptest.NewRecorder()
		handleLogin(w, r)
		return w.Code
	}
	for i := 0; i < maxLoginFailures; i++ {
		want := http.StatusUnauthorized
		if i == maxLoginFailures-1 {
			want = http.StatusTooManyRequests
		}
		if got := login("203.0.113.30", "wrong"); got != want {
			t.Fatalf("failure %d: got %d, want %d", i+1, got, want)
		}
	}
	if got := login("198.51.100.20", "secret"); got != http.StatusOK {
		t.Fatalf("another client was locked: got %d, want 200", got)
	}
	if got := login("203.0.113.30", "secret"); got != http.StatusTooManyRequests {
		t.Fatalf("original client lock was cleared: got %d, want 429", got)
	}
}

func TestGetClientIP_ConfiguredProxy(t *testing.T) {
	previous := trustedProxyIPs
	trustedProxyIPs = []net.IP{net.ParseIP("192.168.1.10"), net.ParseIP("fd00::10"), net.ParseIP("fe80::10")}
	t.Cleanup(func() { trustedProxyIPs = previous })
	for _, tc := range []struct {
		name, peer, forwarded, realIP, want string
	}{
		{"configured IPv4", "192.168.1.10:12345", "10.0.0.1, 203.0.113.30", "", "203.0.113.30"},
		{"configured IPv6", "[fd00::10]:12345", "2001:db8::20", "", "2001:db8::20"},
		{"configured scoped IPv6", "[fe80::10%eth0]:12345", "2001:db8::20", "", "2001:db8::20"},
		{"mapped IPv4 peer", "[::ffff:192.168.1.10]:12345", "203.0.113.30", "", "203.0.113.30"},
		{"untrusted peer", "192.168.1.11:12345", "203.0.113.30", "203.0.113.30", "192.168.1.11"},
		{"missing header", "192.168.1.10:12345", "", "", "192.168.1.10"},
		{"real IP fallback", "192.168.1.10:12345", "", "203.0.113.30", "203.0.113.30"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/api/login", nil)
			r.RemoteAddr = tc.peer
			r.Header.Set("X-Forwarded-For", tc.forwarded)
			r.Header.Set("X-Real-IP", tc.realIP)
			if got := getClientIP(r); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestGetClientIP_UnconfiguredProxy(t *testing.T) {
	previous := trustedProxyIPs
	trustedProxyIPs = nil
	t.Cleanup(func() { trustedProxyIPs = previous })
	r := httptest.NewRequest(http.MethodPost, "/api/login", nil)
	r.RemoteAddr = "192.168.1.10:12345"
	r.Header.Set("X-Forwarded-For", "203.0.113.30")
	r.Header.Set("X-Real-IP", "203.0.113.30")
	if got := getClientIP(r); got != "192.168.1.10" {
		t.Fatalf("unconfigured proxy was trusted: got %q", got)
	}
}

func TestParseTrustedProxyIPs(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  int
		valid bool
	}{
		{"", 0, true},
		{"  ", 0, true},
		{"192.168.1.10", 1, true},
		{" 192.168.1.10 , fd00::10 ", 2, true},
		{"caddy.local", 0, false},
		{"192.168.1.0/24", 0, false},
		{"192.168.1.10,", 0, false},
	} {
		t.Run(tc.value, func(t *testing.T) {
			ips, err := parseTrustedProxyIPs(tc.value)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%t, err=%v", tc.valid, err)
			}
			if tc.valid && len(ips) != tc.want {
				t.Fatalf("got %d IPs, want %d", len(ips), tc.want)
			}
		})
	}
}
