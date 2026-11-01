package server_test

import (
	"context"
	"encoding/json"
	"feeders/internal/auth"
	"feeders/internal/game"
	"feeders/internal/model"
	"feeders/internal/realtime"
	"feeders/internal/server"
	"feeders/internal/storage"
	"feeders/pkg/protocol"
	"feeders/web"
	"github.com/gorilla/websocket"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func sqliteHost(t *testing.T, dir string) (*httptest.Server, func()) {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	store, err := storage.NewSQLiteStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	a, err := auth.New(store, time.Hour, false, log)
	if err != nil {
		t.Fatal(err)
	}
	hub := realtime.New(a, log)
	loop := game.NewServerLoop(store, hub, log)
	module := game.New(store, loop, nil, log)
	if err = module.WithPersistence(store); err != nil {
		t.Fatal(err)
	}
	if err = module.WithWorld(filepath.Join("..", "..", "world"), a); err != nil {
		t.Fatal(err)
	}
	loop.ConfigureWorld(module.RoomOf, module.Walkable)
	module.Register(hub)
	ctx, cancel := context.WithCancel(context.Background())
	loop.Start(ctx)
	s := httptest.NewServer(server.New(web.Files, a, hub, module))
	var once sync.Once
	close := func() {
		once.Do(func() {
			cancel()
			loop.Stop()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if err := hub.Close(ctx); err != nil {
				t.Error(err)
			}
			s.Close()
			store.Close()
		})
	}
	t.Cleanup(close)
	return s, close
}
func reqPacket(t *testing.T, c *websocket.Conn, kind, id string, p any) protocol.Envelope {
	t.Helper()
	sendPacket(t, c, kind, id, p)
	return readPacket(t, c, func(e protocol.Envelope) bool { return e.RequestID == id })
}
func mustReply(t *testing.T, c *websocket.Conn, kind, id string, p any) protocol.Envelope {
	t.Helper()
	e := reqPacket(t, c, kind, id, p)
	if e.Type == "error" {
		t.Fatalf("%s: %s", kind, e.Payload)
	}
	return e
}
func TestReleaseTwoSocketsIsolationAndSQLiteRestart(t *testing.T) {
	dir := t.TempDir()
	s, close := sqliteHost(t, dir)
	a, clientA := registerSocket(t, s, "alice")
	b, clientB := registerSocket(t, s, "boris")
	c, _ := registerSocket(t, s, "claire")
	for i, socket := range []*websocket.Conn{a, b, c} {
		mustReply(t, socket, "player.init", string(rune('a'+i)), map[string]any{})
	}
	e := mustReply(t, a, "room.create", "create", map[string]string{"name": "Сохранённая экспедиция"})
	var room struct {
		Code string `json:"code"`
	}
	json.Unmarshal(e.Payload, &room)
	if room.Code == "" {
		t.Fatal("missing room code")
	}
	mustReply(t, b, "room.join", "join", map[string]string{"code": room.Code})
	mustReply(t, c, "room.create", "other", map[string]any{})
	mustReply(t, a, "room.role", "captain", map[string]string{"role": "captain"})
	mustReply(t, b, "room.role", "engineer", map[string]string{"role": "engineer"})
	for i, socket := range []*websocket.Conn{a, b} {
		mustReply(t, socket, "room.ready", "ready"+string(rune('a'+i)), map[string]bool{"ready": true})
	}
	mustReply(t, a, "room.start", "start", map[string]any{})
	snapshot := readPacket(t, b, func(e protocol.Envelope) bool {
		if e.Type != "world.snapshot" {
			return false
		}
		var snap protocol.WorldSnapshot
		json.Unmarshal(e.Payload, &snap)
		return len(snap.Players) == 2
	})
	var world protocol.WorldSnapshot
	json.Unmarshal(snapshot.Payload, &world)
	for _, p := range world.Players {
		if p.Username == "claire" {
			t.Fatal("other room avatar leaked")
		}
	}
	e = mustReply(t, b, "game.action", "repair-stable-id", map[string]any{"action": "repair_cooling", "expected_version": 1})
	var state game.GameState
	json.Unmarshal(e.Payload, &state)
	if !state.CoolingRepaired || state.Energy != 25 || state.Version != 2 {
		t.Fatal(state)
	}
	mustReply(t, b, "game.action", "repair-stable-id", map[string]any{"action": "repair_cooling", "expected_version": 1})
	e = reqPacket(t, a, "game.action", "stale", map[string]any{"action": "use_battery", "expected_version": 1})
	if e.Type != "error" {
		t.Fatal("stale command accepted")
	}
	mustReply(t, a, "chat.send", "chat", map[string]string{"text": "Охлаждение восстановлено, включаем реактор."})
	e = readPacket(t, b, func(e protocol.Envelope) bool {
		if e.Type != "room.state" {
			return false
		}
		var data struct {
			Chat []game.ChatMessage `json:"chat"`
		}
		json.Unmarshal(e.Payload, &data)
		return len(data.Chat) > 0
	})
	if len(e.Payload) == 0 {
		t.Fatal("chat not delivered")
	}
	e = mustReply(t, c, "room.get", "other-check", map[string]any{})
	var other struct {
		Chat []game.ChatMessage `json:"chat"`
	}
	json.Unmarshal(e.Payload, &other)
	if len(other.Chat) != 0 {
		t.Fatal("chat crossed rooms")
	}
	res, err := clientA.Get(s.URL + "/api/world/interest")
	if err != nil {
		t.Fatal(err)
	}
	var plan game.WorldPlan
	json.NewDecoder(res.Body).Decode(&plan)
	res.Body.Close()
	sum := 0
	for _, r := range plan.Resources {
		sum += r.Size
	}
	if sum > plan.Budget || len(plan.Resources) == 0 {
		t.Fatal("invalid world plan")
	}
	res, err = clientA.Get(s.URL + "/api/world/resource/" + plan.Resources[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != 200 || res.Header.Get("Cache-Control") != "no-store" {
		t.Fatal(res.Status, res.Header)
	}
	res.Body.Close()
	res, _ = clientA.Get(s.URL + "/api/world/resource/geometries_000000000000000000000000")
	if res.StatusCode != 403 {
		t.Fatal("unadmitted resource allowed")
	}
	res.Body.Close()
	a.Close()
	b.Close()
	c.Close()
	close()
	again, _ := sqliteHost(t, dir)
	res, err = clientB.Get(again.URL + "/api/auth/me")
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusOK {
		t.Fatal("session not restored", res.Status)
	}
	var profile struct {
		User model.User `json:"user"`
	}
	json.NewDecoder(res.Body).Decode(&profile)
	res.Body.Close()
	if profile.User.Username != "boris" {
		t.Fatal(profile)
	}
	reconnected := reconnectSocket(t, again, clientB)
	mustReply(t, reconnected, "player.init", "restart-init", map[string]any{})
	e = mustReply(t, reconnected, "room.get", "restored-room", map[string]any{})
	var restored struct {
		Code string `json:"code"`
	}
	json.Unmarshal(e.Payload, &restored)
	if restored.Code != room.Code {
		t.Fatal("room lost")
	}
	e = mustReply(t, reconnected, "game.action", "repair-stable-id", map[string]any{"action": "repair_cooling", "expected_version": 1})
	json.Unmarshal(e.Payload, &state)
	if state.Version != 2 || state.Energy != 25 {
		t.Fatal("durable receipt failed", state)
	}
}
