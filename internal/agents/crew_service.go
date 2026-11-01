package agents

import (
	"context"
	"feeders/internal/crew"
	"feeders/internal/npc"
	"feeders/internal/storage"
	"strings"
	"sync"
)

// CrewService preserves Savinykh's persistent historical mentor and adds the
// donor's four personas, scoped RAG, MCP catalog and optional local Ollama.
type CrewService struct {
	local      *npc.Local
	mentor     *Mentor
	mu         sync.Mutex
	store      storage.StateStore
	historical map[string]bool
}

func NewCrewService(local *npc.Local, mentor *Mentor) *CrewService {
	return &CrewService{local: local, mentor: mentor, historical: map[string]bool{}}
}
func (s *CrewService) Observe(player, action string) {
	if s.mentor != nil {
		s.mentor.Observe(player, action)
	}
}
func (s *CrewService) Talk(ctx context.Context, r Request) (Reply, error) {
	if r.AgentID == "savinykh" && r.DialogueMode == "" {
		memoryID := r.PlayerID
		if r.Scope != "" {
			memoryID = r.Scope + "/" + r.PlayerID
		}
		q := strings.ToLower(r.Text)
		s.mu.Lock()
		previousHistorical := s.historical[memoryID]
		s.mu.Unlock()
		explicit := strings.Contains(q, "салют") || strings.Contains(q, "истори") || strings.Contains(q, "1985")
		followup := strings.Contains(q, "проще") || strings.Contains(q, "подробнее") || strings.Contains(q, "продолжи")
		historical := explicit || (previousHistorical && followup)
		if historical && !s.local.ModelEnabled() {
			copy := r
			copy.PlayerID = memoryID
			answer, err := s.mentor.Talk(ctx, copy)
			answer.AgentName = "Виктор Петрович Савиных · капитан"
			if err == nil {
				s.mu.Lock()
				s.historical[memoryID] = true
				if s.store != nil {
					_ = s.store.SaveState("historical_topics", s.historical)
				}
				s.mu.Unlock()
			}
			return answer, err
		}
	}
	reply, err := s.local.Talk(ctx, npc.Request{PlayerID: r.PlayerID, AgentID: r.AgentID, Text: r.Text, Scope: r.Scope, DialogueMode: r.DialogueMode})
	if err != nil {
		return Reply{}, err
	}
	if r.AgentID == "savinykh" && r.DialogueMode == "" {
		s.mu.Lock()
		memoryID := r.PlayerID
		if r.Scope != "" {
			memoryID = r.Scope + "/" + r.PlayerID
		}
		s.historical[memoryID] = false
		s.mu.Unlock()
	}
	p, _ := crew.Find(r.AgentID)
	answer := Reply{DialogueMode: reply.DialogueMode, Retrieval: reply.Retrieval, Status: reply.Status, Grounding: reply.Grounding, Model: reply.Model, FallbackReason: reply.FallbackReason, ToolTransport: reply.ToolTransport, AgentID: reply.AgentID, AgentName: p.Name, Text: reply.Text, Evidence: reply.Evidence, Analysis: reply.Analysis, Mode: reply.Mode}
	answer.Command = reply.Command
	seenSources := map[string]bool{}
	for _, e := range reply.Evidence {
		if reply.Mode == "model" && !strings.Contains(reply.Text, "["+e.ID+"]") {
			continue
		}
		if seenSources[e.Source.ID] {
			continue
		}
		seenSources[e.Source.ID] = true
		mode := e.Source.ContentMode
		if mode == "" {
			mode = "summary"
		}
		if e.Source.ID == "survival-scenario-v1" {
			mode = "game_rules"
		}
		answer.Citations = append(answer.Citations, Citation{SourceTitle: e.Source.Title, SourceURL: e.Source.URL, DocumentID: e.Source.ID, Section: e.Source.Locator, ContentMode: mode, RelationToPerson: e.Source.RelationToPerson, Excerpt: "Материал из локальной базы", Confidence: "source_based"})
	}
	if len(answer.Citations) > 0 {
		answer.Citation = &answer.Citations[0]
	}
	return answer, nil
}

func (s *CrewService) WithMemory(store storage.StateStore) *CrewService {
	s.store = store
	_ = store.LoadState("historical_topics", &s.historical)
	return s
}
