package server_test

import (
	"crypto/tls"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"feeders/internal/auth"
	"feeders/internal/realtime"
	"feeders/internal/server"
	"feeders/internal/storage"
	"feeders/web"
)

func TestLANOriginsKeepSameOriginProtection(t *testing.T) {
	for _, tc := range []struct {
		name, host, origin string
		tls, allowed       bool
	}{
		{"LAN IPv4", "192.168.1.25:8080", "http://192.168.1.25:8080", false, true},
		{"LAN IPv6", "[fd00::25]:8080", "http://[fd00::25]:8080", false, true},
		{"LAN hostname", "flightdeck.local:8080", "http://flightdeck.local:8080", false, true},
		{"HTTPS", "ship.example", "https://ship.example", true, true},
		{"native client", "192.168.1.25:8080", "", false, true},
		{"other port", "192.168.1.25:8080", "http://192.168.1.25:8081", false, false},
		{"other host", "192.168.1.25:8080", "http://evil.example", false, false},
		{"other scheme", "192.168.1.25:8080", "https://192.168.1.25:8080", false, false},
		{"opaque origin", "192.168.1.25:8080", "null", false, false},
		{"credentials", "192.168.1.25:8080", "http://alice@192.168.1.25:8080", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "http://"+tc.host+"/api/auth/login", nil)
			r.Header.Set("Origin", tc.origin)
			if tc.tls {
				r.TLS = &tls.ConnectionState{}
			}
			if got := server.SameOrigin(r); got != tc.allowed {
				t.Fatalf("SameOrigin=%v, want %v", got, tc.allowed)
			}
		})
	}
}

// Exercise the real handlers without a TCP listener. WebSocket frame exchange
// is covered by the existing integration suite when network sockets are allowed.
func TestLANHTTPAuthenticationAndWebSocketGate(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	repo, err := storage.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a, err := auth.New(repo, time.Hour, false, log)
	if err != nil {
		t.Fatal(err)
	}
	hub := realtime.New(a, log)
	handler := server.New(web.Files, a, hub)
	const base = "http://192.168.1.25:8080"
	request := func(method, path, origin, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, base+path, strings.NewReader(body))
		r.Header.Set("Origin", origin)
		r.Header.Set("Content-Type", "application/json")
		if cookie != nil {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	registered := request("POST", "/api/auth/register", base, `{"username":"lan-player","password":"disposable-password"}`, nil)
	if registered.Code != http.StatusCreated {
		t.Fatalf("LAN registration: %d %s", registered.Code, registered.Body.String())
	}
	cookies := registered.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Secure || !cookies[0].HttpOnly || cookies[0].Domain != "" || cookies[0].Path != "/" || cookies[0].SameSite != http.SameSiteLaxMode {
		t.Fatalf("Cookie is unsuitable for HTTP LAN authentication: %+v", cookies)
	}
	cookie := cookies[0]
	for _, tc := range []struct {
		method, path, origin string
		cookie               *http.Cookie
		status               int
	}{
		{"GET", "/api/auth/me", base, cookie, 200},
		{"GET", "/api/auth/me", base, nil, 401},
		{"GET", "/ws", base, nil, 401},
		{"GET", "/ws", "http://evil.example", cookie, 403},
		// No Upgrade headers: reaching 400 verifies auth/origin passed without
		// trying to hijack a real socket or weakening the handshake rules.
		{"GET", "/ws", base, cookie, 400},
		{"POST", "/api/auth/logout", "http://evil.example", cookie, 403},
		{"GET", "/api/auth/me", base, cookie, 200},
		{"POST", "/api/auth/logout", base, cookie, 200},
		{"GET", "/api/auth/me", base, cookie, 401},
	} {
		w := request(tc.method, tc.path, tc.origin, "{}", tc.cookie)
		if w.Code != tc.status {
			t.Fatalf("%s %s origin=%s: %d, want %d: %s", tc.method, tc.path, tc.origin, w.Code, tc.status, w.Body.String())
		}
	}
}
