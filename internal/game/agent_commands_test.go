package game

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"

	"feeders/internal/agents"
	"feeders/internal/knowledge"
	"feeders/internal/mission"
	"feeders/internal/npc"
	"feeders/internal/rag"
	"feeders/internal/storage"
	"feeders/pkg/protocol"
)

func commandFixture(t *testing.T) (*Module, *fakeSession, *storage.SQLiteStore, *agents.CrewService) {
	t.Helper()
	m, store := roomFixture(t, t.TempDir())
	p := attach(t, m, "alice")
	startUniverseRoom(t, m, p)
	corpus, err := rag.Load("../../knowledge")
	if err != nil {
		t.Fatal(err)
	}
	local, err := npc.NewLocal(knowledge.Catalog{Store: corpus, ScenarioOverride: CallCrewTool}, "http://127.0.0.1:11434", "test")
	if err != nil {
		t.Fatal(err)
	}
	service := agents.NewCrewService(local.WithMission(m).WithoutModel(), nil)
	m.WithAgents(service)
	return m, p, store, service
}
func commandContext(m *Module, p *fakeSession, id string) context.Context {
	e, _ := protocol.Message("agent.talk", id, map[string]any{})
	return m.agentCommandContext(context.Background(), p, e, m.memoryScope(p.User().ID), "savinykh")
}
func issue(t *testing.T, m *Module, p *fakeSession, id, tool string, args any) mission.CommandResult {
	t.Helper()
	raw, _ := json.Marshal(args)
	result, err := mission.ExecuteCommand(commandContext(m, p, id), tool, raw)
	if err != nil {
		t.Fatal(err)
	}
	return result
}
func TestMissionCommandsCompleteRealQuestRoute(t *testing.T) {
	m, p, store, service := commandFixture(t)
	defer store.Close()
	code := m.RoomOf("alice")
	states := []GameState{m.engineFor("alice").GetState()}
	commands := []string{"Восстанови охлаждение", "Запусти реактор", "Запусти оранжерею", "Восстанови архив"}
	for i, text := range commands {
		reply, err := service.Talk(commandContext(m, p, fmt.Sprint(i)), agents.Request{PlayerID: "alice", AgentID: "savinykh", DialogueMode: "mission", Text: text})
		if err != nil || reply.Command == nil || reply.Command.Status != "completed" {
			t.Fatalf("%s: %+v %v", text, reply, err)
		}
		states = append(states, m.engineFor("alice").GetState())
	}
	result := issue(t, m, p, "solve", "solve_investigation", map[string]any{"clues": []string{"damaged_filter", "impact_mark", "protective_chronology"}})
	if result.Status != "completed" {
		t.Fatal(result)
	}
	states = append(states, m.engineFor("alice").GetState())
	for i, text := range []string{"Рассчитай курс", "Подключи резервную батарею", "Выполни длительный безопасный манёвр"} {
		reply, err := service.Talk(commandContext(m, p, fmt.Sprint("end", i)), agents.Request{PlayerID: "alice", AgentID: "savinykh", DialogueMode: "mission", Text: text})
		if err != nil || reply.Command.Status != "completed" {
			t.Fatal(reply, err)
		}
		states = append(states, m.engineFor("alice").GetState())
	}
	final := states[len(states)-1]
	if !final.Victory || !final.GameOver || !final.InvestigationSolved || !final.CourseCalculated {
		t.Fatal(final)
	}
	// Verify actual server transitions against the production UI quest projection.
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatal("Node required for server/UI quest contract", err)
	}
	data, _ := json.Marshal(states)
	cmd := exec.Command(node, "../../scripts/test-mission-quests.mjs", "--stdin")
	cmd.Stdin = bytes.NewReader(data)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("UI progress: %s %v", out, err)
	}
	if err := m.persistRoomsLocked(); err != nil {
		t.Fatal(err)
	}
	restored := New(store, nil, nil, m.log)
	if err := restored.WithPersistence(store); err != nil {
		t.Fatal(err)
	}
	if got := restored.rooms[code].engine.GetState(); !reflect.DeepEqual(final, got) {
		t.Fatal("quest completion did not persist", got)
	}
}

func TestMissionCommandAuthorizationStaleScopeAndNoDoubleCost(t *testing.T) {
	m, p, store, _ := commandFixture(t)
	defer store.Close()
	before := m.engineFor("alice").GetState()
	ctx := commandContext(m, p, "same-request")
	raw := json.RawMessage(`{"action":"repair_cooling"}`)
	if _, err := mission.ExecuteCommand(ctx, "execute_action", raw); err != nil {
		t.Fatal(err)
	}
	after := m.engineFor("alice").GetState()
	if _, err := mission.ExecuteCommand(ctx, "execute_action", raw); err == nil {
		t.Fatal("second mutation in same message accepted")
	}
	// A redelivered request has the same receipt but a fresh snapshot: conflict, no spend.
	if _, err := mission.ExecuteCommand(commandContext(m, p, "same-request"), "execute_action", raw); err == nil {
		t.Fatal("conflicting replay accepted")
	}
	if !reflect.DeepEqual(after, m.engineFor("alice").GetState()) || after.Version != before.Version+1 {
		t.Fatal("double spend")
	}
	stale := commandContext(m, p, "stale")
	issue(t, m, p, "battery", "execute_action", map[string]string{"action": "use_battery"})
	if _, err := mission.ExecuteCommand(stale, "execute_action", json.RawMessage(`{"action":"start_reactor"}`)); err == nil {
		t.Fatal("stale plan executed")
	}
	r := m.rooms[m.RoomOf("alice")]
	r.Members["alice"].Role = "biologist"
	if _, err := mission.ExecuteCommand(commandContext(m, p, "forbidden"), "execute_action", json.RawMessage(`{"action":"start_reactor"}`)); err == nil {
		t.Fatal("NPC bypassed player role")
	}
	oldRoom := commandContext(m, p, "old-room")
	roomCmd(t, m, p, "room.create", map[string]string{"name": "Different expedition"})
	if _, err := mission.ExecuteCommand(oldRoom, "execute_action", raw); err == nil {
		t.Fatal("old command crossed room boundary")
	}
	if got := m.engineFor("alice").GetState(); got.CoolingRepaired {
		t.Fatal("new room changed")
	}
}

func TestMissionCommandCannotSurviveReset(t *testing.T) {
	m, p, store, _ := commandFixture(t)
	defer store.Close()
	old := commandContext(m, p, "before-reset")
	roomCmd(t, m, p, "room.reset", map[string]any{})
	_, err := mission.ExecuteCommand(old, "execute_action", json.RawMessage(`{"action":"repair_cooling"}`))
	if err == nil || m.engineFor("alice").GetState().CoolingRepaired {
		t.Fatal("stale generation executed", err)
	}
}

func TestMissionCommandsPreviewVotingWorldAndExperiment(t *testing.T) {
	m, p, store, _ := commandFixture(t)
	defer store.Close()
	before := m.engineFor("alice").GetState()
	result := issue(t, m, p, "preview", "preview_action", map[string]string{"action": "repair_cooling"})
	if result.Status != "preview" || !reflect.DeepEqual(before, m.engineFor("alice").GetState()) {
		t.Fatal("preview mutated mission")
	}
	issue(t, m, p, "clock", "start_experiment", map[string]any{})
	r := m.rooms[m.RoomOf("alice")]
	if r.Experiment == nil {
		t.Fatal("experiment absent")
	}
	issue(t, m, p, "doors", "world_command", map[string]string{"text": "Открой двери"})
	opened := 0
	for _, o := range r.Universe.Objects {
		if o.Kind == "door" && o.Open {
			opened++
		}
	}
	if opened == 0 {
		t.Fatal("doors did not open")
	}
	other := attach(t, m, "boris")
	roomCmd(t, m, other, "room.join", map[string]string{"code": r.Code})
	roomCmd(t, m, other, "room.role", map[string]string{"role": "navigator"})
	result = issue(t, m, p, "evacuate", "execute_action", map[string]string{"action": "final_evacuation"})
	if result.Status != "pending" || r.Proposal == nil || r.engine.GetState().GameOver {
		t.Fatal("final executed without second player", result)
	}
	vote, _ := protocol.Message("decision.vote", "approve", map[string]any{"approve": true, "expected_version": r.engine.GetState().Version})
	m.stateStore = failingCommandStore{store}
	m.vote(context.Background(), other, vote)
	if r.engine.GetState().GameOver || r.Proposal == nil || r.Proposal.Approved["boris"] {
		t.Fatal("failed save completed the final")
	}
	m.stateStore = store
	m.vote(context.Background(), other, vote)
	if !r.engine.GetState().Victory {
		t.Fatal("approved final not executed")
	}
}

type failingCommandStore struct{ storage.StateStore }

func (s failingCommandStore) SaveState(string, any) error {
	return errors.New("simulated disk failure")
}
func TestMissionCommandSaveFailureRollsBack(t *testing.T) {
	m, p, store, _ := commandFixture(t)
	defer store.Close()
	before := m.engineFor("alice").GetState()
	m.stateStore = failingCommandStore{store}
	_, err := mission.ExecuteCommand(commandContext(m, p, "no-save"), "execute_action", json.RawMessage(`{"action":"repair_cooling"}`))
	if err == nil || !reflect.DeepEqual(before, m.engineFor("alice").GetState()) {
		t.Fatal("unsaved operation reported/applied", err)
	}
	m.stateStore = store
	issue(t, m, p, "no-save", "execute_action", map[string]string{"action": "repair_cooling"})
}

func TestMissionDialogueHandlerRunsAndPersistsCommand(t *testing.T) {
	m, p, store, _ := commandFixture(t)
	defer store.Close()
	p.delivered = make(chan struct{}, 1)
	e, _ := protocol.Message("agent.talk", "mission-dialogue", map[string]string{"agent_id": "dzhanibekov", "dialogue_mode": "mission", "text": "Восстанови охлаждение"})
	m.talk(context.Background(), p, e)
	select {
	case <-p.delivered:
	case <-time.After(2 * time.Second):
		t.Fatal("reply missing")
	}
	last := p.messages[len(p.messages)-1]
	var reply agents.Reply
	if last.Type != "agent.reply" || json.Unmarshal(last.Payload, &reply) != nil || reply.Command == nil || reply.Command.Status != "completed" {
		t.Fatal(string(last.Payload))
	}
	m.roomMu.Lock()
	rows := m.rooms[m.membership["alice"]].Dialogues[dialogueKey("alice", "dzhanibekov", "mission")]
	m.roomMu.Unlock()
	if len(rows) != 2 || !strings.Contains(rows[1].Text, "Выполнено") {
		t.Fatal("command receipt missing from history", rows)
	}
}
