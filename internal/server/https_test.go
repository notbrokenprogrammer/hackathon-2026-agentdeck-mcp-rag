package server_test

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"io"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"feeders/internal/auth"
	"feeders/internal/game"
	"feeders/internal/model"
	"feeders/internal/realtime"
	"feeders/internal/server"
	"feeders/internal/storage"
	"feeders/pkg/protocol"
	"feeders/web"
	"github.com/gorilla/websocket"
)

func TestHTTPSTrustBoundary(t *testing.T) {
	const public = "https://crew.example.test"
	for _, tc := range []struct {
		name, peer, host, proto, origin string
		trust, tls                      bool
		status                          int
	}{
		{"local TLS terminator", "127.0.0.1:4411", "crew.example.test", "https", public, true, false, 204},
		{"IPv6 TLS terminator", "[::1]:4411", "crew.example.test", "https", public, true, false, 204},
		{"proxy requires opt in", "127.0.0.1:4411", "crew.example.test", "https", public, false, false, 403},
		{"LAN cannot spoof proxy", "192.168.1.9:4411", "crew.example.test", "https", public, true, false, 403},
		{"wrong origin blocked", "127.0.0.1:4411", "crew.example.test", "https", "https://evil.example", true, false, 403},
		{"wrong host cannot upgrade", "127.0.0.1:4411", "localhost:8080", "https", public, true, false, 403},
		{"multiple proto values rejected", "127.0.0.1:4411", "crew.example.test", "https,http", public, true, false, 403},
		{"public plaintext POST refused", "127.0.0.1:4411", "crew.example.test", "http", public, true, false, 403},
		{"native TLS", "192.168.1.9:4411", "crew.example.test", "", public, false, true, 204},
		{"HTTP LAN still works", "192.168.1.9:4411", "192.168.1.25:8080", "https", "http://192.168.1.25:8080", true, false, 204},
	} {
		t.Run(tc.name, func(t *testing.T) {
			access, err := server.NewPublicAccess(public, tc.trust)
			if err != nil {
				t.Fatal(err)
			}
			handler := access.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !server.SameOrigin(r) {
					w.WriteHeader(403)
					return
				}
				if tc.name == "HTTP LAN still works" && server.IsHTTPS(r) {
					t.Error("forged header upgraded plaintext LAN")
				}
				w.WriteHeader(204)
			}))
			r := httptest.NewRequest("POST", "http://"+tc.host+"/api/auth/login", nil)
			r.RemoteAddr = tc.peer
			r.Header.Set("Origin", tc.origin)
			r.Header.Set("X-Forwarded-Proto", tc.proto)
			r.Header.Set("X-Forwarded-Host", "crew.example.test")
			r.Header.Set("X-Forwarded-For", "127.0.0.1")
			if tc.tls {
				r.TLS = &tls.ConnectionState{}
			}
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Fatalf("status %d want %d", w.Code, tc.status)
			}
		})
	}
	access, _ := server.NewPublicAccess(public, true)
	handler := access.Wrap(server.New(web.Files))
	for _, path := range []string{"/api/internal/experiment", "/api/internal/", "/api/internal"} {
		r := httptest.NewRequest("GET", public+path, nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != 404 {
			t.Fatalf("private tool exposed: %s status %d", path, w.Code)
		}
	}
	r := httptest.NewRequest("GET", "http://localhost:8080/api/host", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	var result struct {
		URLs   []string `json:"urls"`
		Public string   `json:"public_url"`
	}
	json.Unmarshal(w.Body.Bytes(), &result)
	if len(result.URLs) != 1 || result.URLs[0] != public || result.Public != public {
		t.Fatal("HTTPS invitation not available to local host", w.Body.String())
	}
	r = httptest.NewRequest("GET", "http://crew.example.test/?room=ABC123", nil)
	r.RemoteAddr = "127.0.0.1:9911"
	r.Header.Set("X-Forwarded-Proto", "http")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 308 || w.Header().Get("Location") != public+"/?room=ABC123" {
		t.Fatal("HTTPS redirect lost invite")
	}
}

func testTLS(t *testing.T) (*tls.Config, *tls.Config) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	cert := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "crew.example.test"}, DNSNames: []string{"crew.example.test"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, BasicConstraintsValid: true}
	der, err := x509.CreateCertificate(rand.Reader, cert, cert, pub, priv)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(parsed)
	return &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: priv}}}, &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots, ServerName: "crew.example.test"}
}

// This connection performs an actual verified TLS handshake and WebSocket
// upgrade over net.Pipe. No insecure TLS option, DNS or listening socket.
func securePipePlayer(t *testing.T, handler http.Handler, user model.User, cookie *http.Cookie, proxy bool) *pipePlayer {
	t.Helper()
	sc, cc := testTLS(t)
	client, host := net.Pipe()
	front := tls.Client(client, cc)
	back := tls.Server(host, sc)
	t.Cleanup(func() { client.Close(); host.Close() })
	client.SetDeadline(time.Now().Add(4 * time.Second))
	host.SetDeadline(time.Now().Add(4 * time.Second))
	go func() {
		reader := bufio.NewReader(back)
		r, err := http.ReadRequest(reader)
		if err != nil {
			return
		}
		r.RemoteAddr = "192.168.1.90:55000"
		state := back.ConnectionState()
		r.TLS = &state
		if proxy {
			r.TLS = nil
			r.RemoteAddr = "127.0.0.1:55000"
			r.Header.Set("X-Forwarded-Proto", "https")
		}
		host.SetDeadline(time.Time{})
		handler.ServeHTTP(&pipeResponseWriter{conn: back, reader: reader, header: make(http.Header)}, r)
	}()
	if err := front.Handshake(); err != nil {
		t.Fatal("verified TLS", err)
	}
	// NewClient uses the already-encrypted connection; avoid wrapping TLS twice.
	endpoint, _ := url.Parse("ws://crew.example.test/ws")
	conn, res, err := websocket.NewClient(front, endpoint, http.Header{"Origin": {"https://crew.example.test"}, "Cookie": {cookie.Name + "=" + cookie.Value}}, 4096, 4096)
	if err != nil {
		t.Fatal("TLS WebSocket upgrade", err)
	}
	if res.StatusCode != 101 {
		t.Fatal(res.StatusCode)
	}
	client.SetDeadline(time.Time{})
	p := &pipePlayer{conn: conn, user: user, events: make(chan protocol.Envelope, 256), errors: make(chan error, 1)}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			var packet protocol.Envelope
			if err := conn.ReadJSON(&packet); err != nil {
				p.errors <- err
				return
			}
			select {
			case p.events <- packet:
			case <-time.After(time.Second):
				p.errors <- io.ErrShortBuffer
				return
			}
		}
	}()
	t.Cleanup(func() {
		conn.Close()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Error("TLS WS reader did not stop")
		}
	})
	return p
}

func TestHTTPSInMemoryAuthAndMultiplayer(t *testing.T) {
	for _, proxy := range []bool{false, true} {
		name := "direct-TLS"
		if proxy {
			name = "trusted-local-TLS-proxy"
		}
		t.Run(name, func(t *testing.T) {
			log := slog.New(slog.NewTextHandler(io.Discard, nil))
			repo, err := storage.NewSQLiteStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { repo.Close() })
			a, err := auth.New(repo, time.Hour, false, log)
			if err != nil {
				t.Fatal(err)
			}
			hub := realtime.New(a, log)
			loop := game.NewServerLoop(repo, hub, log)
			module := game.New(repo, loop, nil, log)
			module.Register(hub)
			ctx, cancel := context.WithCancel(context.Background())
			loop.Start(ctx)
			t.Cleanup(func() {
				cancel()
				loop.Stop()
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				hub.Close(ctx)
			})
			access, _ := server.NewPublicAccess("https://crew.example.test", proxy)
			handler := access.Wrap(server.New(web.Files, a, hub))
			register := func(name string) *pipePlayer {
				request := httptest.NewRequest("POST", "https://crew.example.test/api/auth/register", strings.NewReader(`{"username":"`+name+`","password":"temporary-HTTPS-test-password"}`))
				request.Header.Set("Origin", "https://crew.example.test")
				request.RemoteAddr = "192.168.1.90:55000"
				if proxy {
					request.TLS = nil
					request.RemoteAddr = "127.0.0.1:55000"
					request.Header.Set("X-Forwarded-Proto", "https")
				}
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, request)
				if response.Code != 201 {
					t.Fatalf("HTTPS registration %d %s", response.Code, response.Body.String())
				}
				cookies := response.Result().Cookies()
				if len(cookies) != 1 || !cookies[0].Secure || !cookies[0].HttpOnly || cookies[0].Domain != "" {
					t.Fatal("HTTPS session cookie", cookies)
				}
				var result struct {
					User model.User `json:"user"`
				}
				json.Unmarshal(response.Body.Bytes(), &result)
				p := securePipePlayer(t, handler, result.User, cookies[0], proxy)
				p.command(t, "player.init", map[string]any{})
				return p
			}
			p1 := register("secure-captain")
			p2 := register("secure-engineer")
			type room struct {
				Code    string             `json:"code"`
				Members []game.RoomMember  `json:"members"`
				Chat    []game.ChatMessage `json:"chat"`
			}
			created := pipeDecode[room](t, p1.command(t, "room.create", map[string]string{"name": "HTTPS crew"}))
			joined := pipeDecode[room](t, p2.command(t, "room.join", map[string]string{"code": created.Code}))
			if joined.Code != created.Code || len(joined.Members) != 2 {
				t.Fatal("HTTPS room join")
			}
			p1.command(t, "chat.send", map[string]string{"text": "Связь по HTTPS работает"})
			p2.receive(t, func(e protocol.Envelope) bool {
				if e.Type != "room.state" {
					return false
				}
				r := pipeDecode[room](t, e)
				return len(r.Chat) == 1 && r.Chat[0].Text == "Связь по HTTPS работает"
			})
			p2.command(t, "chat.send", map[string]string{"text": "Второй игрок на связи"})
			p1.receive(t, func(e protocol.Envelope) bool {
				if e.Type != "room.state" {
					return false
				}
				r := pipeDecode[room](t, e)
				return len(r.Chat) == 2 && r.Chat[1].Text == "Второй игрок на связи"
			})
		})
	}
}
