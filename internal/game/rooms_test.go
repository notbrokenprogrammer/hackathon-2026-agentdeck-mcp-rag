package game

import (
	"context"
	"encoding/json"
	"feeders/internal/model"
	"feeders/internal/storage"
	"feeders/pkg/protocol"
	"fmt"
	"io"
	"log/slog"
	"testing"
)

func roomFixture(t *testing.T, dir string) (*Module, *storage.SQLiteStore) {
	t.Helper()
	store, e := storage.NewSQLiteStore(dir)
	if e != nil {
		t.Fatal(e)
	}
	m := New(store, nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if e = m.WithPersistence(store); e != nil {
		t.Fatal(e)
	}
	return m, store
}
func attach(t *testing.T, m *Module, id string) *fakeSession {
	t.Helper()
	s := &fakeSession{user: model.User{ID: id, Username: id}}
	e, _ := protocol.Message("player.init", "init", map[string]any{})
	m.init(context.Background(), s, e)
	return s
}
func roomCmd(t *testing.T, m *Module, s *fakeSession, kind string, p any) {
	t.Helper()
	e, _ := protocol.Message(kind, "room-command", p)
	m.roomCommand(context.Background(), s, e)
	if last := s.messages[len(s.messages)-1]; last.Type == "error" {
		t.Fatalf("%s: %s", kind, last.Payload)
	}
}
func perform(t *testing.T, m *Module, s *fakeSession, action, id string, version uint64) protocol.Envelope {
	t.Helper()
	e, _ := protocol.Message("game.action", id, protocol.GameActionRequest{Action: action, ExpectedVersion: version})
	m.action(context.Background(), s, e)
	return s.messages[len(s.messages)-1]
}
func TestRoomIsolationRolesIdempotencyAndRestart(t *testing.T) {
	dir := t.TempDir()
	m, store := roomFixture(t, dir)
	a := attach(t, m, "alice")
	b := attach(t, m, "boris")
	c := attach(t, m, "claire")
	roomCmd(t, m, a, "room.create", map[string]string{"name": "A"})
	code := m.RoomOf("alice")
	roomCmd(t, m, b, "room.join", map[string]string{"code": code})
	roomCmd(t, m, c, "room.create", map[string]string{"name": "B"})
	roomCmd(t, m, a, "room.role", map[string]string{"role": "captain"})
	roomCmd(t, m, b, "room.role", map[string]string{"role": "engineer"})
	for _, s := range []*fakeSession{a, b} {
		roomCmd(t, m, s, "room.ready", map[string]bool{"ready": true})
	}
	roomCmd(t, m, a, "room.start", map[string]any{})
	preview, _ := protocol.Message("game.preview", "forecast", protocol.GameActionRequest{Action: "repair_cooling", ExpectedVersion: 1})
	m.previewAction(context.Background(), a, preview)
	var forecast struct {
		Allowed bool      `json:"allowed"`
		After   GameState `json:"after"`
	}
	json.Unmarshal(a.messages[len(a.messages)-1].Payload, &forecast)
	if !forecast.Allowed || forecast.After.Energy != 25 || m.engineFor("alice").GetState().Version != 1 {
		t.Fatal("forecast changed live state or missed system drain", forecast)
	}
	if got := perform(t, m, b, "repair_cooling", "repair-once", 1); got.Type != "game.state" {
		t.Fatal(string(got.Payload))
	}
	after := m.engineFor("alice").GetState()
	read := perform(t, m, a, "get_state", "room-snapshot", 0)
	var actual GameState
	json.Unmarshal(read.Payload, &actual)
	if actual.Version != after.Version || actual.Energy != after.Energy {
		t.Fatal("get_state returned another room", actual)
	}
	if after.Energy != 25 || !after.CoolingRepaired {
		t.Fatal(after)
	}
	perform(t, m, b, "repair_cooling", "repair-once", 1)
	if m.engineFor("alice").GetState().Version != after.Version {
		t.Fatal("duplicate mutated state")
	}
	if got := perform(t, m, a, "use_battery", "stale", 1); got.Type != "error" {
		t.Fatal("stale command accepted")
	}
	if got := perform(t, m, b, "start_greenhouse", "forbidden", 2); got.Type != "error" {
		t.Fatal("role bypass")
	}
	if other := m.engineFor("claire").GetState(); other.Version != 1 || other.CoolingRepaired {
		t.Fatal("room leakage", other)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	again, reopened := roomFixture(t, dir)
	defer reopened.Close()
	a2 := attach(t, again, "alice")
	if again.RoomOf(a2.user.ID) != code {
		t.Fatal("membership not restored")
	}
	restored := again.engineFor("alice").GetState()
	if restored.Version != 2 || restored.Energy != 25 {
		t.Fatal(restored)
	}
	b2 := attach(t, again, "boris")
	perform(t, again, b2, "repair_cooling", "repair-once", 1)
	if again.engineFor("alice").GetState().Version != 2 {
		t.Fatal("receipt lost at restart")
	}
}
func TestFinalRequiresConsensusAndRunsOnce(t *testing.T) {
	m, store := roomFixture(t, t.TempDir())
	defer store.Close()
	a := attach(t, m, "alice")
	b := attach(t, m, "boris")
	roomCmd(t, m, a, "room.create", map[string]any{})
	roomCmd(t, m, b, "room.join", map[string]string{"code": m.RoomOf("alice")})
	roomCmd(t, m, a, "room.role", map[string]string{"role": "captain"})
	roomCmd(t, m, b, "room.role", map[string]string{"role": "engineer"})
	for _, s := range []*fakeSession{a, b} {
		roomCmd(t, m, s, "room.ready", map[string]bool{"ready": true})
	}
	roomCmd(t, m, a, "room.start", map[string]any{})
	perform(t, m, b, "repair_cooling", "r", 1)
	perform(t, m, b, "start_reactor", "s", 2)
	if got := perform(t, m, a, "final_combined", "final", 3); got.Type != "decision.pending" {
		t.Fatal(got.Type, string(got.Payload))
	}
	if m.engineFor("alice").GetState().GameOver {
		t.Fatal("final executed before consensus")
	}
	e, _ := protocol.Message("decision.vote", "vote", map[string]any{"approve": true, "expected_version": 3})
	m.vote(context.Background(), b, e)
	state := m.engineFor("alice").GetState()
	if !state.Victory || state.Version != 4 {
		t.Fatal(state)
	}
	m.vote(context.Background(), b, e)
	if m.engineFor("alice").GetState().Version != 4 {
		t.Fatal("duplicate vote executed final twice")
	}
}
func TestFullMissionAndFailedFinal(t *testing.T) {
	m, store := roomFixture(t, t.TempDir())
	defer store.Close()
	a := attach(t, m, "alice")
	roomCmd(t, m, a, "room.create", map[string]any{})
	roomCmd(t, m, a, "room.role", map[string]string{"role": "commander"})
	roomCmd(t, m, a, "room.ready", map[string]bool{"ready": true})
	roomCmd(t, m, a, "room.start", map[string]any{})
	for i, action := range []string{"repair_cooling", "start_reactor", "use_battery", "start_greenhouse", "restore_archive", "calculate_course"} {
		if got := perform(t, m, a, action, fmt.Sprintf("%d_%s", i, action), uint64(i+1)); got.Type != "game.state" {
			t.Fatal(action, string(got.Payload))
		}
	}
	e, _ := protocol.Message("investigation.solve", "solve", protocol.InvestigationSolveRequest{Clues: []string{"damaged_filter", "impact_mark", "protective_chronology"}, ExpectedVersion: 7})
	m.solve(context.Background(), a, e)
	if !m.engineFor("alice").GetState().InvestigationSolved {
		t.Fatal("investigation failed")
	}
	perform(t, m, a, "final_ecosystem", "rescue", m.engineFor("alice").GetState().Version)
	if !m.engineFor("alice").GetState().Victory {
		t.Fatal("full path cannot win")
	}
	roomCmd(t, m, a, "room.reset", map[string]any{})
	roomCmd(t, m, a, "room.ready", map[string]bool{"ready": true})
	roomCmd(t, m, a, "room.start", map[string]any{})
	for i, action := range []string{"use_battery", "start_reactor", "wait_cycle", "wait_cycle", "final_emergency"} {
		perform(t, m, a, action, fmt.Sprintf("%d_%s", i, action), uint64(i+1))
	}
	if state := m.engineFor("alice").GetState(); !state.GameOver || state.Victory {
		b, _ := json.Marshal(state)
		t.Fatal("failure ending not reached", string(b))
	}
}
