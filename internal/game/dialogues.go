package game

import (
	"context"
	"feeders/internal/agents"
	"feeders/internal/realtime"
	"feeders/pkg/protocol"
	"fmt"
)

type DialogueMessage struct {
	DialogueMode string        `json:"dialogue_mode,omitempty"`
	Role         string        `json:"role"`
	Text         string        `json:"text"`
	Reply        *agents.Reply `json:"reply,omitempty"`
}

func (m *Module) dialogueHistory(ctx context.Context, s realtime.Session, e protocol.Envelope) {
	var p struct {
		AgentID      string `json:"agent_id"`
		DialogueMode string `json:"dialogue_mode,omitempty"`
	}
	if protocol.DecodePayload(e.Payload, &p) != nil {
		sendError(s, e, "invalid_payload", "Выберите наставника")
		return
	}
	if !agents.ValidDialogueMode(p.DialogueMode) {
		sendError(s, e, "invalid_dialogue_mode", "Выберите режим articles, casual или mission")
		return
	}
	m.roomMu.Lock()
	defer m.roomMu.Unlock()
	r := m.rooms[m.membership[s.User().ID]]
	rows := []DialogueMessage{}
	if r != nil {
		if v := r.Dialogues[dialogueKey(s.User().ID, p.AgentID, p.DialogueMode)]; v != nil {
			rows = v
		}
	}
	s.Send("dialogue.history", e.RequestID, map[string]any{"messages": rows, "dialogue_mode": p.DialogueMode})
}

func dialogueKey(player, agent, mode string) string {
	key := player + "/" + agent
	if mode != "" {
		key += "/dialogue/" + mode
	}
	return key
}
func (m *Module) recordDialogue(id, scope, agent, text string, reply agents.Reply) error {
	m.roomMu.Lock()
	defer m.roomMu.Unlock()
	r := m.rooms[m.membership[id]]
	if r == nil {
		return nil
	}
	if scope != fmt.Sprintf("%s:%d", r.Code, r.Generation) {
		return context.Canceled
	}
	if r.Dialogues == nil {
		r.Dialogues = map[string][]DialogueMessage{}
	}
	key := dialogueKey(id, agent, reply.DialogueMode)
	rows := append(r.Dialogues[key], DialogueMessage{DialogueMode: reply.DialogueMode, Role: "user", Text: text}, DialogueMessage{DialogueMode: reply.DialogueMode, Role: "assistant", Text: reply.Text, Reply: &reply})
	if len(rows) > 40 {
		rows = rows[len(rows)-40:]
	}
	r.Dialogues[key] = rows
	return m.persistRoomsLocked()
}
