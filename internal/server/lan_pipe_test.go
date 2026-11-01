package server_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
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

// Only an in-memory connection is used: no Listen, TCP, DNS or HTTP client.
// The real gorilla client and hub still negotiate HTTP Upgrade and exchange
// masked/unmasked WebSocket frames through their normal reader/writer loops.
type pipeResponseWriter struct {
	conn   net.Conn
	reader *bufio.Reader
	header http.Header
	wrote  bool
}

func (w *pipeResponseWriter) Header() http.Header { return w.header }
func (w *pipeResponseWriter) WriteHeader(status int) {
	if w.wrote {
		return
	}
	w.wrote = true
	fmt.Fprintf(w.conn, "HTTP/1.1 %d %s\r\n", status, http.StatusText(status))
	w.header.Write(w.conn)
	fmt.Fprint(w.conn, "\r\n")
}
func (w *pipeResponseWriter) Write(body []byte) (int, error) {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	return w.conn.Write(body)
}
func (w *pipeResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return w.conn, bufio.NewReadWriter(w.reader, bufio.NewWriter(w.conn)), nil
}

type pipePlayer struct {
	conn   *websocket.Conn
	user   model.User
	events chan protocol.Envelope
	errors chan error
	nextID int
}

func connectPipePlayer(t *testing.T, handler http.Handler, user model.User, cookie *http.Cookie) *pipePlayer {
	t.Helper()
	client, host := net.Pipe()
	finished := make(chan struct{})
	t.Cleanup(func() {
		client.Close()
		host.Close()
		select {
		case <-finished:
		case <-time.After(3 * time.Second):
			t.Error("in-memory WebSocket handler did not stop")
		}
	})
	go func() {
		defer close(finished)
		defer host.Close()
		reader := bufio.NewReader(host)
		req, err := http.ReadRequest(reader)
		if err != nil {
			return
		}
		defer req.Body.Close()
		req.RemoteAddr = "192.168.1.26:55000"
		handler.ServeHTTP(&pipeResponseWriter{conn: host, reader: reader, header: make(http.Header)}, req)
	}()
	client.SetDeadline(time.Now().Add(3 * time.Second))
	endpoint, _ := url.Parse("ws://192.168.1.25:8080/ws")
	headers := http.Header{"Origin": {"http://192.168.1.25:8080"}, "Cookie": {cookie.String()}}
	conn, response, err := websocket.NewClient(client, endpoint, headers, 4096, 4096)
	if err != nil {
		t.Fatalf("in-memory WebSocket Upgrade: %v (response %+v)", err, response)
	}
	if response.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("handshake status: %d", response.StatusCode)
	}
	client.SetDeadline(time.Time{})
	p := &pipePlayer{conn: conn, user: user, events: make(chan protocol.Envelope, 256), errors: make(chan error, 1)}
	readerDone := make(chan struct{})
	t.Cleanup(func() {
		conn.Close()
		select {
		case <-readerDone:
		case <-time.After(time.Second):
			t.Error("in-memory WebSocket client reader did not stop")
		}
	})
	go func() {
		defer close(readerDone)
		for {
			var packet protocol.Envelope
			if err := conn.ReadJSON(&packet); err != nil {
				p.errors <- err
				return
			}
			select {
			case p.events <- packet:
			case <-time.After(time.Second):
				p.errors <- fmt.Errorf("in-memory test client event queue overflow")
				return
			}
		}
	}()
	return p
}

func (p *pipePlayer) send(t *testing.T, kind string, payload any) string {
	t.Helper()
	p.nextID++
	id := fmt.Sprintf("%s-%d", p.user.ID, p.nextID)
	packet, err := protocol.Message(kind, id, payload)
	if err != nil {
		t.Fatal(err)
	}
	p.conn.SetWriteDeadline(time.Now().Add(3 * time.Second))
	if err := p.conn.WriteJSON(packet); err != nil {
		t.Fatal(err)
	}
	return id
}

func (p *pipePlayer) receive(t *testing.T, match func(protocol.Envelope) bool) protocol.Envelope {
	t.Helper()
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	for {
		select {
		case packet := <-p.events:
			if packet.ProtocolVersion != 1 {
				t.Fatal("unexpected protocol version", packet.ProtocolVersion)
			}
			if match(packet) {
				return packet
			}
		case err := <-p.errors:
			t.Fatalf("WebSocket reader for %s: %v", p.user.Username, err)
		case <-timer.C:
			t.Fatalf("timed out waiting for WebSocket packet for %s", p.user.Username)
		}
	}
}

func (p *pipePlayer) request(t *testing.T, kind string, payload any) protocol.Envelope {
	t.Helper()
	id := p.send(t, kind, payload)
	return p.receive(t, func(e protocol.Envelope) bool { return e.RequestID == id })
}

func (p *pipePlayer) command(t *testing.T, kind string, payload any) protocol.Envelope {
	t.Helper()
	reply := p.request(t, kind, payload)
	if reply.Type == "error" {
		t.Fatalf("%s: %s", kind, reply.Payload)
	}
	return reply
}

func pipeDecode[T any](t *testing.T, packet protocol.Envelope) T {
	t.Helper()
	var result T
	if err := json.Unmarshal(packet.Payload, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestLANInMemoryWebSocketCrewChatMovementAndDoors(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	repo, err := storage.NewSQLiteStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { repo.Close() })
	authHandler, err := auth.New(repo, time.Hour, false, log)
	if err != nil {
		t.Fatal(err)
	}
	hub := realtime.New(authHandler, log)
	loop := game.NewServerLoop(repo, hub, log)
	module := game.New(repo, loop, nil, log)
	if err := module.WithPersistence(repo); err != nil {
		t.Fatal(err)
	}
	loop.ConfigureWorld(module.RoomOf, module.Walkable)
	module.Register(hub)
	ctx, cancel := context.WithCancel(context.Background())
	loop.Start(ctx)
	t.Cleanup(func() {
		cancel()
		loop.Stop()
		ctx, stop := context.WithTimeout(context.Background(), 3*time.Second)
		defer stop()
		if err := hub.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	handler := server.New(web.Files, authHandler, hub)
	register := func(name string, x, z float64) *pipePlayer {
		t.Helper()
		body, _ := json.Marshal(map[string]string{"username": name, "password": "test-only-password"})
		req := httptest.NewRequest("POST", "http://192.168.1.25:8080/api/auth/register", strings.NewReader(string(body)))
		req.Header.Set("Origin", "http://192.168.1.25:8080")
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("register %s: %d %s", name, rec.Code, rec.Body.String())
		}
		var registered struct {
			User model.User `json:"user"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &registered); err != nil {
			t.Fatal(err)
		}
		// Seed a normal saved position beside the cryo door. After initialization
		// every movement and interaction below uses only client WebSocket frames.
		if err := repo.SavePlayer(context.Background(), model.PlayerState{PlayerID: registered.User.ID,
			Position: model.Position{X: x, Y: 1.7, Z: z}, UpdatedAt: time.Now().UTC()}); err != nil {
			t.Fatal(err)
		}
		cookies := rec.Result().Cookies()
		if len(cookies) != 1 {
			t.Fatal("registration did not issue a session")
		}
		p := connectPipePlayer(t, handler, registered.User, cookies[0])
		ready := pipeDecode[protocol.PlayerReady](t, p.command(t, "player.init", map[string]any{}))
		if ready.User.ID != registered.User.ID || ready.State.Position.X != x {
			t.Fatal("authenticated identity or saved position changed", ready)
		}
		return p
	}
	a := register("pipe-captain", -11.4, 0)
	b := register("pipe-engineer", -12, .6)
	type roomView struct {
		Code    string             `json:"code"`
		Started bool               `json:"started"`
		Members []game.RoomMember  `json:"members"`
		Chat    []game.ChatMessage `json:"chat"`
	}
	created := pipeDecode[roomView](t, a.command(t, "room.create", map[string]string{"name": "Pipe crew"}))
	if created.Code == "" {
		t.Fatal("room code missing")
	}
	b.command(t, "room.join", map[string]string{"code": created.Code})
	a.command(t, "room.role", map[string]string{"role": "captain"})
	b.command(t, "room.role", map[string]string{"role": "engineer"})
	a.command(t, "room.ready", map[string]bool{"ready": true})
	b.command(t, "room.ready", map[string]bool{"ready": true})
	a.command(t, "room.start", map[string]any{})
	for _, p := range []*pipePlayer{a, b} {
		room := pipeDecode[roomView](t, p.command(t, "room.get", map[string]any{}))
		if room.Code != created.Code || !room.Started || len(room.Members) != 2 {
			t.Fatalf("crew did not synchronize for %s: %+v", p.user.Username, room)
		}
		roles := map[string]string{}
		for _, member := range room.Members {
			if !member.Ready {
				t.Fatal("started with unready member")
			}
			roles[member.ID] = member.Role
		}
		if roles[a.user.ID] != "captain" || roles[b.user.ID] != "engineer" {
			t.Fatal("authenticated roles differ", roles)
		}
	}
	a.command(t, "chat.send", map[string]string{"text": "Проверка связи, инженер."})
	b.receive(t, func(e protocol.Envelope) bool {
		if e.Type != "room.state" {
			return false
		}
		r := pipeDecode[roomView](t, e)
		return r.Code == created.Code && len(r.Chat) == 1 && r.Chat[0].Name == a.user.Username && r.Chat[0].Text == "Проверка связи, инженер."
	})
	b.command(t, "chat.send", map[string]string{"text": "Принимаю, капитан."})
	a.receive(t, func(e protocol.Envelope) bool {
		if e.Type != "room.state" {
			return false
		}
		r := pipeDecode[roomView](t, e)
		return r.Code == created.Code && len(r.Chat) == 2 && r.Chat[1].Name == b.user.Username && r.Chat[1].Text == "Принимаю, капитан."
	})
	errorCode := func(packet protocol.Envelope, want string) {
		t.Helper()
		if packet.Type != "error" || pipeDecode[protocol.Error](t, packet).Code != want {
			t.Fatalf("expected %s error, got %s %s", want, packet.Type, packet.Payload)
		}
	}
	errorCode(a.request(t, "player.input", protocol.PlayerInput{Seq: 1, X: -10, Y: 1.7, Z: 0}), "door_closed")
	state := pipeDecode[game.UniverseSnapshot](t, a.command(t, "universe.get", map[string]any{}))
	door := func(snapshot game.UniverseSnapshot) game.UniverseObject {
		t.Helper()
		for _, object := range snapshot.Objects {
			if object.ID == "door-cryo" {
				return object
			}
		}
		t.Fatal("cryo door missing")
		return game.UniverseObject{}
	}
	if door(state).Open {
		t.Fatal("transition door was not initially closed")
	}
	state = pipeDecode[game.UniverseSnapshot](t, a.command(t, "universe.interact", map[string]any{
		"object_id": "door-cryo", "action": "toggle", "expected_version": state.Version}))
	opened := door(state)
	if !opened.Open || opened.MotionAt == 0 {
		t.Fatal("door did not begin opening")
	}
	b.receive(t, func(e protocol.Envelope) bool {
		if e.Type != "universe.state" {
			return false
		}
		other := pipeDecode[game.UniverseSnapshot](t, e)
		return other.RoomCode == created.Code && other.Version == state.Version && door(other) == opened
	})
	errorCode(b.request(t, "universe.interact", map[string]any{
		"object_id": "door-cryo", "action": "toggle", "expected_version": state.Version}), "door_moving")
	// Wait for the real 900ms door animation; no clock or collision bypass.
	time.Sleep(time.Until(time.UnixMilli(opened.MotionAt).Add(time.Second)))
	snapshotAt := func(p *pipePlayer, x float64) {
		t.Helper()
		p.receive(t, func(e protocol.Envelope) bool {
			if e.Type != "world.snapshot" {
				return false
			}
			snapshot := pipeDecode[protocol.WorldSnapshot](t, e)
			if len(snapshot.Players) != 2 {
				return false
			}
			for _, player := range snapshot.Players {
				if player.ID == a.user.ID && player.X == x && player.Y == 1.7 && player.Z == 0 {
					return true
				}
			}
			return false
		})
	}
	a.send(t, "player.input", protocol.PlayerInput{Seq: 2, X: -10, Y: 1.7, Z: 0})
	snapshotAt(a, -10)
	snapshotAt(b, -10)
	errorCode(b.request(t, "universe.interact", map[string]any{
		"object_id": "door-cryo", "action": "toggle", "expected_version": state.Version}), "door_occupied")
	a.send(t, "player.input", protocol.PlayerInput{Seq: 3, X: -9, Y: 1.7, Z: 0})
	snapshotAt(b, -9)
	state = pipeDecode[game.UniverseSnapshot](t, b.command(t, "universe.interact", map[string]any{
		"object_id": "door-cryo", "action": "toggle", "expected_version": state.Version}))
	closed := door(state)
	if closed.Open {
		t.Fatal("door did not close after the other player left the opening")
	}
	a.receive(t, func(e protocol.Envelope) bool {
		if e.Type != "universe.state" {
			return false
		}
		other := pipeDecode[game.UniverseSnapshot](t, e)
		return other.RoomCode == created.Code && other.Version == state.Version && door(other) == closed
	})
	t.Log("In-memory real WebSocket frames: two sessions, common crew/roles/readiness, bidirectional chat, authoritative movement, shared door animation and occupancy safety passed; no physical LAN assertion")
}
