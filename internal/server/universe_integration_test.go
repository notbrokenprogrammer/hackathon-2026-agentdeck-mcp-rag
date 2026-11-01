package server_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"feeders/internal/auth"
	"feeders/internal/game"
	"feeders/internal/realtime"
	"feeders/internal/server"
	"feeders/internal/storage"
	"feeders/pkg/protocol"
	"feeders/web"
	"github.com/gorilla/websocket"
)

func persistedUniverseHost(t *testing.T, dir string) (*httptest.Server, func()) {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	repo, err := storage.NewSQLiteStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	a, err := auth.New(repo, time.Hour, false, log)
	if err != nil {
		t.Fatal(err)
	}
	hub := realtime.New(a, log)
	loop := game.NewServerLoop(repo, hub, log)
	module := game.New(repo, loop, nil, log)
	if err := module.WithPersistence(repo); err != nil {
		t.Fatal(err)
	}
	loop.ConfigureWorld(module.RoomOf, module.Walkable)
	module.Register(hub)
	ctx, cancel := context.WithCancel(context.Background())
	loop.Start(ctx)
	host := httptest.NewServer(server.New(web.Files, a, hub))
	var once sync.Once
	stop := func() {
		once.Do(func() {
			cancel()
			loop.Stop()
			ctx, done := context.WithTimeout(context.Background(), 3*time.Second)
			defer done()
			if err := hub.Close(ctx); err != nil {
				t.Error(err)
			}
			host.Close()
			if err := repo.Close(); err != nil {
				t.Error(err)
			}
		})
	}
	t.Cleanup(stop)
	return host, stop
}
func universeExchange(t *testing.T, c *websocket.Conn, kind, id string, p any) protocol.Envelope {
	t.Helper()
	sendPacket(t, c, kind, id, p)
	return readPacket(t, c, func(e protocol.Envelope) bool { return e.RequestID == id })
}
func wireUniverse(t *testing.T, e protocol.Envelope) game.Universe {
	t.Helper()
	if e.Type != "universe.state" {
		t.Fatalf("expected universe.state, got %s %s", e.Type, e.Payload)
	}
	var state game.Universe
	if err := json.Unmarshal(e.Payload, &state); err != nil {
		t.Fatal(err)
	}
	var scope struct {
		RoomCode   string `json:"room_code"`
		Generation int    `json:"generation"`
	}
	if err := json.Unmarshal(e.Payload, &scope); err != nil || scope.RoomCode == "" || scope.Generation < 1 {
		t.Fatal("unscoped universe state", string(e.Payload))
	}
	return state
}
func awaitUniverse(t *testing.T, c *websocket.Conn, version uint64) game.Universe {
	t.Helper()
	e := readPacket(t, c, func(e protocol.Envelope) bool {
		if e.Type != "universe.state" {
			return false
		}
		var state game.Universe
		json.Unmarshal(e.Payload, &state)
		return state.Version == version
	})
	return wireUniverse(t, e)
}
func wireError(t *testing.T, e protocol.Envelope, code string) {
	t.Helper()
	var p protocol.Error
	json.Unmarshal(e.Payload, &p)
	if e.Type != "error" || p.Code != code {
		t.Fatalf("want %s, got %s %s", code, e.Type, e.Payload)
	}
}
func prepareUniverseRoom(t *testing.T, c *websocket.Conn, prefix string) string {
	t.Helper()
	e := universeExchange(t, c, "room.create", prefix+"-create", map[string]any{})
	if e.Type != "room.state" {
		t.Fatal(e.Type, string(e.Payload))
	}
	var room struct {
		Code string `json:"code"`
	}
	json.Unmarshal(e.Payload, &room)
	for _, cmd := range []struct {
		kind string
		p    any
	}{{"room.role", map[string]string{"role": "commander"}}, {"room.ready", map[string]bool{"ready": true}}, {"room.start", map[string]any{}}} {
		got := universeExchange(t, c, cmd.kind, prefix+cmd.kind, cmd.p)
		if got.Type == "error" {
			t.Fatal(got.Type, string(got.Payload))
		}
	}
	return room.Code
}

func TestUniverseRealSocketsReplicationAuthorizationAndRestart(t *testing.T) {
	dir := t.TempDir()
	host, stop := persistedUniverseHost(t, dir)
	owner, ownerHTTP := registerSocket(t, host, "universe-owner")
	peer, _ := registerSocket(t, host, "universe-peer")
	other, otherHTTP := registerSocket(t, host, "universe-other")
	clients := []*websocket.Conn{owner, peer, other}
	ids := []string{"owner-init", "peer-init", "other-init"}
	var ownerID string
	for i, c := range clients {
		e := universeExchange(t, c, "player.init", ids[i], map[string]any{})
		if e.Type != "player.ready" {
			t.Fatal(e.Type)
		}
		if i == 0 {
			var ready protocol.PlayerReady
			json.Unmarshal(e.Payload, &ready)
			ownerID = ready.User.ID
		}
	}
	code := prepareUniverseRoom(t, owner, "owner")
	got := universeExchange(t, peer, "room.join", "peer-join", map[string]string{"code": code})
	if got.Type != "room.state" {
		t.Fatal(got.Type, string(got.Payload))
	}
	otherCode := prepareUniverseRoom(t, other, "other")
	initialNPCCount := wireUniverse(t, universeExchange(t, owner, "universe.get", "initial-settings", map[string]any{})).Settings.NPCCount
	admin := map[string]any{"expected_version": 1, "settings": map[string]any{"npc_count": 96, "light_intensity": 1.4}}
	wireError(t, universeExchange(t, peer, "universe.admin", "peer-denied", admin), "forbidden")
	state := wireUniverse(t, universeExchange(t, owner, "universe.admin", "owner-edit", admin))
	if state.Version != 2 || state.Settings.NPCCount != 96 {
		t.Fatal("owner update failed", state)
	}
	replicated := awaitUniverse(t, peer, 2)
	if replicated.Settings.NPCCount != 96 || replicated.Settings.LightIntensity != 1.4 {
		t.Fatal("peer did not receive environment edit")
	}
	initialObjectCount := len(state.Objects)
	interaction := func(version int, action string) map[string]any {
		return map[string]any{"expected_version": version, "object_id": "crate-cryo", "action": action}
	}
	wireUniverse(t, universeExchange(t, owner, "universe.interact", "pickup", interaction(2, "pickup")))
	held := awaitUniverse(t, peer, 3)
	if held.Objects[1].HeldBy != ownerID {
		t.Fatal("holder identity was not replicated")
	}
	wireError(t, universeExchange(t, peer, "universe.interact", "contention", interaction(3, "pickup")), "invalid_action")
	wireError(t, universeExchange(t, peer, "universe.interact", "not-my-crate", interaction(3, "drop")), "forbidden")
	wireUniverse(t, universeExchange(t, owner, "universe.interact", "drop", interaction(3, "drop")))
	dropped := awaitUniverse(t, peer, 4)
	if dropped.Objects[1].HeldBy != "" || dropped.Objects[1].X != -15 || dropped.Objects[1].Z != 1.2 {
		t.Fatal("server-derived drop position was not replicated", dropped.Objects[1])
	}
	command := map[string]string{"agent_id": "savinykh", "text": "Добавь растения"}
	if e := universeExchange(t, owner, "agent.talk", "grow-plants", command); e.Type != "agent.reply" {
		t.Fatal(e.Type, string(e.Payload))
	}
	generated := awaitUniverse(t, peer, 5)
	if len(generated.Objects) != initialObjectCount+4 {
		t.Fatal("agent did not generate scene objects", len(generated.Objects))
	}
	// Network retries must not execute a world-changing chat instruction twice.
	universeExchange(t, owner, "agent.talk", "grow-plants", command)
	current := wireUniverse(t, universeExchange(t, owner, "universe.get", "after-retry", map[string]any{}))
	if current.Version != 5 || len(current.Objects) != initialObjectCount+4 {
		t.Fatal("chat replay duplicated objects")
	}
	universeExchange(t, owner, "universe.admin", "guard", map[string]any{"expected_version": 5, "agent": map[string]string{"id": "savinykh", "behavior": "guard"}})
	awaitUniverse(t, peer, 6)
	wireError(t, universeExchange(t, peer, "universe.command", "guard-denied", map[string]any{"expected_version": 6, "agent_id": "savinykh", "text": "Добавь растения"}), "command_rejected")

	sample := map[string]any{"fps": 58.5, "ping_ms": 21, "frame_ms": 17.1, "draw_calls": 80, "triangles": 18000, "renderer": "WebGL", "hidden": false}
	if e := universeExchange(t, peer, "telemetry.report", "sample", sample); e.Type != "telemetry.accepted" {
		t.Fatal(e.Type, string(e.Payload))
	}
	wireError(t, universeExchange(t, peer, "telemetry.get", "private-metrics", map[string]any{}), "forbidden")
	metrics := universeExchange(t, owner, "telemetry.get", "owner-metrics", map[string]any{})
	var telemetry struct {
		Clients []struct {
			Name   string   `json:"name"`
			FPS    *float64 `json:"fps"`
			PingMS *float64 `json:"ping_ms"`
			Stale  bool     `json:"stale"`
			Online bool     `json:"online"`
		} `json:"clients"`
	}
	if metrics.Type != "telemetry.state" || json.Unmarshal(metrics.Payload, &telemetry) != nil || len(telemetry.Clients) != 2 {
		t.Fatal("owner metrics missing clients", string(metrics.Payload))
	}
	found := false
	for _, client := range telemetry.Clients {
		if client.Name == "universe-peer" {
			found = client.FPS != nil && *client.FPS == 58.5 && client.PingMS != nil && *client.PingMS == 21 && !client.Stale && client.Online
		}
	}
	if !found {
		t.Fatal("actual reported sample not visible to owner", string(metrics.Payload))
	}
	// Drain the other room up to a response on its own socket; a broadcast leak is
	// observable without timeouts, which would poison the WebSocket reader.
	sendPacket(t, other, "universe.get", "isolation-check", map[string]any{})
	for {
		var e protocol.Envelope
		other.SetReadDeadline(time.Now().Add(3 * time.Second))
		if err := other.ReadJSON(&e); err != nil {
			t.Fatal(err)
		}
		if e.Type == "universe.state" {
			u := wireUniverse(t, e)
			if u.Version != 1 || u.Settings.NPCCount != initialNPCCount || len(u.Objects) != initialObjectCount {
				t.Fatal("another room received edited universe", u)
			}
		}
		if e.RequestID == "isolation-check" {
			break
		}
	}
	otherMetrics := universeExchange(t, other, "telemetry.get", "other-metrics", map[string]any{})
	json.Unmarshal(otherMetrics.Payload, &telemetry)
	if len(telemetry.Clients) != 1 {
		t.Fatal("metrics crossed room boundary")
	}

	stop()
	restarted, _ := persistedUniverseHost(t, dir)
	resumed := reconnectSocket(t, restarted, ownerHTTP)
	if e := universeExchange(t, resumed, "player.init", "resume", map[string]any{}); e.Type != "player.ready" {
		t.Fatal(e.Type, string(e.Payload))
	}
	restored := wireUniverse(t, universeExchange(t, resumed, "universe.get", "restored", map[string]any{}))
	if restored.Version != 6 || restored.Settings.NPCCount != 96 || len(restored.Objects) != initialObjectCount+4 || restored.Agents[0].Behavior != "guard" || restored.Objects[1].Z != 1.2 {
		t.Fatal("restart lost room environment", restored)
	}
	afterRestart := universeExchange(t, resumed, "telemetry.get", "fresh-session", map[string]any{})
	json.Unmarshal(afterRestart.Payload, &telemetry)
	for _, client := range telemetry.Clients {
		if client.FPS != nil || !client.Stale {
			t.Fatal("old process telemetry survived restart")
		}
	}
	assertScope := func(e protocol.Envelope, wantCode string, wantGeneration int) {
		t.Helper()
		var scope struct {
			RoomCode   string `json:"room_code"`
			Generation int    `json:"generation"`
		}
		if err := json.Unmarshal(e.Payload, &scope); err != nil || scope.RoomCode != wantCode || scope.Generation != wantGeneration {
			t.Fatalf("wrong event scope: want %s:%d, got %s", wantCode, wantGeneration, e.Payload)
		}
	}
	assertScope(afterRestart, code, 1)
	// A reset reuses version 1, so generation is essential for the client to
	// reject delayed events from the preceding mission with a higher version.
	universeExchange(t, resumed, "room.reset", "reset-scope", map[string]any{})
	resetEvent := universeExchange(t, resumed, "universe.get", "reset-snapshot", map[string]any{})
	assertScope(resetEvent, code, 2)
	if reset := wireUniverse(t, resetEvent); reset.Version != 1 || len(reset.Objects) != initialObjectCount {
		t.Fatal("reset state wrong", reset)
	}
	assertScope(universeExchange(t, resumed, "telemetry.get", "reset-metrics", map[string]any{}), code, 2)
	// Membership changes must derive scope from the destination Room, even
	// though both rooms now have universe version 1.
	universeExchange(t, resumed, "room.join", "switch-room", map[string]string{"code": otherCode})
	assertScope(universeExchange(t, resumed, "universe.get", "joined-snapshot", map[string]any{}), otherCode, 1)
	otherResumed := reconnectSocket(t, restarted, otherHTTP)
	universeExchange(t, otherResumed, "player.init", "other-resume", map[string]any{})
	assertScope(universeExchange(t, otherResumed, "telemetry.get", "joined-metrics", map[string]any{}), otherCode, 1)
}
