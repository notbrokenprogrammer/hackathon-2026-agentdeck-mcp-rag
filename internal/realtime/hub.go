// Package realtime owns socket lifetime and transport, not game logic.
package realtime

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"feeders/internal/model"
	"feeders/internal/server"
	"feeders/pkg/protocol"
	"github.com/gorilla/websocket"
)

type Authenticator interface {
	Resolve(*http.Request) (model.User, bool)
}

// Session is passed by the transport: handlers must never trust an ID in payload.
type Session interface {
	User() model.User
	Send(kind, requestID string, payload any) bool
}

type Handler func(context.Context, Session, protocol.Envelope)
type DisconnectHandler func(context.Context, model.User)

type Hub struct {
	auth         Authenticator
	log          *slog.Logger
	mu           sync.Mutex
	clients      map[string]*client
	handlers     map[string]Handler
	onDisconnect DisconnectHandler
	closing      bool
	wg           sync.WaitGroup
}

func New(auth Authenticator, log *slog.Logger) *Hub {
	h := &Hub{auth: auth, log: log, clients: make(map[string]*client), handlers: make(map[string]Handler)}
	h.Handle("ping", func(ctx context.Context, s Session, e protocol.Envelope) { s.Send("pong", e.RequestID, e.Payload) })
	return h
}

// Register handlers before serving. Duplicate message types fail at startup.
func (h *Hub) Handle(kind string, fn Handler) {
	if _, exists := h.handlers[kind]; exists {
		panic("duplicate realtime handler: " + kind)
	}
	h.handlers[kind] = fn
}
func (h *Hub) OnDisconnect(fn DisconnectHandler) { h.onDisconnect = fn }
func (h *Hub) RegisterRoutes(mux *http.ServeMux) { mux.HandleFunc("GET /ws", h.ServeWS) }

func (h *Hub) ServeWS(w http.ResponseWriter, r *http.Request) {
	u, ok := h.auth.Resolve(r)
	if !ok {
		server.JSON(w, 401, protocol.Error{Code: "unauthorized", Message: "Login required"})
		return
	}
	if !server.SameOrigin(r) {
		server.JSON(w, 403, protocol.Error{Code: "forbidden_origin", Message: "Same-origin requests required"})
		return
	}
	// Reserve a player before upgrading: concurrent tabs must not race Join/Leave.
	h.mu.Lock()
	if h.closing {
		h.mu.Unlock()
		server.JSON(w, 503, protocol.Error{Code: "shutting_down", Message: "Host is stopping"})
		return
	}
	if _, exists := h.clients[u.ID]; exists {
		h.mu.Unlock()
		server.JSON(w, 409, protocol.Error{Code: "already_connected", Message: "One socket per player"})
		return
	}
	ctx, cancel := context.WithCancel(r.Context())
	c := &client{hub: h, user: u, ctx: ctx, cancel: cancel, out: make(chan protocol.Envelope, 64), done: make(chan struct{})}
	h.clients[u.ID] = c
	h.wg.Add(1)
	h.mu.Unlock()
	defer func() {
		c.stop()
		if c.conn != nil && h.onDisconnect != nil {
			leaveCtx, leaveCancel := context.WithTimeout(context.Background(), 5*time.Second)
			h.onDisconnect(leaveCtx, u)
			leaveCancel()
		}
		h.mu.Lock()
		delete(h.clients, u.ID)
		h.mu.Unlock()
		h.wg.Done()
	}()
	upgrader := websocket.Upgrader{HandshakeTimeout: 5 * time.Second, CheckOrigin: server.SameOrigin}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	// Publish/Close can run during the handshake; only this goroutine sets conn.
	c.connMu.Lock()
	c.conn = conn
	c.connMu.Unlock()
	select {
	case <-c.done:
		_ = conn.Close()
		return
	default:
	}
	writerDone := make(chan struct{})
	go func() { defer close(writerDone); c.writeLoop(r) }()
	c.readLoop(r)
	c.stop()
	<-writerDone
}

// Publish targets one player. A full queue disconnects a slow client rather than
// blocking a simulation tick or allowing unbounded memory growth.
func (h *Hub) Publish(playerID, kind string, payload any) bool {
	h.mu.Lock()
	c := h.clients[playerID]
	h.mu.Unlock()
	return c != nil && c.Send(kind, "", payload)
}

// Broadcast sends a server event to all connected clients.
func (h *Hub) Broadcast(kind string, payload any) int {
	h.mu.Lock()
	clients := make([]*client, 0, len(h.clients))
	for _, c := range h.clients {
		clients = append(clients, c)
	}
	h.mu.Unlock()

	sent := 0
	for _, c := range clients {
		if c.Send(kind, "", payload) {
			sent++
		}
	}
	return sent
}

// ConnectedPlayerIDs returns a list of all currently connected player IDs.
func (h *Hub) ConnectedPlayerIDs() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	ids := make([]string, 0, len(h.clients))
	for id := range h.clients {
		ids = append(ids, id)
	}
	return ids
}

func (h *Hub) Close(ctx context.Context) error {
	h.mu.Lock()
	h.closing = true
	for _, c := range h.clients {
		c.stop()
	}
	h.mu.Unlock()
	done := make(chan struct{})
	go func() { h.wg.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type client struct {
	hub    *Hub
	user   model.User
	ctx    context.Context
	cancel context.CancelFunc
	connMu sync.Mutex
	conn   *websocket.Conn
	out    chan protocol.Envelope
	done   chan struct{}
	once   sync.Once
}

func (c *client) User() model.User { return c.user }
func (c *client) Send(kind, requestID string, payload any) bool {
	e, err := protocol.Message(kind, requestID, payload)
	if err != nil {
		c.hub.log.Error("encode message", "type", kind, "error", err)
		return false
	}
	select {
	case <-c.done:
		return false
	default:
	}
	select {
	case c.out <- e:
		return true
	default:
		c.stop()
		return false
	}
}
func (c *client) stop() {
	c.once.Do(func() {
		close(c.done)
		c.cancel()
		c.connMu.Lock()
		if c.conn != nil {
			_ = c.conn.Close()
		}
		c.connMu.Unlock()
	})
}

func (c *client) readLoop(r *http.Request) {
	c.conn.SetReadLimit(protocol.MaxMessageBytes)
	_ = c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.conn.SetPongHandler(func(string) error { return c.conn.SetReadDeadline(time.Now().Add(60 * time.Second)) })
	// Per-connection token bucket: enough for the existing 18 Hz client.
	tokens := 120.0
	last := time.Now()
	for {
		kind, data, err := c.conn.ReadMessage()
		if err != nil {
			return
		}
		if _, ok := c.hub.auth.Resolve(r); !ok {
			return
		}
		now := time.Now()
		tokens += now.Sub(last).Seconds() * 60
		last = now
		if tokens > 120 {
			tokens = 120
		}
		tokens--
		if tokens < 0 {
			return
		}
		if kind != websocket.TextMessage {
			c.Send("error", "", protocol.Error{Code: "invalid_message", Message: "Text JSON required"})
			continue
		}
		e, err := protocol.Decode(data)
		if err != nil {
			c.Send("error", "", protocol.Error{Code: "invalid_message", Message: err.Error()})
			continue
		}
		fn, exists := c.hub.handlers[e.Type]
		if !exists {
			c.Send("error", e.RequestID, protocol.Error{Code: "unknown_type", Message: "Unknown message type"})
			continue
		}
		fn(c.ctx, c, e)
	}
}

func (c *client) writeLoop(r *http.Request) {
	defer c.stop()
	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()
	sessionCheck := time.NewTicker(time.Second)
	defer sessionCheck.Stop()
	for {
		select {
		case <-c.done:
			return
		case <-sessionCheck.C:
			if _, ok := c.hub.auth.Resolve(r); !ok {
				return
			}
		case e := <-c.out:
			_ = c.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if err := c.conn.WriteJSON(e); err != nil {
				return
			}
		case <-ping.C:
			if err := c.conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second)); err != nil && !errors.Is(err, websocket.ErrCloseSent) {
				return
			}
		}
	}
}
