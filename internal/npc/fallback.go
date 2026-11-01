package npc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"feeders/internal/crew"
	"feeders/internal/mission"
	"feeders/internal/rag"
)

// Configure before serving. Mission tools share the host's authoritative state.
func (s *Local) WithMission(m ViewSource) *Local { s.missions = m; return s }
func (s *Local) WithoutModel() *Local            { s.modelEnabled = false; return s }

func (s *Local) Talk(ctx context.Context, r Request) (Reply, error) {
	if !ValidDialogueMode(r.DialogueMode) {
		return Reply{}, errors.New("invalid dialogue_mode: use articles, casual or mission")
	}
	if _, ok := crew.Find(r.AgentID); !ok || strings.TrimSpace(r.PlayerID) == "" || len(r.PlayerID) > 128 || strings.TrimSpace(r.Text) == "" || len(r.Text) > 2048 {
		return Reply{}, errors.New("invalid player, agent or text")
	}
	if err := ctx.Err(); err != nil {
		return Reply{}, err
	}
	k := r.memoryKey()
	s.mu.Lock()
	if s.active[k] {
		s.mu.Unlock()
		return Reply{}, ErrBusy
	}
	s.active[k] = true
	s.mu.Unlock()
	select {
	case s.slots <- struct{}{}:
	case <-ctx.Done():
		s.mu.Lock()
		delete(s.active, k)
		s.mu.Unlock()
		return Reply{}, ctx.Err()
	}
	defer func() { s.mu.Lock(); delete(s.active, k); s.mu.Unlock(); <-s.slots }()
	if r.DialogueMode == DialogueMission {
		return s.talkCommands(ctx, r)
	}
	if s.missions != nil && r.DialogueMode != DialogueCasual {
		if v, err := s.missions.View(r.PlayerID); err == nil {
			ctx = mission.WithView(ctx, v)
		} else if !errors.Is(err, mission.ErrNotJoined) {
			return Reply{}, err
		}
	}
	var err error
	if r.DialogueMode != DialogueCasual {
		ctx, err = s.catalog.Bind(ctx)
		if err != nil {
			return Reply{}, err
		}
	}
	v, playing := mission.FromContext(ctx)
	playing = playing && r.DialogueMode != DialogueCasual
	if playing {
		scope := fmt.Sprint(v.State.Version) + ":" + v.AccessKey()
		s.mu.Lock()
		old := s.history[k]
		if old.scope != scope && (r.Scope == "" || previousAccessKey(old.scope) != v.AccessKey()) {
			delete(s.history, k)
		}
		s.mu.Unlock()
	} else {
		// A formerly joined actor must not keep mission-only facts in the
		// model's conversational context after leaving that access scope.
		s.mu.Lock()
		if s.history[k].scope != "" {
			delete(s.history, k)
		}
		s.mu.Unlock()
	}
	var reply Reply
	if r.DialogueMode == DialogueCasual {
		if s.modelEnabled {
			reply, err = s.talkCasual(ctx, r)
		} else {
			err = &modelError{code: "disabled", message: "Модель выключена на хосте. Для свободного общения перезапустите игру с OLLAMA_ENABLED=true."}
		}
	} else if r.DialogueMode == "" && !s.modelEnabled && isFollowup(r.Text) && s.retrievalQuery(r) == r.Text {
		reply = Reply{AgentID: r.AgentID, Text: "Сначала задайте вопрос по теме, затем я смогу объяснить его подробнее.", Status: "no_matches", Mode: "knowledge_base", Evidence: []rag.Chunk{}}
	} else if s.modelEnabled {
		reply, err = s.talkModel(ctx, r)
	} else {
		err = errors.New("model disabled")
	}
	if ctx.Err() != nil {
		return Reply{}, ctx.Err()
	}
	if errors.Is(err, ErrBusy) {
		return Reply{}, err
	}
	if playing && s.missions != nil {
		current, viewErr := s.missions.View(r.PlayerID)
		if viewErr != nil {
			return Reply{}, viewErr
		}
		if current.State.Version != v.State.Version || current.AccessKey() != v.AccessKey() {
			s.mu.Lock()
			delete(s.history, r.memoryKey())
			s.mu.Unlock()
			v = current
			ctx = mission.WithView(ctx, v)
			err = errors.New("mission changed during inference")
		}
	}
	fallbackReason := ""
	if err != nil && r.DialogueMode == DialogueCasual {
		fallbackReason = casualFailureReason(err)
		reply = Reply{AgentID: r.AgentID, Text: fallbackReason, Mode: "unavailable", Status: "model_unavailable", Grounding: "none"}
		err = nil
	} else if err != nil && s.modelEnabled {
		fallbackReason = modelFallbackReason(err)
		slog.Warn("local model fallback", "agent", r.AgentID, "model", s.model, "reason", err.Error())
	}
	if err != nil || reply.Mode == "" {
		reply, err = s.fromKnowledge(ctx, r)
		if err != nil {
			return Reply{}, err
		}
	}
	reply.Model = s.model
	reply.DialogueMode = r.DialogueMode
	reply.FallbackReason = fallbackReason
	if playing && r.DialogueMode == "" {
		reply.MissionVersion = v.State.Version
		reply.Analysis = s.analysis(ctx, r.AgentID, r.Text)
		if reply.Status == "no_matches" || reply.Status == "empty_corpus" {
			reply.Text = "В доступных источниках нет подтверждения по этому вопросу. Уточните тему. Текущее состояние корабля показано выше; доступные действия и их цены можно проверить в бортовом пульте."
			reply.Status = "rules"
			reply.Mode = "rules"
		}
		// State and numerical advice live in authoritative analysis and the HUD.
		// Do not prepend a resource report to every conversational greeting.
		if reply.Mode != "model" {
			reply.Text = mission.Summary(v) + "\n" + reply.Text
		}
	}
	s.mu.Lock()
	previous := append([]message(nil), s.history[k].messages...)
	s.mu.Unlock()
	if reply.Mode != "unavailable" {
		s.remember(k, append(previous, message{Role: "user", Content: r.Text}, message{Role: "assistant", Content: reply.Text}))
	}
	if playing {
		s.mu.Lock()
		old := s.history[k]
		old.scope = fmt.Sprint(v.State.Version) + ":" + v.AccessKey()
		s.history[k] = old
		s.saveMemoryLocked()
		s.mu.Unlock()
	}

	s.mu.Lock()
	saveErr := s.memoryError
	s.mu.Unlock()
	if saveErr != nil {
		return Reply{}, saveErr
	}
	return reply, nil
}
func (s *Local) fromKnowledge(ctx context.Context, r Request) (Reply, error) {
	query := s.retrievalQuery(r)
	found, transport, err := s.search(ctx, r, query)
	if err != nil {
		return Reply{}, err
	}

	reply := Reply{AgentID: r.AgentID, Status: found.Status, Mode: "knowledge_base", Grounding: "sources", Evidence: []rag.Chunk{}, ToolTransport: transport, Retrieval: &found.Retrieval}
	if found.Status != "ok" {
		if r.DialogueMode == DialogueArticles {
			return noArticleSources(reply), nil
		}
		if found.Status == "empty_corpus" {
			reply.Text = "Материалы для этого персонажа ещё не загружены."
		} else {
			reply.Text = "В доступных источниках нет подтверждения. Уточните вопрос или откройте необходимые факты."
		}
		return reply, nil
	}
	intro := "Найденные документальные материалы. Фрагменты приведены на языке источника:"
	for _, h := range found.Hits {
		if h.Source.ID == "survival-scenario-v1" {
			intro = "Справочные материалы сценария. Актуальные цены и прогнозы операций приведены отдельно в расчёте сервера:"
			break
		}
	}
	parts := []string{intro}
	for _, h := range found.Hits {
		reply.Evidence = append(reply.Evidence, h.Chunk)
		runes := []rune(h.Text)
		excerpt := h.Text
		if len(runes) > 600 {
			excerpt = string(runes[:600]) + "…"
		}
		parts = append(parts, excerpt+" ["+h.ID+"]")
	}
	reply.Text = strings.Join(parts, "\n\n")
	reply.Status = "grounded"
	return reply, nil
}

// Both model and fallback retrieval use the same player/persona-specific topic.
// A short follow-up must not silently bypass the model because "проще" alone
// matches no source. It must also never borrow another player's conversation.
func (s *Local) retrievalQuery(r Request) string {
	query := r.Text
	if !isFollowup(query) {
		return query
	}
	s.mu.Lock()
	old := append([]message(nil), s.history[r.memoryKey()].messages...)
	s.mu.Unlock()
	for i := len(old) - 1; i >= 0; i-- {
		if old[i].Role != "user" {
			continue
		}
		if isFollowup(old[i].Content) || socialQuestion(old[i].Content) {
			continue
		}
		if combined := query + " " + old[i].Content; len(combined) <= 2048 {
			return combined
		}
		break
	}
	return query
}

func isFollowup(text string) bool {
	switch strings.Trim(strings.ToLower(strings.TrimSpace(text)), ".!?") {
	case "объясни проще", "проще", "подробнее", "продолжи", "почему", "а почему", "почему так", "почему это важно", "как это работает", "объясни подробнее", "расскажи подробнее", "что это значит", "а дальше", "и дальше", "дальше", "поясни", "повтори", "объясни это":
		return true
	}
	return false
}

func (s *Local) analysis(ctx context.Context, id, text string) map[string]any {
	out := map[string]any{}
	names := []string{"mission_status"}
	text = strings.ToLower(text)
	switch id {
	case mission.Captain:
		names = append(names, "rescue_plans", "check_evidence")
	case mission.Engineer:
		names = append(names, "systems_diagnosis")
		if strings.Contains(text, "дрон") || strings.Contains(text, "маршрут") {
			names = append(names, "quest_options")
		}
	case mission.Biologist:
		names = append(names, "oxygen_forecast")
		if strings.Contains(text, "инфекц") || strings.Contains(text, "проб") {
			names = append(names, "analyze_samples")
		}
	case mission.Navigator:
		names = append(names, "archive_timeline", "compare_navigation")
	}
	for _, suffix := range names {
		args := json.RawMessage(`{}`)
		if suffix == "quest_options" {
			args = json.RawMessage(`{"quest":"drone"}`)
		}
		v, err := s.catalog.Call(ctx, id+"_"+suffix, args)
		if err != nil {
			out[suffix] = map[string]string{"unavailable": err.Error()}
		} else {
			out[suffix] = v
		}
	}
	return out
}

// Investigation prose is restricted to complete, verbatim sentences selected
// from visible RAG; negations cannot be stripped to invent a confirmed clue.
func extractive(answer string, evidence []rag.Chunk) bool {
	text := citationPattern.ReplaceAllString(answer, "")
	normalize := func(s string) string { return strings.Join(strings.Fields(s), " ") }
	split := func(s string) []string {
		return strings.FieldsFunc(s, func(r rune) bool { return r == '\n' || r == '.' || r == '!' || r == '?' })
	}
	allowed := map[string]bool{}
	for _, c := range evidence {
		for _, sentence := range split(c.Text) {
			allowed[normalize(sentence)] = true
		}
	}
	count := 0
	for _, line := range split(text) {
		line = strings.TrimSpace(line)
		if line != "" && !allowed[normalize(line)] {
			return false
		}
		if line != "" {
			count++
		}
	}
	return count > 0
}

func hasScenarioEvidence(evidence []rag.Chunk) bool {
	for _, c := range evidence {
		if c.Source.ID == "survival-scenario-v1" {
			return true
		}
	}
	return false
}

func (s *Local) ModelEnabled() bool { return s.modelEnabled }
