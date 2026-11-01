package game

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"feeders/internal/realtime"
	"feeders/internal/storage"
	"feeders/pkg/protocol"
)

type RoomMember struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Role  string `json:"role"`
	Ready bool   `json:"ready"`
}
type Proposal struct {
	Action   string          `json:"action"`
	Version  uint64          `json:"version"`
	Voters   []string        `json:"voters"`
	Approved map[string]bool `json:"approved"`
}
type ChatMessage struct {
	Name string `json:"name"`
	Text string `json:"text"`
	At   int64  `json:"at"`
}
type Receipt struct {
	Fingerprint string    `json:"fingerprint"`
	State       GameState `json:"state"`
}
type Room struct {
	Universe             *Universe                    `json:"universe,omitempty"`
	UniverseChatReceipts []UniverseChatReceipt        `json:"universe_chat_receipts,omitempty"`
	Experiment           *Experiment                  `json:"experiment,omitempty"`
	Learning             map[string]LearningResult    `json:"learning,omitempty"`
	Dialogues            map[string][]DialogueMessage `json:"dialogues"`
	Code                 string                       `json:"code"`
	Name                 string                       `json:"name"`
	Owner                string                       `json:"owner"`
	Started              bool                         `json:"started"`
	Generation           int                          `json:"generation"`
	Members              map[string]*RoomMember       `json:"members"`
	State                GameState                    `json:"state"`
	Proposal             *Proposal                    `json:"proposal,omitempty"`
	Chat                 []ChatMessage                `json:"chat"`
	Receipts             map[string]Receipt           `json:"receipts"`
	engine               *ScenarioEngine
}
type roomDisk struct {
	Rooms      map[string]*Room  `json:"rooms"`
	Membership map[string]string `json:"membership"`
}

func (m *Module) WithPersistence(s storage.StateStore) error {
	m.roomMu.Lock()
	defer m.roomMu.Unlock()
	m.stateStore = s
	m.strictRooms = true
	d := roomDisk{}
	if err := s.LoadState("rooms_v2", &d); err != nil && !errors.Is(err, storage.ErrNotFound) {
		return err
	}
	if d.Rooms != nil {
		m.rooms = d.Rooms
		m.membership = d.Membership
		if m.membership == nil {
			m.membership = map[string]string{}
		}
		for _, r := range m.rooms {
			u := universeOf(r)
			for i := range u.Objects {
				u.Objects[i].HeldBy = ""
			}
			r.engine = &ScenarioEngine{state: r.State}
			if r.Receipts == nil {
				r.Receipts = map[string]Receipt{}
			}
		}
	}
	return nil
}
func (m *Module) persistRoomsLocked() error {
	if m.stateStore == nil {
		return nil
	}
	for _, r := range m.rooms {
		r.State = r.engine.GetState()
	}
	return m.stateStore.SaveState("rooms_v2", roomDisk{m.rooms, m.membership})
}
func (m *Module) RoomOf(id string) string {
	m.roomMu.Lock()
	defer m.roomMu.Unlock()
	return m.membership[id]
}
func (m *Module) memoryScope(id string) string {
	m.roomMu.Lock()
	defer m.roomMu.Unlock()
	if r := m.rooms[m.membership[id]]; r != nil {
		return fmt.Sprintf("%s:%d", r.Code, r.Generation)
	}
	return ""
}
func (m *Module) engineFor(id string) *ScenarioEngine {
	m.roomMu.Lock()
	defer m.roomMu.Unlock()
	if r := m.rooms[m.membership[id]]; r != nil {
		return r.engine
	}
	return m.scenario
}
func (m *Module) roomViewLocked(r *Room) map[string]any {
	members := []any{}
	for _, p := range r.Members {
		online := m.isReady(p.ID)
		members = append(members, map[string]any{"id": p.ID, "name": p.Name, "role": p.Role, "ready": p.Ready, "online": online})
	}
	view := map[string]any{"code": r.Code, "name": r.Name, "owner": r.Owner, "started": r.Started, "generation": r.Generation, "members": members, "proposal": r.Proposal, "experiment": r.Experiment, "chat": r.Chat}
	data, _ := json.Marshal(view)
	copy := map[string]any{}
	json.Unmarshal(data, &copy)
	if experiment, ok := copy["experiment"].(map[string]any); ok && !r.Experiment.Ran {
		if predictions, ok := experiment["predictions"].(map[string]any); ok {
			for id := range predictions {
				predictions[id] = "submitted"
			}
		}
	}
	return copy
}
func (m *Module) publishRoom(code string) {
	defer m.publishUniverse(code)
	m.roomMu.Lock()
	r := m.rooms[code]
	if r == nil {
		m.roomMu.Unlock()
		return
	}
	view := m.roomViewLocked(r)
	state := r.engine.GetState()
	ids := []string{}
	for id := range r.Members {
		ids = append(ids, id)
	}
	m.roomMu.Unlock()
	if m.hub != nil {
		for _, id := range ids {
			m.hub.Publish(id, "room.state", view)
			m.hub.Publish(id, "game.state", state)
		}
	}
}
func (m *Module) sendRoom(s realtime.Session, id string) {
	m.roomMu.Lock()
	r := m.rooms[m.membership[s.User().ID]]
	if r != nil {
		s.Send("room.state", id, m.roomViewLocked(r))
		s.Send("game.state", "", r.engine.GetState())
		s.Send("universe.state", "", snapshotUniverse(r))
	} else {
		s.Send("room.state", id, map[string]any{"code": ""})
	}
	m.roomMu.Unlock()
}

func (m *Module) roomCommand(ctx context.Context, s realtime.Session, e protocol.Envelope) {
	if !m.isReady(s.User().ID) {
		sendError(s, e, "not_initialized", "Сначала войдите в игру")
		return
	}
	var p struct {
		Code  string `json:"code"`
		Name  string `json:"name"`
		Role  string `json:"role"`
		Ready bool   `json:"ready"`
	}
	if err := protocol.DecodePayload(e.Payload, &p); err != nil {
		sendError(s, e, "invalid_payload", "Некорректная команда комнаты")
		return
	}
	id := s.User().ID
	if e.Type == "room.get" {
		m.sendRoom(s, e.RequestID)
		return
	}
	m.roomMu.Lock()
	oldCode := m.membership[id]
	r := m.rooms[oldCode]
	fail := func(msg string) { m.roomMu.Unlock(); sendError(s, e, "room_error", msg) }
	switch e.Type {
	case "room.create", "room.join":
		if e.Type == "room.create" {
			if len(m.rooms) >= 100 {
				fail("Достигнут лимит партий на хосте")
				return
			}
			var b [4]byte
			if _, err := rand.Read(b[:]); err != nil {
				fail("Не удалось создать код")
				return
			}
			code := strings.ToUpper(hex.EncodeToString(b[:]))[:6]
			for m.rooms[code] != nil {
				rand.Read(b[:])
				code = strings.ToUpper(hex.EncodeToString(b[:]))[:6]
			}
			name := strings.TrimSpace(p.Name)
			if name == "" {
				name = "Экспедиция «Горизонт-7»"
			}
			if len([]rune(name)) > 60 {
				fail("Название не длиннее 60 символов")
				return
			}
			r = &Room{Code: code, Name: name, Owner: id, Generation: 1, Members: map[string]*RoomMember{}, Receipts: map[string]Receipt{}, Chat: []ChatMessage{}, engine: NewScenarioEngine()}
			m.rooms[code] = r
		} else {
			r = m.rooms[strings.ToUpper(strings.TrimSpace(p.Code))]
			if r == nil {
				fail("Комната с таким кодом не найдена")
				return
			}
			if len(r.Members) >= 4 && r.Members[id] == nil {
				fail("В комнате уже четыре участника")
				return
			}
		}
		if old := m.rooms[oldCode]; old != nil && old.Code != r.Code {
			m.releaseUniverseHeldLocked(old, id)
			delete(old.Members, id)
			old.Proposal = nil
			if old.Owner == id {
				for next := range old.Members {
					old.Owner = next
					break
				}
			}
		}
		if r.Members[id] == nil {
			r.Members[id] = &RoomMember{ID: id, Name: s.User().Username}
		}
		m.membership[id] = r.Code
	case "room.leave":
		if r == nil {
			fail("Вы не в комнате")
			return
		}
		m.releaseUniverseHeldLocked(r, id)
		delete(r.Members, id)
		delete(m.membership, id)
		r.Proposal = nil
		if r.Owner == id {
			for next := range r.Members {
				r.Owner = next
				break
			}
		}
	case "room.role":
		if r == nil {
			fail("Выберите комнату")
			return
		}
		if r.Started && r.Members[id].Role != "" {
			fail("Роль закреплена до завершения партии")
			return
		}
		if p.Role != "captain" && p.Role != "engineer" && p.Role != "biologist" && p.Role != "navigator" && p.Role != "commander" {
			fail("Неизвестная роль")
			return
		}
		for other, v := range r.Members {
			if other != id && v.Role == p.Role {
				fail("Эта роль уже занята")
				return
			}
		}
		r.Members[id].Role = p.Role
		r.Members[id].Ready = r.Started
	case "room.ready":
		if r == nil || r.Members[id].Role == "" {
			fail("Сначала выберите роль")
			return
		}
		r.Members[id].Ready = p.Ready
	case "room.start":
		if r == nil || r.Owner != id {
			fail("Миссию запускает создатель комнаты")
			return
		}
		for _, v := range r.Members {
			if !v.Ready || v.Role == "" {
				fail("Все участники должны выбрать роль и подтвердить готовность")
				return
			}
		}
		r.Started = true
	case "room.reset":
		if r == nil || r.Owner != id {
			fail("Новую миссию начинает создатель комнаты")
			return
		}
		r.Generation++
		r.Universe = newUniverse()
		r.UniverseChatReceipts = nil
		r.engine = NewScenarioEngine()
		r.Started = false
		r.Receipts = map[string]Receipt{}
		r.Chat = []ChatMessage{}
		r.Proposal = nil
		r.Dialogues = map[string][]DialogueMessage{}
		r.Experiment = nil
		r.Learning = map[string]LearningResult{}
		for _, v := range r.Members {
			v.Ready = false
		}
	default:
		fail("Неизвестная команда")
		return
	}
	code := ""
	if r != nil {
		code = r.Code
	}
	err := m.persistRoomsLocked()
	m.roomMu.Unlock()
	if err != nil {
		m.failure(s, e, err)
		return
	}
	m.sendRoom(s, e.RequestID)
	m.publishRoom(code)
	if oldCode != "" && oldCode != code {
		m.publishRoom(oldCode)
	}
	m.exportCrewView()
}

func allowedRole(role, action string) bool {
	if role == "commander" {
		return true
	}
	if strings.HasPrefix(action, "final_") || action == "wait_cycle" {
		return role == "captain"
	}
	switch action {
	case "repair_cooling", "start_reactor", "use_battery":
		return role == "engineer" || role == "captain"
	case "start_greenhouse":
		return role == "biologist" || role == "captain"
	case "restore_archive", "calculate_course":
		return role == "navigator" || role == "captain"
	}
	return false
}
func (m *Module) roomAction(ctx context.Context, s realtime.Session, e protocol.Envelope, p protocol.GameActionRequest) bool {
	m.roomMu.Lock()
	r := m.rooms[m.membership[s.User().ID]]
	if r == nil {
		m.roomMu.Unlock()
		if m.strictRooms {
			sendError(s, e, "not_joined", "Выберите комнату")
			return true
		}
		return false
	}
	id := s.User().ID
	if !m.validCommandScope(ctx, r, id) {
		m.roomMu.Unlock()
		sendError(s, e, "stale_scope", "Экспедиция изменилась; повторите поручение в текущей миссии")
		return true
	}
	if p.Action == "get_state" {
		state := r.engine.GetState()
		m.roomMu.Unlock()
		s.Send("game.state", e.RequestID, state)
		return true
	}
	fail := func(code, msg string) { m.roomMu.Unlock(); sendError(s, e, code, msg) }
	if !r.Started {
		fail("not_started", "Все участники должны подготовиться и начать миссию")
		return true
	}
	fingerprint := p.Action + fmt.Sprint(p.ExpectedVersion)
	key := id + ":" + e.RequestID
	if got, ok := r.Receipts[key]; ok {
		if got.Fingerprint != fingerprint {
			fail("request_conflict", "ID запроса уже использован для другой операции")
		} else {
			m.roomMu.Unlock()
			s.Send("game.state", e.RequestID, got.State)
		}
		return true
	}
	before := r.engine.GetState()
	if p.ExpectedVersion == 0 || p.ExpectedVersion != before.Version {
		fail("stale_version", "Состояние изменилось. Проверьте ресурсы и подтвердите заново")
		return true
	}
	if !allowedRole(r.Members[id].Role, p.Action) {
		fail("forbidden_role", "Операция недоступна вашей роли. Обратитесь к специалисту или капитану")
		return true
	}
	if strings.HasPrefix(p.Action, "final_") && len(r.Members) > 1 {
		preview := &ScenarioEngine{state: before}
		if _, err := preview.ExecuteAction(p.Action); err != nil {
			fail("action_failed", err.Error())
			return true
		}
		voters := []string{}
		for actor := range r.Members {
			if m.isReady(actor) && r.Members[actor].Role != "" {
				voters = append(voters, actor)
			}
		}
		if len(voters) > 1 {
			previousProposal := r.Proposal
			r.Proposal = &Proposal{Action: p.Action, Version: before.Version, Voters: voters, Approved: map[string]bool{id: true}}
			err := m.persistRoomsLocked()
			if err != nil {
				r.Proposal = previousProposal
			}
			code := r.Code
			m.roomMu.Unlock()
			if err != nil {
				m.failure(s, e, err)
				return true
			}
			s.Send("decision.pending", e.RequestID, map[string]bool{"pending": true})
			m.publishRoom(code)
			return true
		}
	}
	state, err := r.engine.ExecuteAction(p.Action)
	if err != nil {
		fail("action_failed", err.Error())
		return true
	}
	previousProposal := r.Proposal
	r.Proposal = nil
	r.Receipts[key] = Receipt{fingerprint, state}
	err = m.persistRoomsLocked()
	if err != nil {
		r.engine.mu.Lock()
		r.engine.state = before
		r.engine.mu.Unlock()
		r.State = before
		delete(r.Receipts, key)
		r.Proposal = previousProposal
	}
	code := r.Code
	m.roomMu.Unlock()
	if err != nil {
		m.failure(s, e, err)
		return true
	}
	m.observe(id, p.Action)
	s.Send("game.state", e.RequestID, state)
	m.publishRoom(code)
	m.exportCrewView()
	return true
}
func (m *Module) observe(id, action string) {
	if observer, ok := m.agents.(interface{ Observe(string, string) }); ok {
		memoryID := id
		if scope := m.memoryScope(id); scope != "" {
			memoryID = scope + "/" + id
		}
		observer.Observe(memoryID, action)
	}
}
func (m *Module) roomSolve(ctx context.Context, s realtime.Session, e protocol.Envelope, p protocol.InvestigationSolveRequest) bool {
	m.roomMu.Lock()
	r := m.rooms[m.membership[s.User().ID]]
	if r == nil {
		m.roomMu.Unlock()
		if m.strictRooms {
			sendError(s, e, "not_joined", "Выберите комнату")
			return true
		}
		return false
	}
	if !m.validCommandScope(ctx, r, s.User().ID) || !r.Started || p.ExpectedVersion == 0 || p.ExpectedVersion != r.engine.GetState().Version {
		m.roomMu.Unlock()
		sendError(s, e, "stale_version", "Начните миссию и проверьте актуальную версию")
		return true
	}
	before := r.engine.GetState()
	solved, msg, err := r.engine.SolveInvestigation(p.Clues)
	if err == nil {
		err = m.persistRoomsLocked()
		if err != nil {
			r.engine.mu.Lock()
			r.engine.state = before
			r.engine.mu.Unlock()
			r.State = before
		}
	}
	code := r.Code
	m.roomMu.Unlock()
	if err != nil {
		sendError(s, e, "solve_failed", err.Error())
		return true
	}
	s.Send("investigation.result", e.RequestID, map[string]any{"solved": solved, "message": msg})
	m.publishRoom(code)
	m.exportCrewView()
	return true
}
func (m *Module) vote(ctx context.Context, s realtime.Session, e protocol.Envelope) {
	var p struct {
		Approve bool   `json:"approve"`
		Version uint64 `json:"expected_version"`
	}
	if err := protocol.DecodePayload(e.Payload, &p); err != nil {
		sendError(s, e, "invalid_payload", "Некорректное голосование")
		return
	}
	id := s.User().ID
	m.roomMu.Lock()
	r := m.rooms[m.membership[id]]
	if r == nil || r.Proposal == nil {
		m.roomMu.Unlock()
		sendError(s, e, "no_proposal", "Нет активного решения")
		return
	}
	proposal := r.Proposal
	allowed := false
	for _, actor := range proposal.Voters {
		if actor == id {
			allowed = true
		}
	}
	if !allowed || proposal.Version != p.Version || r.engine.GetState().Version != p.Version {
		m.roomMu.Unlock()
		sendError(s, e, "stale_version", "Решение устарело или вы не участник голосования")
		return
	}
	before := r.engine.GetState()
	previousProposal := *proposal
	previousProposal.Approved = map[string]bool{}
	for actor, approved := range proposal.Approved {
		previousProposal.Approved[actor] = approved
	}
	if !p.Approve {
		r.Proposal = nil
	} else {
		proposal.Approved[id] = true
		all := true
		for _, actor := range proposal.Voters {
			if !proposal.Approved[actor] {
				all = false
			}
		}
		if all {
			if _, err := r.engine.ExecuteAction(proposal.Action); err != nil {
				m.roomMu.Unlock()
				sendError(s, e, "action_failed", err.Error())
				return
			}
			r.Proposal = nil
		}
	}
	err := m.persistRoomsLocked()
	code := r.Code
	if err != nil {
		r.engine.mu.Lock()
		r.engine.state = before
		r.engine.mu.Unlock()
		r.State = before
		r.Proposal = &previousProposal
	}
	view := m.roomViewLocked(r)
	m.roomMu.Unlock()
	if err != nil {
		m.failure(s, e, err)
		return
	}
	s.Send("room.state", e.RequestID, view)
	m.publishRoom(code)
	m.exportCrewView()
}
func (m *Module) chat(ctx context.Context, s realtime.Session, e protocol.Envelope) {
	var p struct {
		Text string `json:"text"`
	}
	if err := protocol.DecodePayload(e.Payload, &p); err != nil || len([]rune(strings.TrimSpace(p.Text))) == 0 || len([]rune(p.Text)) > 800 {
		sendError(s, e, "invalid_payload", "Сообщение: от 1 до 800 символов")
		return
	}
	m.roomMu.Lock()
	r := m.rooms[m.membership[s.User().ID]]
	if r == nil {
		m.roomMu.Unlock()
		sendError(s, e, "not_joined", "Выберите комнату")
		return
	}
	msg := ChatMessage{Name: s.User().Username, Text: strings.TrimSpace(p.Text), At: time.Now().UnixMilli()}
	if n := len(r.Chat); n > 0 && r.Chat[n-1].Name == msg.Name && msg.At-r.Chat[n-1].At < 800 {
		m.roomMu.Unlock()
		sendError(s, e, "rate_limited", "Подождите перед следующим сообщением")
		return
	}
	r.Chat = append(r.Chat, msg)
	if len(r.Chat) > 80 {
		r.Chat = r.Chat[len(r.Chat)-80:]
	}
	err := m.persistRoomsLocked()
	code := r.Code
	m.roomMu.Unlock()
	if err != nil {
		m.failure(s, e, err)
		return
	}
	s.Send("chat.sent", e.RequestID, msg)
	m.publishRoom(code)
}
