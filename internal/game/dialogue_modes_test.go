package game

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"feeders/internal/agents"
	"feeders/pkg/protocol"
)

type modeCapturingAgents struct{ requests chan agents.Request }

func (a *modeCapturingAgents) Talk(_ context.Context, r agents.Request) (agents.Reply, error) {
	a.requests <- r
	return agents.Reply{AgentID: r.AgentID, Text: "Ответ в режиме " + r.DialogueMode, Mode: "model", DialogueMode: r.DialogueMode}, nil
}

func TestDialogueModeHandlerForwardingAndWorldCommandsRemainLegacyOnly(t *testing.T) {
	for _, mode := range []string{"articles", "casual"} {
		t.Run(mode, func(t *testing.T) {
			m, store := roomFixture(t, t.TempDir())
			defer store.Close()
			player := attach(t, m, "alice")
			startUniverseRoom(t, m, player)
			player.delivered = make(chan struct{}, 1)
			service := &modeCapturingAgents{requests: make(chan agents.Request, 1)}
			m.agents = service
			before, _ := json.Marshal(m.rooms[m.RoomOf("alice")].Universe)
			message, _ := protocol.Message("agent.talk", "request-"+mode, map[string]string{"agent_id": "savinykh", "text": "Открой двери", "dialogue_mode": mode})
			m.talk(context.Background(), player, message)
			select {
			case got := <-service.requests:
				if got.DialogueMode != mode || got.PlayerID != "alice" || got.AgentID != "savinykh" || got.Text != "Открой двери" || got.Scope != m.memoryScope("alice") {
					t.Fatal("mode or trusted identity lost", got)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("explicit dialogue was intercepted as a world command")
			}
			select {
			case <-player.delivered:
			case <-time.After(2 * time.Second):
				t.Fatal("mode reply missing")
			}
			after, _ := json.Marshal(m.rooms[m.RoomOf("alice")].Universe)
			if string(before) != string(after) {
				t.Fatal("explicit conversation changed the game world")
			}
			last := player.messages[len(player.messages)-1]
			var reply agents.Reply
			if err := json.Unmarshal(last.Payload, &reply); err != nil || last.Type != "agent.reply" || reply.DialogueMode != mode {
				t.Fatal(last, err)
			}
			rows := m.rooms[m.RoomOf("alice")].Dialogues[dialogueKey("alice", "savinykh", mode)]
			if len(rows) != 2 || rows[0].DialogueMode != mode || rows[1].DialogueMode != mode || rows[1].Reply.DialogueMode != mode {
				t.Fatal("stored mode missing", rows)
			}
		})
	}
}

func TestInvalidDialogueModesRejectedBeforeCommandsAndInference(t *testing.T) {
	m, store := roomFixture(t, t.TempDir())
	defer store.Close()
	player := attach(t, m, "alice")
	startUniverseRoom(t, m, player)
	service := &modeCapturingAgents{requests: make(chan agents.Request, 1)}
	m.agents = service
	before, _ := json.Marshal(m.rooms[m.RoomOf("alice")].Universe)
	for _, command := range []string{"agent.talk", "dialogue.history"} {
		payload := map[string]string{"agent_id": "savinykh", "dialogue_mode": "invented"}
		if command == "agent.talk" {
			payload["text"] = "Открой двери"
		}
		e, _ := protocol.Message(command, command, payload)
		if command == "agent.talk" {
			m.talk(context.Background(), player, e)
		} else {
			m.dialogueHistory(context.Background(), player, e)
		}
		last := player.messages[len(player.messages)-1]
		var err protocol.Error
		_ = json.Unmarshal(last.Payload, &err)
		if last.Type != "error" || err.Code != "invalid_dialogue_mode" {
			t.Fatal("invalid mode accepted", last)
		}
	}
	if len(service.requests) != 0 {
		t.Fatal("invalid mode reached inference")
	}
	after, _ := json.Marshal(m.rooms[m.RoomOf("alice")].Universe)
	if string(before) != string(after) {
		t.Fatal("invalid mode executed world command")
	}
}

func TestDialogueModeHistoryPersistsAndIsolatesPlayerAndLegacy(t *testing.T) {
	dir := t.TempDir()
	m, store := roomFixture(t, dir)
	player := attach(t, m, "alice")
	roomCmd(t, m, player, "room.create", map[string]string{"name": "Mode test"})
	code := m.RoomOf("alice")
	for _, mode := range []string{"", "articles", "casual", "mission"} {
		reply := agents.Reply{AgentID: "savinykh", Text: "Ответ " + mode, DialogueMode: mode}
		if err := m.recordDialogue("alice", m.memoryScope("alice"), "savinykh", "Вопрос "+mode, reply); err != nil {
			t.Fatal(err)
		}
	}
	store.Close()
	m, store = roomFixture(t, dir)
	defer store.Close()
	player = attach(t, m, "alice")
	other := attach(t, m, "boris")
	roomCmd(t, m, other, "room.join", map[string]string{"code": code})
	for _, mode := range []string{"", "articles", "casual", "mission"} {
		for _, session := range []*fakeSession{player, other} {
			e, _ := protocol.Message("dialogue.history", "history", map[string]string{"agent_id": "savinykh", "dialogue_mode": mode})
			m.dialogueHistory(context.Background(), session, e)
			last := session.messages[len(session.messages)-1]
			var body struct {
				Messages     []DialogueMessage `json:"messages"`
				DialogueMode string            `json:"dialogue_mode"`
			}
			if err := json.Unmarshal(last.Payload, &body); err != nil || last.Type != "dialogue.history" {
				t.Fatal(last, err)
			}
			if body.DialogueMode != mode {
				t.Fatal("history response mode missing")
			}
			if session == other {
				if len(body.Messages) != 0 {
					t.Fatal("another player's history exposed")
				}
				continue
			}
			if len(body.Messages) != 2 || body.Messages[0].Text != "Вопрос "+mode || body.Messages[1].Text != "Ответ "+mode || body.Messages[1].DialogueMode != mode {
				t.Fatal("history mixed across modes or restart", body)
			}
		}
	}
}
