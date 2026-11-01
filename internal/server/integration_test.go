package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
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

type host struct {
	server *httptest.Server
	hub    *realtime.Hub
	repo   *storage.FileStore
	auth   *auth.Handler
	client *http.Client
}

func newHost(t *testing.T, dir string, ttl time.Duration) *host {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	repo, err := storage.NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	a, err := auth.New(repo, ttl, false, log)
	if err != nil {
		t.Fatal(err)
	}
	h := realtime.New(a, log)
	game.New(repo, nil, nil, log).Register(h)
	s := httptest.NewServer(server.New(web.Files, a, h))
	jar, _ := cookiejar.New(nil)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := h.Close(ctx); err != nil {
			t.Error(err)
		}
		s.Close()
	})
	return &host{s, h, repo, a, &http.Client{Jar: jar, Timeout: 5 * time.Second}}
}

func (h *host) post(t *testing.T, path, body string, want int) *http.Response {
	t.Helper()
	res, err := h.client.Post(h.server.URL+path, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { res.Body.Close() })
	if res.StatusCode != want {
		b, _ := io.ReadAll(res.Body)
		t.Fatalf("%s: status %d want %d: %s", path, res.StatusCode, want, b)
	}
	return res
}

func (h *host) dial(t *testing.T, origin string, authenticated bool) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	headers := http.Header{}
	if origin != "" {
		headers.Set("Origin", origin)
	}
	if authenticated {
		u, _ := url.Parse(h.server.URL)
		for _, c := range h.client.Jar.Cookies(u) {
			headers.Add("Cookie", c.String())
		}
	}
	return websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(h.server.URL, "http")+"/ws", headers)
}

func exchange(t *testing.T, conn *websocket.Conn, kind string, payload any) protocol.Envelope {
	t.Helper()
	msg, err := protocol.Message(kind, "req-1", payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.WriteJSON(msg); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	var response protocol.Envelope
	if err := conn.ReadJSON(&response); err != nil {
		t.Fatal(err)
	}
	if response.RequestID != "req-1" || response.ProtocolVersion != 1 {
		t.Fatalf("bad envelope: %+v", response)
	}
	return response
}

const credentials = `{"username":"Explorer","password":"secret123"}`

func TestAuthWebSocketAndPersistence(t *testing.T) {
	dir := t.TempDir()
	h := newHost(t, dir, time.Hour)
	_, res, err := h.dial(t, h.server.URL, false)
	if err == nil || res == nil || res.StatusCode != 401 {
		t.Fatalf("unauthenticated upgrade: %v %+v", err, res)
	}
	res.Body.Close()
	reg := h.post(t, "/api/auth/register", credentials, 201)
	var registered struct {
		User model.User `json:"user"`
	}
	if err := json.NewDecoder(reg.Body).Decode(&registered); err != nil {
		t.Fatal(err)
	}
	cookies := reg.Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteLaxMode || cookies[0].Path != "/" {
		t.Fatalf("cookie: %+v", cookies)
	}
	h.post(t, "/api/auth/register", credentials, 409)
	h.post(t, "/api/auth/login", `{"username":"explorer","password":"wrongpass"}`, 401)
	h.post(t, "/api/auth/login", credentials, 200)
	account, err := h.repo.FindAccount(context.Background(), "explorer")
	if err != nil || bytes.Contains(account.PasswordHash, []byte("secret123")) {
		t.Fatalf("password storage: %v", err)
	}
	_, res, err = h.dial(t, "http://evil.example", true)
	if err == nil || res.StatusCode != 403 {
		t.Fatalf("origin accepted: %v", err)
	}
	res.Body.Close()
	conn, _, err := h.dial(t, h.server.URL, true)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_, res, err = h.dial(t, h.server.URL, true)
	if err == nil || res.StatusCode != 409 {
		t.Fatalf("duplicate connection accepted: %v", err)
	}
	res.Body.Close()
	init := exchange(t, conn, "player.init", map[string]any{"username": "spoofed", "client_time": 123})
	if init.Type != "player.ready" {
		t.Fatalf("init: %+v", init)
	}
	var ready protocol.PlayerReady
	if err := json.Unmarshal(init.Payload, &ready); err != nil {
		t.Fatal(err)
	}
	if ready.User.ID != registered.User.ID || ready.User.Username != "explorer" || ready.Capabilities.Movement {
		t.Fatalf("ready: %+v", ready)
	}
	if r := exchange(t, conn, "ping", map[string]int{"timestamp": 123}); r.Type != "pong" {
		t.Fatal(r)
	}
	if r := exchange(t, conn, "player.input", protocol.PlayerInput{Seq: 1, Y: 1.7}); r.Type != "error" || !bytes.Contains(r.Payload, []byte("feature_disabled")) {
		t.Fatal(r)
	}
	if r := exchange(t, conn, "unknown", map[string]any{}); !bytes.Contains(r.Payload, []byte("unknown_type")) {
		t.Fatal(r)
	}
	if !h.hub.Publish(registered.User.ID, "test.event", map[string]bool{"ok": true}) {
		t.Fatal("publish failed")
	}
	var event protocol.Envelope
	if err := conn.ReadJSON(&event); err != nil || event.Type != "test.event" || event.RequestID != "" {
		t.Fatalf("event: %+v %v", event, err)
	}
	h.post(t, "/api/auth/logout", `{}`, 200)
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, _, err := conn.ReadMessage(); err == nil {
		t.Fatal("socket survived logout")
	}
	// A new server uses persisted account and player records, but not old sessions.
	h2 := newHost(t, dir, time.Hour)
	h2.post(t, "/api/auth/login", credentials, 200)
	state, err := h2.repo.LoadPlayer(context.Background(), registered.User.ID)
	if err != nil || state.PlayerID != registered.User.ID {
		t.Fatalf("persisted state: %+v %v", state, err)
	}
}

func TestHTTPStaticAndValidation(t *testing.T) {
	h := newHost(t, t.TempDir(), time.Hour)
	for _, tc := range []struct {
		path, accept string
		status       int
		contains     string
	}{
		{"/healthz", "", 200, `"status":"ok"`}, {"/", "", 200, "Последний экипаж"},
		{"/vendor/three.module.js", "", 200, "REVISION"}, {"/room/history", "text/html", 200, "/src/main.js"},
		{"/missing.js", "text/html", 404, ""}, {"/api/missing", "text/html", 404, "not_found"},
		{"/vendor/", "", 404, ""}, {"/embed.go", "", 404, ""},
	} {
		req, _ := http.NewRequest("GET", h.server.URL+tc.path, nil)
		req.Header.Set("Accept", tc.accept)
		res, err := h.client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != tc.status || !strings.Contains(string(body), tc.contains) {
			t.Errorf("%s: status=%d body-prefix=%.100s", tc.path, res.StatusCode, body)
		}
	}
	h.post(t, "/api/auth/register", `{"username":"a","password":"x"}`, 400)
	h.post(t, "/api/auth/register", credentials+`{}`, 400)
	req, _ := http.NewRequest("POST", h.server.URL+"/api/auth/register", strings.NewReader(credentials))
	req.Header.Set("Origin", "http://evil.example")
	res, err := h.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 403 {
		t.Fatal(res.StatusCode)
	}
}

func TestSessionExpiryAndShutdown(t *testing.T) {
	h := newHost(t, t.TempDir(), 150*time.Millisecond)
	// Capture the cookie manually: browser MaxAge is measured in whole seconds.
	res := h.post(t, "/api/auth/register", credentials, 201)
	r, _ := http.NewRequest("GET", h.server.URL+"/ws", nil)
	r.AddCookie(res.Cookies()[0])
	if _, ok := h.auth.Resolve(r); !ok {
		t.Fatal("new session invalid")
	}
	time.Sleep(200 * time.Millisecond)
	if _, ok := h.auth.Resolve(r); ok {
		t.Fatal("expired session accepted")
	}
	h2 := newHost(t, t.TempDir(), time.Hour)
	h2.post(t, "/api/auth/register", credentials, 201)
	conn, _, err := h2.dial(t, h2.server.URL, true)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := h2.hub.Close(ctx); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	if _, _, err := conn.ReadMessage(); err == nil {
		t.Fatal("socket survived shutdown")
	}
}
