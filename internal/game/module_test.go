package game

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"feeders/internal/agents"
	"feeders/internal/model"
	"feeders/internal/storage"
	"feeders/pkg/protocol"
)

type fakeSession struct {
	user      model.User
	messages  []protocol.Envelope
	delivered chan struct{}
}

func (s *fakeSession) User() model.User { return s.user }
func (s *fakeSession) Send(kind, id string, payload any) bool {
	e, err := protocol.Message(kind, id, payload)
	if err != nil {
		panic(err)
	}
	s.messages = append(s.messages, e)
	if kind == "agent.reply" && s.delivered != nil {
		s.delivered <- struct{}{}
	}
	return true
}

// This fixture only verifies the seam; it is not an example of movement validation.
type fakeWorld struct {
	repo                  storage.Players
	state                 model.PlayerState
	joins, inputs, leaves int
	reject                bool
	seenPlayer            string
}

func (w *fakeWorld) Join(ctx context.Context, user model.User, state model.PlayerState) (model.PlayerState, error) {
	w.joins++
	w.state = state
	w.state.Position.X = 10
	return w.state, nil
}
func (w *fakeWorld) Input(ctx context.Context, id string, input protocol.PlayerInput) error {
	w.inputs++
	w.seenPlayer = id
	if w.reject {
		return errors.New("invalid movement")
	}
	w.state.Position.X = input.X
	w.state.LastSeq = input.Seq
	return nil
}
func (w *fakeWorld) Leave(ctx context.Context, id string) error {
	w.leaves++
	return w.repo.SavePlayer(ctx, w.state)
}

type fakeAgents struct {
	called bool
	reply  agents.Reply
}

func (fa *fakeAgents) Talk(ctx context.Context, req agents.Request) (agents.Reply, error) {
	fa.called = true
	return fa.reply, nil
}

func TestWorldExtensionAndIdentity(t *testing.T) {
	repo, err := storage.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	w := &fakeWorld{repo: repo}
	m := New(repo, w, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	s := &fakeSession{user: model.User{ID: "trusted-id", Username: "explorer"}}
	ctx := context.Background()
	init, _ := protocol.Message("player.init", "init", map[string]string{"username": "spoofed"})
	input, _ := protocol.Message("player.input", "input", protocol.PlayerInput{Seq: 1, X: 20, Y: 1.7})
	m.input(ctx, s, input)
	if string(s.messages[0].Payload) != `{"code":"not_initialized","message":"Send player.init first"}` {
		t.Fatal(s.messages[0])
	}
	m.init(ctx, s, init)
	m.init(ctx, s, init)
	if w.joins != 1 {
		t.Fatalf("Join called %d times", w.joins)
	}
	var ready protocol.PlayerReady
	if err := json.Unmarshal(s.messages[1].Payload, &ready); err != nil {
		t.Fatal(err)
	}
	if ready.State.Position.X != 10 || !ready.Capabilities.Movement || ready.User.ID != "trusted-id" {
		t.Fatal(ready)
	}
	m.input(ctx, s, input)
	if w.inputs != 1 || w.seenPlayer != "trusted-id" {
		t.Fatalf("input did not use trusted identity: %+v", w)
	}
	w.reject = true
	m.input(ctx, s, input)
	var failure protocol.Error
	_ = json.Unmarshal(s.messages[len(s.messages)-1].Payload, &failure)
	if failure.Code != "input_rejected" {
		t.Fatal(failure)
	}
	m.leave(ctx, s.user)
	m.leave(ctx, s.user)
	if w.leaves != 1 {
		t.Fatalf("Leave called %d times", w.leaves)
	}
	state, err := repo.LoadPlayer(ctx, s.user.ID)
	if err != nil || state.Position.X != 20 || state.LastSeq != 1 {
		t.Fatalf("saved state: %+v %v", state, err)
	}
}

func TestAgentTalkIntegration(t *testing.T) {
	repo, err := storage.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	fa := &fakeAgents{
		reply: agents.Reply{
			AgentID:   "engineer",
			AgentName: "Марк Вэнс",
			Text:      "Импульсный режим 2 Гц снизит нагрузку до 9 МВт.",
		},
	}

	m := New(repo, nil, fa, slog.New(slog.NewTextHandler(io.Discard, nil)))
	s := &fakeSession{user: model.User{ID: "astro-2", Username: "cadet"}, delivered: make(chan struct{}, 1)}
	ctx := context.Background()
	init, _ := protocol.Message("player.init", "init", map[string]any{})
	m.init(ctx, s, init)

	msg, _ := protocol.Message("agent.talk", "req-talk-1", map[string]string{
		"agent_id": "engineer",
		"text":     "Что делать с реактором?",
	})

	m.talk(ctx, s, msg)
	select {
	case <-s.delivered:
	case <-time.After(time.Second):
		t.Fatal("agent response timed out")
	}

	if !fa.called {
		t.Fatal("expected fakeAgents.Talk to be invoked")
	}

	last := s.messages[len(s.messages)-1]
	if last.Type != "agent.reply" {
		t.Fatalf("expected agent.reply, got %s", last.Type)
	}
}
