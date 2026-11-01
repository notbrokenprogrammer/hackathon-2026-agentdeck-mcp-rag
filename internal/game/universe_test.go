package game

import (
	"context"
	"encoding/json"
	"math"
	"testing"
	"time"

	"feeders/internal/model"
	"feeders/pkg/protocol"
)

func universeCmd(t *testing.T, m *Module, s *fakeSession, kind string, p any) protocol.Envelope {
	t.Helper()
	e, err := protocol.Message(kind, "universe-request", p)
	if err != nil {
		t.Fatal(err)
	}
	m.universeCommand(context.Background(), s, e)
	return s.messages[len(s.messages)-1]
}
func universeError(t *testing.T, e protocol.Envelope, code string) {
	t.Helper()
	var p protocol.Error
	json.Unmarshal(e.Payload, &p)
	if e.Type != "error" || p.Code != code {
		t.Fatalf("want %s error, got %s %s", code, e.Type, e.Payload)
	}
}
func startUniverseRoom(t *testing.T, m *Module, s *fakeSession) {
	t.Helper()
	roomCmd(t, m, s, "room.create", map[string]any{})
	roomCmd(t, m, s, "room.role", map[string]string{"role": "commander"})
	roomCmd(t, m, s, "room.ready", map[string]bool{"ready": true})
	roomCmd(t, m, s, "room.start", map[string]any{})
}
func TestUniverseOwnerValidationIsolationAndRestart(t *testing.T) {
	dir := t.TempDir()
	m, store := roomFixture(t, dir)
	a := attach(t, m, "owner")
	b := attach(t, m, "member")
	c := attach(t, m, "other")
	startUniverseRoom(t, m, a)
	code := m.RoomOf(a.user.ID)
	roomCmd(t, m, b, "room.join", map[string]string{"code": code})
	startUniverseRoom(t, m, c)
	update := map[string]any{"expected_version": 1, "settings": map[string]any{"npc_count": 120, "ambient_color": "#112233"}, "agent": map[string]string{"id": "savinykh", "behavior": "guard"}}
	universeError(t, universeCmd(t, m, b, "universe.admin", update), "forbidden")
	if got := universeCmd(t, m, a, "universe.admin", update); got.Type != "universe.state" {
		t.Fatal(string(got.Payload))
	}
	u := m.rooms[code].Universe
	if u.Settings.NPCCount != 120 || u.Agents[0].Behavior != "guard" || u.Version != 2 {
		t.Fatal(u)
	}
	universeError(t, universeCmd(t, m, a, "universe.admin", update), "stale_version")
	for _, settings := range []map[string]any{{"npc_count": 161}, {"npc_count": -1}, {"ambient_color": "<script>"}, {"light_intensity": 0}, {"simulation_speed": 3}} {
		universeError(t, universeCmd(t, m, a, "universe.admin", map[string]any{"expected_version": 2, "settings": settings}), "invalid_payload")
	}
	for _, spawn := range []map[string]any{{"kind": "crate", "x": 200, "z": 0}, {"kind": "script", "x": 0, "z": 0}, {"kind": "plant", "x": 0, "y": -1, "z": 0}} {
		universeError(t, universeCmd(t, m, a, "universe.admin", map[string]any{"expected_version": 2, "spawn": spawn}), "invalid_payload")
	}
	if universeOf(m.rooms[m.RoomOf(c.user.ID)]).Settings.NPCCount != newUniverse().Settings.NPCCount {
		t.Fatal("settings leaked across rooms")
	}
	if m.rooms[code].Universe.Version != 2 {
		t.Fatal("invalid requests mutated version")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	restored, reopened := roomFixture(t, dir)
	defer reopened.Close()
	if restored.rooms[code].Universe.Settings.NPCCount != 120 || restored.rooms[code].Universe.Agents[0].Behavior != "guard" {
		t.Fatal("universe lost on restart")
	}
	a2 := attach(t, restored, "owner")
	roomCmd(t, restored, a2, "room.reset", map[string]any{})
	if restored.rooms[code].Universe.Settings.NPCCount != newUniverse().Settings.NPCCount || restored.rooms[code].Universe.Version != 1 {
		t.Fatal("reset did not restore defaults")
	}
}
func TestUniverseInteractionOwnershipDistanceAndDisconnect(t *testing.T) {
	m, store := roomFixture(t, t.TempDir())
	defer store.Close()
	a := attach(t, m, "alice")
	b := attach(t, m, "boris")
	startUniverseRoom(t, m, a)
	code := m.RoomOf("alice")
	roomCmd(t, m, b, "room.join", map[string]string{"code": code})
	// The fallback authoritative position is (-15,1.7,0), close to crate-cryo.
	p := func(version uint64, object, action string) map[string]any {
		return map[string]any{"expected_version": version, "object_id": object, "action": action}
	}
	universeError(t, universeCmd(t, m, a, "universe.interact", p(1, "door-bridge", "toggle")), "too_far")
	if got := universeCmd(t, m, a, "universe.interact", p(1, "crate-cryo", "pickup")); got.Type != "universe.state" {
		t.Fatal(string(got.Payload))
	}
	universeError(t, universeCmd(t, m, b, "universe.interact", p(2, "crate-cryo", "pickup")), "invalid_action")
	universeError(t, universeCmd(t, m, b, "universe.interact", p(2, "crate-cryo", "drop")), "forbidden")
	if got := universeCmd(t, m, a, "universe.interact", p(2, "crate-cryo", "drop")); got.Type != "universe.state" {
		t.Fatal(string(got.Payload))
	}
	o := m.rooms[code].Universe.Objects[1]
	if o.HeldBy != "" || o.X != -15 || o.Z != 1.2 {
		t.Fatal("drop did not use server position", o)
	}
	universeCmd(t, m, a, "universe.interact", p(3, "crate-cryo", "pickup"))
	m.leaveUniverse("alice")
	if m.rooms[code].Universe.Objects[1].HeldBy != "" {
		t.Fatal("disconnected player locked crate")
	}
	universeCmd(t, m, a, "universe.interact", p(5, "crate-cryo", "pickup"))
	roomCmd(t, m, a, "room.leave", map[string]any{})
	if m.rooms[code].Universe.Objects[1].HeldBy != "" {
		t.Fatal("room departure locked crate")
	}
}
func TestUniverseAgentCommandsBehaviorAndBounds(t *testing.T) {
	u := newUniverse()
	for _, input := range []string{"Как добавить растения?", "Не добавь растения", "Можешь открыть дверь?", "Привет"} {
		if universeIntent(input) != "" {
			t.Fatal("non-command changed world", input)
		}
	}
	if _, err := applyUniverseIntent(u, "savinykh", "Добавь растения"); err != nil || len(u.Objects) != len(newUniverse().Objects)+4 {
		t.Fatal(err, len(u.Objects))
	}
	for _, behavior := range []string{"passive", "guard", "patrol"} {
		u.Agents[0].Behavior = behavior
		if _, err := applyUniverseIntent(u, "savinykh", "Добавь растения"); err == nil {
			t.Fatal("behavior not enforced", behavior)
		}
	}
	u.Agents[0].Behavior = "guard"
	if _, err := applyUniverseIntent(u, "savinykh", "Открой двери"); err != nil || !u.Objects[0].Open {
		t.Fatal(err)
	}
	u.Agents[0].Behavior = "helpful"
	for len(u.Objects) < 79 {
		u.Objects = append(u.Objects, UniverseObject{Kind: "crate"})
	}
	before := len(u.Objects)
	if _, err := applyUniverseIntent(u, "savinykh", "Добавь ящики"); err == nil || len(u.Objects) != before {
		t.Fatal("object cap not atomic")
	}
	if finiteRange(math.Inf(1), 0, 100) || finiteRange(math.NaN(), 0, 100) {
		t.Fatal("nonfinite accepted")
	}
}
func TestUniverseChatCommandAndClosedDoorMovement(t *testing.T) {
	m, store := roomFixture(t, t.TempDir())
	defer store.Close()
	a := attach(t, m, "alice")
	startUniverseRoom(t, m, a)
	if !m.universeBlocksMovement("alice", -7, 0) {
		t.Fatal("walked through closed door")
	}
	e, _ := protocol.Message("agent.talk", "create", map[string]string{"agent_id": "savinykh", "text": "Добавь растения"})
	m.talk(context.Background(), a, e)
	if last := a.messages[len(a.messages)-1]; last.Type != "agent.reply" {
		t.Fatal(last.Type, string(last.Payload))
	}
	if len(m.rooms[m.RoomOf("alice")].Universe.Objects) != len(newUniverse().Objects)+4 {
		t.Fatal("chat did not modify environment")
	}
	m.talk(context.Background(), a, e)
	if len(m.rooms[m.RoomOf("alice")].Universe.Objects) != len(newUniverse().Objects)+4 {
		t.Fatal("duplicate chat command created more objects")
	}

	e, _ = protocol.Message("agent.talk", "open", map[string]string{"agent_id": "savinykh", "text": "Открой двери"})
	m.talk(context.Background(), a, e)
	for i := range m.rooms[m.RoomOf("alice")].Universe.Objects {
		m.rooms[m.RoomOf("alice")].Universe.Objects[i].MotionAt -= int64(deck.DoorDuration)
	}
	if m.universeBlocksMovement("alice", -7, 0) {
		t.Fatal("open door still blocks movement")
	}
}
func TestUniverseTelemetryMembershipFreshnessAndValidation(t *testing.T) {
	m, store := roomFixture(t, t.TempDir())
	defer store.Close()
	a := attach(t, m, "alice")
	b := attach(t, m, "boris")
	c := attach(t, m, "other")
	startUniverseRoom(t, m, a)
	roomCmd(t, m, b, "room.join", map[string]string{"code": m.RoomOf("alice")})
	startUniverseRoom(t, m, c)
	sample := map[string]any{"fps": 60, "ping_ms": 24, "frame_ms": 16.67, "draw_calls": 32, "triangles": 6000, "renderer": "WebGL", "hidden": false}
	e, _ := protocol.Message("telemetry.report", "sample", sample)
	m.telemetryReport(context.Background(), b, e)
	if m.telemetry["boris"].FPS != 60 || m.telemetry["boris"].UpdatedAt == 0 {
		t.Fatal("sample missing")
	}
	e, _ = protocol.Message("telemetry.get", "metrics", map[string]any{})
	m.telemetryGet(context.Background(), b, e)
	universeError(t, b.messages[len(b.messages)-1], "forbidden")
	m.telemetryGet(context.Background(), a, e)
	var result struct {
		Clients []map[string]any `json:"clients"`
	}
	json.Unmarshal(a.messages[len(a.messages)-1].Payload, &result)
	if len(result.Clients) != 2 {
		t.Fatal("metrics leaked room", result)
	}
	if result.Clients[0]["fps"] != nil || result.Clients[0]["stale"] != true || result.Clients[1]["fps"] != float64(60) || result.Clients[1]["stale"] != false {
		t.Fatal("fresh/missing sample labels wrong", result)
	}
	old := m.telemetry["boris"]
	old.UpdatedAt = time.Now().Add(-11 * time.Second).UnixMilli()
	m.telemetry["boris"] = old
	m.telemetryGet(context.Background(), a, e)
	json.Unmarshal(a.messages[len(a.messages)-1].Payload, &result)
	if result.Clients[1]["stale"] != true {
		t.Fatal("old metrics marked fresh")
	}
	invalid, _ := protocol.Message("telemetry.report", "bad", map[string]any{"fps": -1})
	m.telemetryReport(context.Background(), a, invalid)
	universeError(t, a.messages[len(a.messages)-1], "invalid_payload")
	injected := protocol.Envelope{Type: "telemetry.report", RequestID: "spoof", Payload: json.RawMessage(`{"fps":50,"player_id":"alice"}`)}
	m.telemetryReport(context.Background(), b, injected)
	universeError(t, b.messages[len(b.messages)-1], "invalid_payload")
	m.leaveUniverse("boris")
	if _, exists := m.telemetry["boris"]; exists {
		t.Fatal("disconnect retained sample")
	}
	// Noninitialized identity cannot publish a sample for anybody.
	d := &fakeSession{user: model.User{ID: "unknown"}}
	m.telemetryReport(context.Background(), d, invalid)
	universeError(t, d.messages[len(d.messages)-1], "not_initialized")
}

func TestUniverseSnapshotScopeIsDetachedFromRoomAndPersistence(t *testing.T) {
	r := &Room{Code: "FIRST", Generation: 1, Universe: newUniverse()}
	old := snapshotUniverse(r)
	r.Code = "SECOND"
	r.Generation = 2
	r.Universe = newUniverse()
	next := snapshotUniverse(r)
	if old.RoomCode != "FIRST" || old.Generation != 1 || next.RoomCode != "SECOND" || next.Generation != 2 {
		t.Fatal("snapshot scope followed mutable room")
	}
	next.Objects[0].Open = true
	if old.Objects[0].Open || r.Universe.Objects[0].Open {
		t.Fatal("snapshot aliases live state")
	}
	raw, err := json.Marshal(r.Universe)
	if err != nil {
		t.Fatal(err)
	}
	var stored map[string]any
	json.Unmarshal(raw, &stored)
	if _, ok := stored["room_code"]; ok {
		t.Fatal("wire scope persisted into universe")
	}
	if _, ok := stored["generation"]; ok {
		t.Fatal("wire generation persisted into universe")
	}
}
