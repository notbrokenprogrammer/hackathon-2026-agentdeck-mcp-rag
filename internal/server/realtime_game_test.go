package server_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"feeders/internal/agents"
	"feeders/internal/auth"
	"feeders/internal/game"
	"feeders/internal/realtime"
	"feeders/internal/server"
	"feeders/internal/storage"
	"feeders/pkg/protocol"
	"feeders/web"
	"github.com/gorilla/websocket"
)

type blockedMentor struct {
	started chan struct{}
	release chan struct{}
}

func (b *blockedMentor) Talk(ctx context.Context, r agents.Request) (agents.Reply, error) {
	close(b.started)
	select {
	case <-b.release:
		return agents.Reply{AgentID: "savinykh", Text: "done"}, nil
	case <-ctx.Done():
		return agents.Reply{}, ctx.Err()
	}
}

func fullHost(t *testing.T, agent agents.Service) (*httptest.Server, *realtime.Hub) {
	t.Helper()
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
	loop := game.NewServerLoop(repo, hub, log)
	ctx, cancel := context.WithCancel(context.Background())
	loop.Start(ctx)
	game.New(repo, loop, agent, log).Register(hub)
	s := httptest.NewServer(server.New(web.Files, a, hub))
	t.Cleanup(func() {
		cancel()
		loop.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = hub.Close(ctx)
		s.Close()
	})
	return s, hub
}
func registerSocket(t *testing.T, s *httptest.Server, name string) (*websocket.Conn, *http.Client) {
	t.Helper()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, Timeout: 3 * time.Second}
	res, err := client.Post(s.URL+"/api/auth/register", "application/json", strings.NewReader(`{"username":"`+name+`","password":"test-password"}`))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 201 {
		t.Fatal(res.Status)
	}
	return reconnectSocket(t, s, client), client
}
func reconnectSocket(t *testing.T, s *httptest.Server, client *http.Client) *websocket.Conn {
	t.Helper()
	req, _ := http.NewRequest("GET", s.URL, nil)
	headers := http.Header{"Origin": []string{s.URL}}
	for _, c := range client.Jar.Cookies(req.URL) {
		headers.Add("Cookie", c.String())
	}
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(s.URL, "http")+"/ws", headers)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}
func sendPacket(t *testing.T, c *websocket.Conn, kind, id string, payload any) {
	t.Helper()
	e, _ := protocol.Message(kind, id, payload)
	if err := c.WriteJSON(e); err != nil {
		t.Fatal(err)
	}
}
func readPacket(t *testing.T, c *websocket.Conn, match func(protocol.Envelope) bool) protocol.Envelope {
	t.Helper()
	c.SetReadDeadline(time.Now().Add(3 * time.Second))
	for {
		var e protocol.Envelope
		if err := c.ReadJSON(&e); err != nil {
			t.Fatal(err)
		}
		if match(e) {
			return e
		}
	}
}

func TestTwoPlayersRealWebSocketAndReconnect(t *testing.T) {
	m, err := agents.NewMentor(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s, h := fullHost(t, m)
	a, client := registerSocket(t, s, "pilot-one")
	b, _ := registerSocket(t, s, "pilot-two")
	sendPacket(t, a, "player.init", "init-a", map[string]any{})
	ready := readPacket(t, a, func(e protocol.Envelope) bool { return e.Type == "player.ready" })
	var p protocol.PlayerReady
	json.Unmarshal(ready.Payload, &p)
	sendPacket(t, b, "player.init", "init-b", map[string]any{})
	readPacket(t, b, func(e protocol.Envelope) bool { return e.Type == "player.ready" })
	sendPacket(t, a, "player.input", "move", protocol.PlayerInput{Seq: 1, X: -14.5, Y: 1.7, Z: 0, Yaw: .4})
	readPacket(t, b, func(e protocol.Envelope) bool {
		if e.Type != "world.snapshot" {
			return false
		}
		var snap protocol.WorldSnapshot
		json.Unmarshal(e.Payload, &snap)
		if len(snap.Players) != 2 {
			return false
		}
		for _, other := range snap.Players {
			if other.ID == p.User.ID && other.X == -14.5 {
				return true
			}
		}
		return false
	})
	sendPacket(t, a, "game.action", "action", map[string]string{"action": "repair_cooling"})
	readPacket(t, b, func(e protocol.Envelope) bool {
		if e.Type != "game.state" {
			return false
		}
		var state game.GameState
		json.Unmarshal(e.Payload, &state)
		return state.CoolingRepaired && state.Cycle == 2
	})
	sendPacket(t, a, "agent.talk", "mentor", map[string]string{"agent_id": "savinykh", "text": "Как восстанавливали питание Салюта-7?"})
	e := readPacket(t, a, func(e protocol.Envelope) bool { return e.RequestID == "mentor" })
	if e.Type != "agent.reply" {
		t.Fatalf("mentor: %+v", e)
	}
	var reply agents.Reply
	json.Unmarshal(e.Payload, &reply)
	if reply.Citation == nil || reply.Citation.SourceURL == "" {
		t.Fatal("mentor did not cite a source")
	}
	a.Close()
	deadline := time.Now().Add(2 * time.Second)
	for len(h.ConnectedPlayerIDs()) > 1 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	a = reconnectSocket(t, s, client)
	sendPacket(t, a, "player.init", "rejoin", map[string]any{})
	e = readPacket(t, a, func(e protocol.Envelope) bool { return e.Type == "player.ready" })
	json.Unmarshal(e.Payload, &p)
	if p.State.Position.X != -14.5 {
		t.Fatalf("reconnect lost position: %+v", p.State)
	}
	sendPacket(t, a, "player.input", "new-seq", protocol.PlayerInput{Seq: 1, X: -14, Y: 1.7, Z: 0, Yaw: 0})
	readPacket(t, b, func(e protocol.Envelope) bool {
		if e.Type != "world.snapshot" {
			return false
		}
		var snap protocol.WorldSnapshot
		json.Unmarshal(e.Payload, &snap)
		for _, other := range snap.Players {
			if other.ID == p.User.ID && other.X == -14 {
				return true
			}
		}
		return false
	})
}

func TestAgentDoesNotBlockSocketReader(t *testing.T) {
	m := &blockedMentor{started: make(chan struct{}), release: make(chan struct{})}
	s, _ := fullHost(t, m)
	c, _ := registerSocket(t, s, "async-pilot")
	sendPacket(t, c, "player.init", "init", map[string]any{})
	readPacket(t, c, func(e protocol.Envelope) bool { return e.Type == "player.ready" })
	sendPacket(t, c, "agent.talk", "talk", map[string]string{"agent_id": "savinykh", "text": "питание"})
	select {
	case <-m.started:
	case <-time.After(time.Second):
		t.Fatal("agent did not start")
	}
	sendPacket(t, c, "ping", "while-thinking", map[string]int{"timestamp": 1})
	readPacket(t, c, func(e protocol.Envelope) bool { return e.RequestID == "while-thinking" && e.Type == "pong" })
	close(m.release)
	readPacket(t, c, func(e protocol.Envelope) bool { return e.RequestID == "talk" && e.Type == "agent.reply" })
}
