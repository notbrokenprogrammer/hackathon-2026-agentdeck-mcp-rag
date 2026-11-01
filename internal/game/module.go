// Package game wires protocol handlers to domain services, simulation loop and AI agents.
package game

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"feeders/internal/agents"
	"feeders/internal/model"
	"feeders/internal/realtime"
	"feeders/internal/storage"
	"feeders/internal/world"
	"feeders/pkg/protocol"
)

type Module struct {
	players        storage.Players
	world          world.Service
	agents         agents.Service
	scenario       *ScenarioEngine
	hub            *realtime.Hub
	log            *slog.Logger
	mu             sync.Mutex
	ready          map[string]model.PlayerState
	agentWork      chan struct{}
	agentBusy      map[string]bool
	crewProjection string
	projectionMu   sync.Mutex
	worldToolKey   string
	streamWorld    *StreamWorld
	strictRooms    bool
	roomMu         sync.Mutex
	rooms          map[string]*Room
	membership     map[string]string
	stateStore     storage.StateStore
	telemetry      map[string]ClientTelemetry
}

func (m *Module) WithAgents(s agents.Service) *Module { m.agents = s; return m }

// New creates a game module with authoritative world service and local AI agents service.
func New(players storage.Players, worldService world.Service, agentService agents.Service, log *slog.Logger) *Module {
	return &Module{
		rooms:      map[string]*Room{},
		membership: map[string]string{},
		players:    players,
		world:      worldService,
		agents:     agentService,
		scenario:   NewScenarioEngine(),
		log:        log,
		ready:      make(map[string]model.PlayerState),
		agentWork:  make(chan struct{}, 8),
		agentBusy:  make(map[string]bool),
	}
}

func (m *Module) Register(h *realtime.Hub) {
	m.hub = h
	m.registerUniverse(h)
	for _, kind := range []string{"room.get", "room.create", "room.join", "room.leave", "room.role", "room.ready", "room.start", "room.reset"} {
		h.Handle(kind, m.roomCommand)
	}
	h.Handle("experiment.preview", m.experimentPreview)
	h.Handle("experiment.start", m.experimentStart)
	h.Handle("experiment.command", m.experimentCommand)
	h.Handle("chat.send", m.chat)
	h.Handle("dialogue.history", m.dialogueHistory)
	h.Handle("decision.vote", m.vote)
	h.Handle("player.init", m.init)
	h.Handle("player.input", m.input)
	h.Handle("player.teleport", m.teleport)
	h.Handle("agent.talk", m.talk)
	h.Handle("game.action", m.action)
	h.Handle("game.preview", m.previewAction)
	h.Handle("investigation.solve", m.solve)
	h.OnDisconnect(m.leave)
}

func (m *Module) init(ctx context.Context, s realtime.Session, e protocol.Envelope) {
	// Legacy username is accepted for compatibility but identity comes from cookie.
	var input struct {
		ClientTime int64  `json:"client_time"`
		Username   string `json:"username"`
	}
	if err := protocol.DecodePayload(e.Payload, &input); err != nil {
		sendError(s, e, "invalid_payload", "Invalid player.init payload")
		return
	}

	id := s.User().ID
	m.mu.Lock()
	state, exists := m.ready[id]
	m.mu.Unlock()

	if !exists {
		var err error
		state, err = m.players.LoadPlayer(ctx, id)
		if errors.Is(err, storage.ErrNotFound) {
			// Спавн в криокапсуле станции «Горизонт-7» (жилой отсек [-15.0, 1.7, 0.0])
			state = model.PlayerState{
				PlayerID:  id,
				Position:  model.Position{X: -15.0, Y: 1.7, Z: 0.0},
				Yaw:       0,
				LastSeq:   0,
				UpdatedAt: time.Now().UTC(),
			}
			err = m.players.SavePlayer(ctx, state)
		}
		if err != nil {
			m.failure(s, e, err)
			return
		}

		if m.world != nil {
			state, err = m.world.Join(ctx, s.User(), state)
			if err != nil {
				m.failure(s, e, err)
				return
			}
		}

		state = m.repairDoorSpawn(state)
		m.mu.Lock()
		m.ready[id] = state
		m.mu.Unlock()
	}

	s.Send("player.ready", e.RequestID, protocol.PlayerReady{
		User:  s.User(),
		State: state,
		Capabilities: protocol.Capabilities{
			Movement: m.world != nil,
			World:    m.world != nil,
			Agents:   m.agents != nil,
		},
	})
	m.exportCrewView()
	m.publishRoom(m.RoomOf(id))

}

func (m *Module) input(ctx context.Context, s realtime.Session, e protocol.Envelope) {
	m.mu.Lock()
	state, ready := m.ready[s.User().ID]
	m.mu.Unlock()

	if !ready {
		sendError(s, e, "not_initialized", "Send player.init first")
		return
	}

	input, err := protocol.DecodeInput(e.Payload)
	if err != nil {
		sendError(s, e, "invalid_payload", "Invalid player.input payload")
		return
	}

	if m.world == nil {
		sendError(s, e, "feature_disabled", "Movement module is not connected")
		return
	}

	if m.universeBlocksMovement(s.User().ID, input.X, input.Z) {
		sendError(s, e, "door_closed", "Шлюз закрыт. Откройте его клавишей E")
		accepted := state
		if loop, ok := m.world.(interface{ GetAllPlayersStates() []model.PlayerState }); ok {
			for _, current := range loop.GetAllPlayersStates() {
				if current.PlayerID == s.User().ID {
					accepted = current
					break
				}
			}
		}
		s.Send("player.position", "", accepted)
		return
	}
	if err := m.world.Input(ctx, s.User().ID, input); err != nil {
		if loop, ok := m.world.(*ServerLoop); ok {
			for _, accepted := range loop.GetAllPlayersStates() {
				if accepted.PlayerID == s.User().ID {
					s.Send("player.position", "", accepted)
					break
				}
			}
		}
		sendError(s, e, "input_rejected", "Движение ограничено границей отсека. Позиция восстановлена.")
		return
	}

	// The authoritative world owns accepted coordinates; do not copy unchecked or
	// stale proposals into a second persistence state.
	_ = state
}

func (m *Module) talk(ctx context.Context, s realtime.Session, e protocol.Envelope) {
	var input struct {
		AgentID      string `json:"agent_id"`
		Text         string `json:"text"`
		DialogueMode string `json:"dialogue_mode,omitempty"`
	}
	if err := protocol.DecodePayload(e.Payload, &input); err != nil {
		sendError(s, e, "invalid_payload", "Invalid agent.talk payload")
		return
	}

	if !agents.ValidDialogueMode(input.DialogueMode) {
		sendError(s, e, "invalid_dialogue_mode", "Выберите режим articles, casual или mission")
		return
	}
	if input.DialogueMode == "" && m.talkUniverse(s, e, input.AgentID, input.Text) {
		return
	}
	if m.agents == nil {
		sendError(s, e, "feature_disabled", "AI agent service is not connected")
		return
	}

	id := s.User().ID
	m.mu.Lock()
	_, ready := m.ready[id]
	busy := m.agentBusy[id]
	if ready && !busy {
		m.agentBusy[id] = true
	}
	m.mu.Unlock()
	if !ready {
		sendError(s, e, "not_initialized", "Send player.init first")
		return
	}
	if busy {
		sendError(s, e, "agent_busy", "Дождитесь ответа наставника")
		return
	}
	select {
	case m.agentWork <- struct{}{}:
	default:
		m.mu.Lock()
		delete(m.agentBusy, id)
		m.mu.Unlock()
		sendError(s, e, "agent_busy", "Наставник занят, повторите вопрос")
		return
	}
	go func() {
		defer func() { <-m.agentWork; m.mu.Lock(); delete(m.agentBusy, id); m.mu.Unlock() }()
		talkCtx, cancel := context.WithTimeout(ctx, 70*time.Second)
		defer cancel()
		scope := m.memoryScope(id)
		if input.DialogueMode == "mission" {
			talkCtx = m.agentCommandContext(talkCtx, s, e, scope, input.AgentID)
		}
		reply, err := m.agents.Talk(talkCtx, agents.Request{PlayerID: id, AgentID: input.AgentID, Text: input.Text, Scope: scope, DialogueMode: input.DialogueMode})
		if err != nil {
			sendError(s, e, "agent_error", err.Error())
			return
		}
		if talkCtx.Err() == nil {
			reply.DialogueMode = input.DialogueMode
			if err := m.recordDialogue(id, scope, input.AgentID, input.Text, reply); err != nil {
				sendError(s, e, "save_failed", "Не удалось сохранить диалог")
				return
			}
			s.Send("agent.reply", e.RequestID, reply)
		}
	}()
}

func (m *Module) action(ctx context.Context, s realtime.Session, e protocol.Envelope) {
	if !m.isReady(s.User().ID) {
		sendError(s, e, "not_initialized", "Send player.init first")
		return
	}
	var input protocol.GameActionRequest
	if err := protocol.DecodePayload(e.Payload, &input); err != nil {
		sendError(s, e, "invalid_payload", "Invalid game.action payload")
		return
	}

	if m.scenario == nil {
		sendError(s, e, "feature_disabled", "Scenario engine is not initialized")
		return
	}

	if m.roomAction(ctx, s, e, input) {
		return
	}
	if input.Action == "get_state" {
		s.Send("game.state", e.RequestID, m.scenario.GetState())
		return
	}
	state, err := m.scenario.ExecuteAction(input.Action)
	if err != nil {
		sendError(s, e, "action_failed", err.Error())
		return
	}
	if observer, ok := m.agents.(interface{ Observe(string, string) }); ok {
		observer.Observe(s.User().ID, input.Action)
	}
	m.exportCrewView()

	if m.hub != nil {
		m.hub.Broadcast("game.state", state)
	}
	s.Send("game.state", e.RequestID, state)
}

func (m *Module) solve(ctx context.Context, s realtime.Session, e protocol.Envelope) {
	if !m.isReady(s.User().ID) {
		sendError(s, e, "not_initialized", "Send player.init first")
		return
	}
	var input protocol.InvestigationSolveRequest
	if err := protocol.DecodePayload(e.Payload, &input); err != nil {
		sendError(s, e, "invalid_payload", "Invalid investigation.solve payload")
		return
	}
	unique := map[string]bool{}
	for _, clue := range input.Clues {
		unique[clue] = true
	}
	if len(input.Clues) != 3 || len(unique) != 3 {
		sendError(s, e, "invalid_payload", "Выберите три разных доказательства")
		return
	}

	if m.scenario == nil {
		sendError(s, e, "feature_disabled", "Scenario engine is not initialized")
		return
	}

	if m.roomSolve(ctx, s, e, input) {
		return
	}
	solved, msg, err := m.scenario.SolveInvestigation(input.Clues)
	if err != nil {
		sendError(s, e, "solve_failed", err.Error())
		return
	}

	state := m.scenario.GetState()
	m.exportCrewView()
	if m.hub != nil {
		m.hub.Broadcast("game.state", state)
	} else {
		s.Send("game.state", e.RequestID, state)
	}

	s.Send("investigation.result", e.RequestID, map[string]any{
		"solved":  solved,
		"message": msg,
	})
}

func (m *Module) leave(ctx context.Context, user model.User) {
	m.leaveUniverse(user.ID)
	defer m.exportCrewView()
	defer func() { m.publishRoom(m.RoomOf(user.ID)) }()
	m.mu.Lock()
	state, exists := m.ready[user.ID]
	delete(m.ready, user.ID)
	m.mu.Unlock()

	if exists {
		if m.world != nil {
			if err := m.world.Leave(ctx, user.ID); err != nil {
				m.log.Error("world leave error", "player_id", user.ID, "error", err)
			}
			return
		}
		if err := m.players.SavePlayer(ctx, state); err != nil {
			m.log.Error("save departing player", "player_id", user.ID, "error", err)
		}
	}
}

func (m *Module) isReady(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.ready[id]
	return ok
}

func sendError(s realtime.Session, e protocol.Envelope, code, message string) {
	s.Send("error", e.RequestID, protocol.Error{Code: code, Message: message})
}

func (m *Module) failure(s realtime.Session, e protocol.Envelope, err error) {
	m.log.Error("initialize player", "error", err)
	sendError(s, e, "internal_error", "Cannot initialize player")
}
