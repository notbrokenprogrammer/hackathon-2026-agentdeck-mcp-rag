package game

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"

	"feeders/internal/agents"
	"feeders/internal/realtime"
	"feeders/pkg/protocol"
)

// Universe is a small authoritative, room-scoped overlay on the streamed ship.
// It is persisted with the room; client performance samples are deliberately not.
type Universe struct {
	LayoutVersion int              `json:"layout_version"`
	Version       uint64           `json:"version"`
	Settings      UniverseSettings `json:"settings"`
	Agents        []UniverseAgent  `json:"agents"`
	Objects       []UniverseObject `json:"objects"`
	LastEvent     UniverseEvent    `json:"last_event"`
}

// Scope belongs to the wire snapshot, never to persisted Universe data. A late
// event remains identifiable after a member switches rooms or resets a mission.
type UniverseSnapshot struct {
	*Universe
	RoomCode   string `json:"room_code"`
	Generation int    `json:"generation"`
}

func snapshotUniverse(r *Room) UniverseSnapshot {
	return UniverseSnapshot{Universe: cloneUniverse(universeOf(r)), RoomCode: r.Code, Generation: r.Generation}
}

type UniverseSettings struct {
	AmbientColor    string  `json:"ambient_color"`
	LightIntensity  float64 `json:"light_intensity"`
	NPCCount        int     `json:"npc_count"`
	SimulationSpeed float64 `json:"simulation_speed"`
}
type UniverseAgent struct {
	ID       string `json:"id"`
	Behavior string `json:"behavior"`
}
type UniverseObject struct {
	ID         string  `json:"id"`
	Kind       string  `json:"kind"`
	Name       string  `json:"name"`
	X          float64 `json:"x"`
	Y          float64 `json:"y"`
	Z          float64 `json:"z"`
	Rotation   float64 `json:"rotation"`
	Open       bool    `json:"open"`
	MotionAt   int64   `json:"motion_at,omitempty"`
	MotionFrom float64 `json:"motion_from,omitempty"`
	Locked     string  `json:"locked,omitempty"`
	HeldBy     string  `json:"held_by"`
}
type UniverseChatReceipt struct {
	Key   string       `json:"key"`
	Agent string       `json:"agent"`
	Text  string       `json:"text"`
	Reply agents.Reply `json:"reply"`
}

type UniverseEvent struct {
	Text  string `json:"text"`
	Actor string `json:"actor"`
	At    int64  `json:"at"`
}
type ClientTelemetry struct {
	FPS       float64 `json:"fps"`
	PingMS    float64 `json:"ping_ms"`
	FrameMS   float64 `json:"frame_ms"`
	DrawCalls int     `json:"draw_calls"`
	Triangles int     `json:"triangles"`
	Renderer  string  `json:"renderer"`
	Hidden    bool    `json:"hidden"`
	UpdatedAt int64   `json:"updated_at"`
}

func newUniverse() *Universe {
	u := &Universe{LayoutVersion: 1, Version: 1, Settings: UniverseSettings{"#e2ebf4", 1, 16, 1},
		Agents: []UniverseAgent{{"savinykh", "helpful"}, {"dzhanibekov", "helpful"}, {"ryazansky", "helpful"}, {"krikalev", "helpful"}},
		Objects: []UniverseObject{
			{ID: "crate-cryo", Kind: "crate", Name: "Контейнер снабжения", X: -13, Z: 2},
			{ID: "crate-bridge", Kind: "crate", Name: "Ящик инструментов", X: -3, Z: 3},
			{ID: "plant-bio", Kind: "plant", Name: "Гидропонный модуль", X: -14, Z: -14},
			{ID: "beacon-reactor", Kind: "beacon", Name: "Сервисный маяк", X: 16, Z: 3},
		}, LastEvent: UniverseEvent{Text: "Среда готова к исследованию"}}
	doors := architecturalDoors()
	u.Objects = append([]UniverseObject{doors[0]}, u.Objects...)
	u.Objects = append(u.Objects, doors[1:]...)
	return u
}
func universeOf(r *Room) *Universe {
	if r.Universe == nil {
		r.Universe = newUniverse()
	}
	if r.Universe.LayoutVersion < 1 {
		// Add architectural doors once; preserve existing object state and mission data.
		for _, door := range architecturalDoors() {
			found := false
			for i, o := range r.Universe.Objects {
				if o.ID == door.ID {
					r.Universe.Objects[i].X = door.X
					r.Universe.Objects[i].Z = door.Z
					r.Universe.Objects[i].Rotation = door.Rotation
					found = true
					break
				}
			}
			if !found {
				r.Universe.Objects = append(r.Universe.Objects, door)
			}
		}
		for i := range r.Universe.Objects {
			o := &r.Universe.Objects[i]
			if o.Kind != "door" && o.Z < -11 && o.Z >= -23 && math.Abs(o.X) <= 7 {
				o.X -= 16
			}
		}
		if r.Universe.Settings.AmbientColor == "#78e8ca" {
			r.Universe.Settings.AmbientColor = "#e2ebf4"
		}
		r.Universe.LayoutVersion = 1
		r.Universe.Version++
	}
	return r.Universe
}
func cloneUniverse(u *Universe) *Universe {
	c := *u
	c.Agents = append([]UniverseAgent(nil), u.Agents...)
	c.Objects = append([]UniverseObject(nil), u.Objects...)
	if c.Objects == nil {
		c.Objects = []UniverseObject{}
	}
	return &c
}
func (m *Module) registerUniverse(h *realtime.Hub) {
	for _, kind := range []string{"universe.get", "universe.admin", "universe.interact", "universe.command"} {
		h.Handle(kind, m.universeCommand)
	}
	h.Handle("telemetry.report", m.telemetryReport)
	h.Handle("telemetry.get", m.telemetryGet)
}
func (m *Module) publishUniverse(code string) {
	m.roomMu.Lock()
	r := m.rooms[code]
	if r == nil {
		m.roomMu.Unlock()
		return
	}
	snapshot := snapshotUniverse(r)
	ids := make([]string, 0, len(r.Members))
	for id := range r.Members {
		ids = append(ids, id)
	}
	m.roomMu.Unlock()
	if m.hub != nil {
		for _, id := range ids {
			m.hub.Publish(id, "universe.state", snapshot)
		}
	}
}

// These optional fields form a patch; omitted values preserve existing settings.
type universeSettingsPatch struct {
	AmbientColor    *string  `json:"ambient_color"`
	LightIntensity  *float64 `json:"light_intensity"`
	NPCCount        *int     `json:"npc_count"`
	SimulationSpeed *float64 `json:"simulation_speed"`
}
type universeRequest struct {
	ExpectedVersion uint64                 `json:"expected_version"`
	Settings        *universeSettingsPatch `json:"settings"`
	Agent           *UniverseAgent         `json:"agent"`
	Spawn           *struct {
		Kind string  `json:"kind"`
		X    float64 `json:"x"`
		Y    float64 `json:"y"`
		Z    float64 `json:"z"`
	} `json:"spawn"`
	RemoveID string `json:"remove_id"`
	ObjectID string `json:"object_id"`
	Action   string `json:"action"`
	AgentID  string `json:"agent_id"`
	Text     string `json:"text"`
}

var universeColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

func finiteRange(v, lo, hi float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= lo && v <= hi
}
func universeFloor(x, z float64) bool {
	return finiteRange(x, -23, 24) && finiteRange(z, -24, 22) && deckWalkable(x, z)
}
func objectKind(kind string) bool {
	return kind == "door" || kind == "crate" || kind == "plant" || kind == "beacon"
}
func objectLabel(kind string) string {
	return map[string]string{"door": "Шлюз", "crate": "Контейнер", "plant": "Гидропонный модуль", "beacon": "Сервисный маяк"}[kind]
}
func behaviorOK(b string) bool {
	return b == "helpful" || b == "patrol" || b == "guard" || b == "passive"
}
func (m *Module) universeCommand(ctx context.Context, s realtime.Session, e protocol.Envelope) {
	if !m.isReady(s.User().ID) {
		sendError(s, e, "not_initialized", "Сначала войдите в игру")
		return
	}
	var p universeRequest
	if protocol.DecodePayload(e.Payload, &p) != nil {
		sendError(s, e, "invalid_payload", "Некорректная команда вселенной")
		return
	}
	id := s.User().ID
	// Read simulation positions before the room lock: the room owns state, the loop owns movement.
	position := m.worldPosition(id)
	positions := m.doorPlayerPositions()
	m.roomMu.Lock()
	r := m.rooms[m.membership[id]]
	fail := func(code, msg string) { m.roomMu.Unlock(); sendError(s, e, code, msg) }
	if r == nil || r.Members[id] == nil {
		fail("not_joined", "Выберите экспедицию")
		return
	}
	if !m.validCommandScope(ctx, r, id) {
		fail("stale_scope", "Экспедиция изменилась; повторите поручение")
		return
	}
	current := universeOf(r)
	if e.Type == "universe.get" {
		snapshot := snapshotUniverse(r)
		m.roomMu.Unlock()
		s.Send("universe.state", e.RequestID, snapshot)
		return
	}
	if e.Type == "universe.admin" && r.Owner != id {
		fail("forbidden", "Редактирование доступно владельцу экспедиции")
		return
	}
	if p.ExpectedVersion == 0 || p.ExpectedVersion != current.Version {
		fail("stale_version", "Мир изменился. Обновите состояние и повторите действие")
		return
	}
	candidate := cloneUniverse(current)
	message := ""
	switch e.Type {
	case "universe.admin":
		if p.ObjectID != "" || p.Action != "" || p.AgentID != "" || p.Text != "" {
			fail("invalid_payload", "Используйте поля редактора вселенной")
			return
		}
		if p.Settings == nil && p.Agent == nil && p.Spawn == nil && p.RemoveID == "" {
			fail("invalid_payload", "Не выбрано изменение")
			return
		}
		if patch := p.Settings; patch != nil {
			if patch.AmbientColor != nil {
				candidate.Settings.AmbientColor = *patch.AmbientColor
			}
			if patch.LightIntensity != nil {
				candidate.Settings.LightIntensity = *patch.LightIntensity
			}
			if patch.NPCCount != nil {
				candidate.Settings.NPCCount = *patch.NPCCount
			}
			if patch.SimulationSpeed != nil {
				candidate.Settings.SimulationSpeed = *patch.SimulationSpeed
			}
			v := candidate.Settings
			if !universeColor.MatchString(v.AmbientColor) || !finiteRange(v.LightIntensity, .2, 2) || v.NPCCount < 0 || v.NPCCount > 160 || !finiteRange(v.SimulationSpeed, 0, 2) {
				fail("invalid_payload", "Цвет #RRGGBB, свет 0.2–2, NPC 0–160, скорость 0–2")
				return
			}
		}
		if p.Agent != nil {
			found := false
			for i, a := range candidate.Agents {
				if a.ID == p.Agent.ID && behaviorOK(p.Agent.Behavior) {
					candidate.Agents[i] = *p.Agent
					found = true
				}
			}
			if !found {
				fail("invalid_payload", "Неизвестный агент или поведение")
				return
			}
		}
		if p.Spawn != nil {
			v := p.Spawn
			if !objectKind(v.Kind) || !universeFloor(v.X, v.Z) || !finiteRange(v.Y, 0, 3) || !m.Walkable(v.X, v.Z) || len(candidate.Objects) >= 80 {
				fail("invalid_payload", "Объект должен находиться на палубе; максимум 80 объектов")
				return
			}
			candidate.Objects = append(candidate.Objects, UniverseObject{ID: fmt.Sprintf("object-%d", candidate.Version+1), Kind: v.Kind, Name: objectLabel(v.Kind), X: v.X, Y: v.Y, Z: v.Z})
		}
		if p.RemoveID != "" {
			if isArchitecturalDoor(p.RemoveID) {
				fail("fixed_architecture", "Эта дверь является частью герметичного корпуса")
				return
			}
			found := false
			for i, o := range candidate.Objects {
				if o.ID == p.RemoveID {
					if o.HeldBy != "" {
						fail("object_held", "Сначала положите предмет")
						return
					}
					candidate.Objects = append(candidate.Objects[:i], candidate.Objects[i+1:]...)
					found = true
					break
				}
			}
			if !found {
				fail("not_found", "Объект уже отсутствует")
				return
			}
		}
		message = "Владелец обновил вселенную"
	case "universe.interact":
		if !r.Started {
			fail("not_started", "Сначала начните миссию")
			return
		}
		if p.Settings != nil || p.Agent != nil || p.Spawn != nil || p.RemoveID != "" || p.AgentID != "" || p.Text != "" {
			fail("invalid_payload", "Некорректное взаимодействие")
			return
		}
		index := -1
		for i, o := range candidate.Objects {
			if o.ID == p.ObjectID {
				index = i
				break
			}
		}
		if index < 0 {
			fail("not_found", "Объект уже отсутствует")
			return
		}
		o := &candidate.Objects[index]
		if p.Action != "drop" && math.Hypot(o.X-position.X, o.Z-position.Z) > 3.5 {
			fail("too_far", "Подойдите к объекту ближе чем на 3.5 м")
			return
		}
		switch p.Action {
		case "toggle":
			if o.Kind != "door" {
				fail("invalid_action", "Открывать можно только двери")
				return
			}
			now := time.Now().UnixMilli()
			if o.Locked != "" {
				fail("door_locked", o.Locked)
				return
			}
			if o.MotionAt > 0 && now-o.MotionAt < int64(deck.DoorDuration) {
				fail("door_moving", "Дождитесь завершения движения створок")
				return
			}
			if o.Open {
				for id, p := range positions {
					if r.Members[id] != nil && doorOccupied(*o, p[0], p[1]) {
						fail("door_occupied", "Проём занят. Отойдите от двери, чтобы закрыть её")
						return
					}
				}
			}
			setDoorTarget(o, !o.Open, now)
			message = "Шлюз закрыт"
			if o.Open {
				message = "Шлюз открыт"
			}
		case "pickup":
			if o.Kind != "crate" || o.HeldBy != "" {
				fail("invalid_action", "Контейнер уже занят или предмет нельзя взять")
				return
			}
			for _, held := range candidate.Objects {
				if held.HeldBy == id {
					fail("hands_full", "Сначала положите предмет из рук")
					return
				}
			}
			o.HeldBy = id
			message = "Контейнер взят"
		case "drop":
			if o.HeldBy != id {
				fail("forbidden", "Этот предмет не находится у вас в руках")
				return
			}
			o.HeldBy = ""
			o.X = position.X
			o.Y = 0
			o.Z = position.Z
			if universeFloor(position.X, position.Z+1.2) && m.Walkable(position.X, position.Z+1.2) {
				o.Z += 1.2
			}
			message = "Контейнер перемещён"
		default:
			fail("invalid_action", "Доступны: открыть, взять, положить")
			return
		}
	case "universe.command":
		if !r.Started {
			fail("not_started", "Сначала начните миссию")
			return
		}
		if p.Settings != nil || p.Agent != nil || p.Spawn != nil || p.RemoveID != "" || p.ObjectID != "" || p.Action != "" {
			fail("invalid_payload", "Некорректная команда агенту")
			return
		}
		var err error
		message, err = applyUniverseIntent(candidate, p.AgentID, p.Text)
		if err != nil {
			fail("command_rejected", err.Error())
			return
		}
	default:
		fail("invalid_payload", "Неизвестная команда")
		return
	}
	m.protectDoorOccupants(candidate, r, positions)
	candidate.Version++
	candidate.LastEvent = UniverseEvent{Text: message, Actor: s.User().Username, At: time.Now().UnixMilli()}
	r.Universe = candidate
	if err := m.persistRoomsLocked(); err != nil {
		r.Universe = current
		fail("save_failed", "Не удалось сохранить мир. Изменение отменено")
		return
	}
	code := r.Code
	snapshot := snapshotUniverse(r)
	m.roomMu.Unlock()
	if e.Type == "universe.command" {
		s.Send("universe.result", e.RequestID, map[string]any{"message": message, "changed": true, "version": snapshot.Version})
		s.Send("universe.state", "", snapshot)
	} else {
		s.Send("universe.state", e.RequestID, snapshot)
	}
	m.publishUniverse(code)
}

// A deliberately bounded classifier: only explicit imperatives can modify the
// environment. Questions, negation and arbitrary model output never execute.
func universeIntent(text string) string {
	text = strings.ToLower(strings.TrimSpace(text))
	if len([]rune(text)) == 0 || len([]rune(text)) > 800 || strings.Contains(text, "?") || strings.Contains(text, "не ") {
		return ""
	}
	has := func(parts ...string) bool {
		for _, p := range parts {
			if strings.Contains(text, p) {
				return true
			}
		}
		return false
	}
	if has("открой", "открыть") && has("двер", "шлюз") {
		return "open"
	}
	if has("закрой", "закрыть") && has("двер", "шлюз") {
		return "close"
	}
	if has("убери", "удали") && has("растен", "сад", "оранжер") {
		return "remove_plants"
	}
	if has("добавь", "создай", "поставь", "вырасти", "посади") {
		if has("растен", "сад", "оранжер") {
			return "plants"
		}
		if has("ящик", "контейнер") {
			return "crates"
		}
		if has("маяк") {
			return "beacon"
		}
	}
	if has("включи", "сделай") && has("аварийн") && has("свет", "освещ") {
		return "emergency"
	}
	if has("включи", "верни", "восстанови") && has("обычн", "нормальн") && has("свет", "освещ") {
		return "normal"
	}
	return ""
}
func applyUniverseIntent(u *Universe, agent, text string) (string, error) {
	behavior := ""
	for _, a := range u.Agents {
		if a.ID == agent {
			behavior = a.Behavior
		}
	}
	if behavior == "" {
		return "", fmt.Errorf("Выберите одного из четырёх наставников")
	}
	if behavior == "passive" {
		return "", fmt.Errorf("Агент переведён в режим наблюдения и не меняет мир")
	}
	intent := universeIntent(text)
	if intent == "" {
		return "", fmt.Errorf("Попробуйте: «Добавь растения», «Добавь ящики», «Открой двери» или «Добавь маяк»")
	}
	if behavior == "guard" && intent != "open" && intent != "close" && intent != "beacon" {
		return "", fmt.Errorf("Режим охраны: агент управляет шлюзами и маяками")
	}
	if behavior == "patrol" && intent != "beacon" && intent != "crates" {
		return "", fmt.Errorf("Режим патруля: агент размещает маяки и снабжение")
	}
	switch intent {
	case "open", "close":
		found := false
		for i := range u.Objects {
			if u.Objects[i].Kind == "door" && u.Objects[i].Locked == "" {
				setDoorTarget(&u.Objects[i], intent == "open", time.Now().UnixMilli())
				found = true
			}
		}
		if !found {
			return "", fmt.Errorf("В этой вселенной нет шлюзов")
		}
		if intent == "open" {
			return "Агент открыл шлюзы", nil
		}
		return "Агент закрыл шлюзы", nil
	case "emergency":
		u.Settings.AmbientColor = "#ff6b55"
		u.Settings.LightIntensity = .6
		return "Агент включил аварийное освещение", nil
	case "normal":
		u.Settings.AmbientColor = "#e2ebf4"
		u.Settings.LightIntensity = 1
		return "Агент восстановил обычное освещение", nil
	case "remove_plants":
		objects := []UniverseObject{}
		for _, o := range u.Objects {
			if o.Kind != "plant" {
				objects = append(objects, o)
			}
		}
		if len(objects) == len(u.Objects) {
			return "", fmt.Errorf("Растений для удаления нет")
		}
		u.Objects = objects
		return "Агент освободил место от растений", nil
	}
	kind := "beacon"
	count := 1
	x, z := 5., 3.
	if intent == "plants" {
		kind = "plant"
		count = 4
		x = -4
		z = -18
	}
	if intent == "crates" {
		kind = "crate"
		count = 3
		x = -18
		z = 3
	}
	if len(u.Objects)+count > 80 {
		return "", fmt.Errorf("Достигнут лимит 80 объектов: удалите лишние в редакторе")
	}
	for i := 0; i < count; i++ {
		u.Objects = append(u.Objects, UniverseObject{ID: fmt.Sprintf("generated-%d-%d", u.Version+1, i), Kind: kind, Name: objectLabel(kind), X: x + float64(i)*2, Y: 0, Z: z + float64((u.Version/3)%2)})
	}
	return fmt.Sprintf("Агент создал объекты: %s × %d", objectLabel(kind), count), nil
}

func (m *Module) telemetryReport(ctx context.Context, s realtime.Session, e protocol.Envelope) {
	if !m.isReady(s.User().ID) {
		sendError(s, e, "not_initialized", "Сначала войдите в игру")
		return
	}
	// updated_at is generated on the host, never taken from the browser.
	var p struct {
		FPS       float64 `json:"fps"`
		PingMS    float64 `json:"ping_ms"`
		FrameMS   float64 `json:"frame_ms"`
		DrawCalls int     `json:"draw_calls"`
		Triangles int     `json:"triangles"`
		Renderer  string  `json:"renderer"`
		Hidden    bool    `json:"hidden"`
	}
	if protocol.DecodePayload(e.Payload, &p) != nil || !finiteRange(p.FPS, 0, 1000) || !finiteRange(p.PingMS, 0, 60000) || !finiteRange(p.FrameMS, 0, 60000) || p.DrawCalls < 0 || p.DrawCalls > 1000000 || p.Triangles < 0 || p.Triangles > 100000000 || len(p.Renderer) > 80 {
		sendError(s, e, "invalid_payload", "Некорректные метрики клиента")
		return
	}
	now := time.Now().UnixMilli()
	id := s.User().ID
	m.roomMu.Lock()
	if m.rooms[m.membership[id]] == nil {
		m.roomMu.Unlock()
		sendError(s, e, "not_joined", "Выберите экспедицию")
		return
	}
	if m.telemetry == nil {
		m.telemetry = map[string]ClientTelemetry{}
	}
	if previous, ok := m.telemetry[id]; ok && now-previous.UpdatedAt < 500 {
		m.roomMu.Unlock()
		sendError(s, e, "rate_limited", "Метрики принимаются не чаще двух раз в секунду")
		return
	}
	m.telemetry[id] = ClientTelemetry{p.FPS, p.PingMS, p.FrameMS, p.DrawCalls, p.Triangles, p.Renderer, p.Hidden, now}
	m.roomMu.Unlock()
	s.Send("telemetry.accepted", e.RequestID, map[string]int64{"at": now})
}
func (m *Module) telemetryGet(ctx context.Context, s realtime.Session, e protocol.Envelope) {
	var empty struct{}
	if protocol.DecodePayload(e.Payload, &empty) != nil {
		sendError(s, e, "invalid_payload", "Некорректный запрос метрик")
		return
	}
	m.roomMu.Lock()
	r := m.rooms[m.membership[s.User().ID]]
	if r == nil || r.Owner != s.User().ID {
		m.roomMu.Unlock()
		sendError(s, e, "forbidden", "Метрики доступны владельцу экспедиции")
		return
	}
	now := time.Now().UnixMilli()
	clients := []map[string]any{}
	for id, member := range r.Members {
		sample, ok := m.telemetry[id]
		online := m.isReady(id)
		// Missing samples remain null: zero would incorrectly look like a measurement.
		row := map[string]any{"player_id": id, "name": member.Name, "online": online, "fps": nil, "ping_ms": nil, "frame_ms": nil, "draw_calls": nil, "triangles": nil, "renderer": "", "hidden": false, "updated_at": sample.UpdatedAt, "stale": !ok || !online || now-sample.UpdatedAt > 10000}
		if ok {
			row["fps"] = sample.FPS
			row["ping_ms"] = sample.PingMS
			row["frame_ms"] = sample.FrameMS
			row["draw_calls"] = sample.DrawCalls
			row["triangles"] = sample.Triangles
			row["renderer"] = sample.Renderer
			row["hidden"] = sample.Hidden
		}
		clients = append(clients, row)
	}
	sort.Slice(clients, func(i, j int) bool { return clients[i]["name"].(string) < clients[j]["name"].(string) })
	code, generation := r.Code, r.Generation
	m.roomMu.Unlock()
	s.Send("telemetry.state", e.RequestID, map[string]any{"clients": clients, "at": now, "room_code": code, "generation": generation})
}

// Called with roomMu held. A disconnected/departed carrier cannot permanently
// lock an object. The last grounded position remains a safe release location.
func (m *Module) releaseUniverseHeldLocked(r *Room, id string) bool {
	if r == nil || r.Universe == nil {
		return false
	}
	changed := false
	for i := range r.Universe.Objects {
		if r.Universe.Objects[i].HeldBy == id {
			r.Universe.Objects[i].HeldBy = ""
			changed = true
		}
	}
	if changed {
		r.Universe.Version++
	}
	return changed
}
func (m *Module) leaveUniverse(id string) {
	m.roomMu.Lock()
	r := m.rooms[m.membership[id]]
	delete(m.telemetry, id)
	changed := m.releaseUniverseHeldLocked(r, id)
	code := ""
	if r != nil {
		code = r.Code
	}
	if changed {
		if err := m.persistRoomsLocked(); err != nil {
			m.log.Error("save released objects", "error", err)
		}
	}
	m.roomMu.Unlock()
	if changed {
		m.publishUniverse(code)
	}
}
func (m *Module) universeBlocksMovement(id string, x, z float64) bool {
	from := m.worldPosition(id)
	m.roomMu.Lock()
	r := m.rooms[m.membership[id]]
	if r == nil || r.Universe == nil {
		m.roomMu.Unlock()
		return false
	}
	if math.Hypot(x-from.X, z-from.Z) > 30 {
		m.roomMu.Unlock()
		return true
	}
	now := time.Now().UnixMilli()
	changed := false
	// A player approaching during closing trips the safety sensor before the leaves.
	for i := range r.Universe.Objects {
		o := &r.Universe.Objects[i]
		if o.Kind == "door" && !o.Open && doorFraction(*o, now) > 0 && (doorOccupied(*o, from.X, from.Z) || doorOccupied(*o, x, z)) {
			setDoorTarget(o, true, now)
			changed = true
		}
	}
	blocked := false
	steps := int(math.Ceil(math.Hypot(x-from.X, z-from.Z) / .08))
	if steps < 1 {
		steps = 1
	}
	for _, o := range r.Universe.Objects {
		if o.Kind != "door" {
			continue
		}
		for n := 1; n <= steps; n++ {
			t := float64(n) / float64(steps)
			if doorBlocks(o, from.X+(x-from.X)*t, from.Z+(z-from.Z)*t, now) {
				blocked = true
				break
			}
		}
	}
	code := r.Code
	if changed {
		r.Universe.Version++
		_ = m.persistRoomsLocked()
	}
	m.roomMu.Unlock()
	if changed {
		m.publishUniverse(code)
	}
	return blocked
}

// Chat uses the same bounded command service as the explicit environment panel.
// Ordinary questions continue through the knowledge/LLM service unchanged.
func (m *Module) talkUniverse(s realtime.Session, e protocol.Envelope, agent, text string) bool {
	if universeIntent(text) == "" {
		return false
	}
	if !m.isReady(s.User().ID) {
		sendError(s, e, "not_initialized", "Сначала войдите в игру")
		return true
	}
	positions := m.doorPlayerPositions()
	m.roomMu.Lock()
	r := m.rooms[m.membership[s.User().ID]]
	if r == nil {
		m.roomMu.Unlock()
		return false
	}
	key := s.User().ID + ":" + e.RequestID
	for _, receipt := range r.UniverseChatReceipts {
		if receipt.Key == key {
			m.roomMu.Unlock()
			if receipt.Agent != agent || receipt.Text != text {
				sendError(s, e, "request_conflict", "ID запроса уже использован")
			} else {
				s.Send("agent.reply", e.RequestID, receipt.Reply)
			}
			return true
		}
	}
	if !r.Started {
		m.roomMu.Unlock()
		sendError(s, e, "not_started", "Сначала начните миссию")
		return true
	}
	current := universeOf(r)
	candidate := cloneUniverse(current)
	message, err := applyUniverseIntent(candidate, agent, text)
	if err != nil {
		m.roomMu.Unlock()
		sendError(s, e, "command_rejected", err.Error())
		return true
	}
	m.protectDoorOccupants(candidate, r, positions)
	candidate.Version++
	candidate.LastEvent = UniverseEvent{message, s.User().Username, time.Now().UnixMilli()}
	reply := agents.Reply{AgentID: agent, AgentName: map[string]string{"savinykh": "Виктор Савиных", "dzhanibekov": "Владимир Джанибеков", "ryazansky": "Сергей Рязанский", "krikalev": "Сергей Крикалёв"}[agent], Text: message, Mode: "world-command"}
	previousReceipts := r.UniverseChatReceipts
	nextReceipts := append([]UniverseChatReceipt(nil), previousReceipts...)
	nextReceipts = append(nextReceipts, UniverseChatReceipt{Key: key, Agent: agent, Text: text, Reply: reply})
	if len(nextReceipts) > 128 {
		nextReceipts = nextReceipts[len(nextReceipts)-128:]
	}
	r.UniverseChatReceipts = nextReceipts
	r.Universe = candidate
	if err = m.persistRoomsLocked(); err != nil {
		r.UniverseChatReceipts = previousReceipts
		r.Universe = current
		m.roomMu.Unlock()
		sendError(s, e, "save_failed", "Не удалось сохранить мир")
		return true
	}
	code := r.Code
	scope := fmt.Sprintf("%s:%d", r.Code, r.Generation)
	m.roomMu.Unlock()
	if err := m.recordDialogue(s.User().ID, scope, agent, text, reply); err != nil {
		m.log.Error("save world command dialogue", "error", err)
	}
	m.publishUniverse(code)
	s.Send("agent.reply", e.RequestID, reply)
	return true
}
