// Package agents provides local, grounded NPC conversations through Ollama.
package npc

import (
	"context"
	"feeders/internal/mission"
	"feeders/internal/rag"
)

type Request struct {
	Scope        string
	PlayerID     string
	AgentID      string
	Text         string
	DialogueMode string `json:"dialogue_mode,omitempty"`
}

const (
	DialogueArticles = "articles"
	DialogueCasual   = "casual"
	DialogueMission  = "mission"
)

func ValidDialogueMode(mode string) bool {
	return mode == "" || mode == DialogueArticles || mode == DialogueCasual || mode == DialogueMission
}

type Reply struct {
	DialogueMode   string                 `json:"dialogue_mode,omitempty"`
	Retrieval      *rag.Retrieval         `json:"retrieval,omitempty"`
	Grounding      string                 `json:"grounding,omitempty"`
	Model          string                 `json:"model,omitempty"`
	FallbackReason string                 `json:"fallback_reason,omitempty"`
	ToolTransport  string                 `json:"tool_transport,omitempty"`
	AgentID        string                 `json:"agent_id"`
	Text           string                 `json:"text"`
	Status         string                 `json:"status"`
	Evidence       []rag.Chunk            `json:"evidence"`
	Mode           string                 `json:"mode"`
	MissionVersion uint64                 `json:"mission_version,omitempty"`
	Analysis       any                    `json:"analysis,omitempty"`
	Command        *mission.CommandResult `json:"command,omitempty"`
}

func (r Request) memoryKey() key {
	agent := r.AgentID
	if r.DialogueMode != "" {
		agent += ":dialogue:" + r.DialogueMode
	}
	return key{r.memoryPlayer(), agent}
}

// Service implementations use local resources only; remember prior interactions
// per (player, agent). Respect context cancellation, timeouts and bounded concurrency.
type Service interface {
	Talk(context.Context, Request) (Reply, error)
}

func (r Request) memoryPlayer() string {
	if r.Scope != "" {
		return r.Scope + "/" + r.PlayerID
	}
	return r.PlayerID
}
