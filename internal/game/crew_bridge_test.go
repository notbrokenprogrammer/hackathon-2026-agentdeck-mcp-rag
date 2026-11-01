package game

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"path/filepath"
	"reflect"
	"testing"

	"feeders/internal/agents"
	"feeders/internal/knowledge"
	"feeders/internal/mission"
	"feeders/internal/model"
	"feeders/internal/npc"
	"feeders/internal/rag"
)

func crewFixture(t *testing.T) (*Module, knowledge.Catalog) {
	t.Helper()
	m := New(nil, nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	m.ready["p"] = model.PlayerState{PlayerID: "p"}
	store, err := rag.Load("../../knowledge")
	if err != nil {
		t.Fatal(err)
	}
	c := knowledge.Catalog{Store: store, ScenarioOverride: CallCrewTool, ViewProvider: func(context.Context) (mission.View, error) { return m.View("p") }}
	return m, c
}

func TestCrewForecastUsesCurrentRulesWithoutMutation(t *testing.T) {
	m, c := crewFixture(t)
	for stage := 0; stage < 4; stage++ {
		before := m.scenario.GetState()
		view, _ := m.View("p")
		for _, name := range []string{"start_reactor", "repair_cooling", "start_garden", "use_battery", "restore_archive", "prepare_course", "wait", "emergency_burn", "ecosystem_plan", "combined_plan", "evacuate"} {
			raw, err := c.Call(context.Background(), "savinykh_preview_action", json.RawMessage(rag.JSON(map[string]string{"name": name})))
			if err != nil {
				t.Fatal(err)
			}
			got := raw.(map[string]any)
			// Execute the current engine on an isolated copy, as a real game command does.
			aliases := map[string]string{"start_garden": "start_greenhouse", "prepare_course": "calculate_course", "wait": "wait_cycle", "emergency_burn": "final_emergency", "ecosystem_plan": "final_ecosystem", "combined_plan": "final_combined", "evacuate": "final_evacuation"}
			action := name
			if aliases[name] != "" {
				action = aliases[name]
			}
			isolated := &ScenarioEngine{state: before}
			actual, actionErr := isolated.ExecuteAction(action)
			if got["allowed"] != (actionErr == nil) {
				t.Fatalf("%s: wrong availability", name)
			}
			if actionErr == nil {
				forecast := got["after"].(GameState)
				if forecast.Energy != actual.Energy || forecast.Oxygen != actual.Oxygen || forecast.Hull != actual.Hull || forecast.Cycle != actual.Cycle || forecast.Victory != actual.Victory || forecast.GameOver != actual.GameOver {
					t.Fatalf("%s: forecast differs: %+v / %+v", name, forecast, actual)
				}
			}
		}
		if !reflect.DeepEqual(before, m.scenario.GetState()) {
			t.Fatal("a read tool mutated the game")
		}
		if _, err := c.Call(mission.WithView(context.Background(), view), "savinykh_preview_action", json.RawMessage(`{"name":"wait","choice":"quest"}`)); err == nil {
			t.Fatal("ignored optional quest arguments")
		}
		for _, action := range []string{"repair_cooling", "start_reactor", "start_greenhouse", "calculate_course"}[stage : stage+1] {
			if _, err := m.scenario.ExecuteAction(action); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestMergedCrewScopeAndLiveProjection(t *testing.T) {
	m, c := crewFixture(t)
	ctx := context.Background()
	for _, item := range []struct{ agent, id string }{{"savinykh", "scenario-confirmed-cause"}, {"dzhanibekov", "scenario-filter"}, {"dzhanibekov", "scenario-drone-route"}} {
		if _, err := c.Call(ctx, item.agent+"_read_chunk", json.RawMessage(rag.JSON(map[string]string{"chunk_id": item.id}))); err == nil {
			t.Fatalf("hidden chunk %s was exposed", item.id)
		}
	}
	if _, err := c.Call(ctx, "krikalev_rescue_plans", nil); err == nil {
		t.Fatal("accepted a tool outside the catalog")
	}
	path := filepath.Join(t.TempDir(), "crew-view.json")
	m.WithCrewProjection(path)
	initial, err := ReadCrewView(path, "p")
	if err != nil {
		t.Fatal(err)
	}
	if initial.State.E != 35 || mission.Captain != "savinykh" {
		t.Fatal(initial)
	}
	m.scenario.ExecuteAction("repair_cooling")
	m.scenario.ExecuteAction("restore_archive")
	m.exportCrewView()
	current, err := ReadCrewView(path, "p")
	if err != nil {
		t.Fatal(err)
	}
	if current.State.Version <= initial.State.Version || !current.HasFact("impact_record") {
		t.Fatal(current)
	}
	if _, err := c.Call(ctx, "dzhanibekov_read_chunk", json.RawMessage(`{"chunk_id":"scenario-filter"}`)); err != nil {
		t.Fatal(err)
	}
	// Every persona responds through the merged service, in offline fallback mode.
	mentor, err := agents.NewMentor(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	local, err := npc.NewLocal(c, "http://127.0.0.1:11434", "test")
	if err != nil {
		t.Fatal(err)
	}
	service := agents.NewCrewService(local.WithMission(m).WithoutModel(), mentor)
	for _, id := range []string{"savinykh", "dzhanibekov", "ryazansky", "krikalev"} {
		reply, err := service.Talk(ctx, agents.Request{PlayerID: "p", AgentID: id, Text: "Какие системы и планы доступны?"})
		if err != nil || reply.Text == "" || reply.Analysis == nil {
			t.Fatalf("%s: %+v, %v", id, reply, err)
		}
	}
	historical, err := service.Talk(ctx, agents.Request{PlayerID: "p", AgentID: "savinykh", Text: "Как восстанавливали питание Салюта-7?"})
	if err != nil || historical.Citation == nil || historical.Analysis != nil {
		t.Fatalf("historical route: %+v %v", historical, err)
	}
	game, err := service.Talk(ctx, agents.Request{PlayerID: "p", AgentID: "savinykh", Text: "Что делать дальше после моей операции?"})
	if err != nil || game.Analysis == nil {
		t.Fatal("game follow-up was routed to history", err)
	}
	simpler, err := service.Talk(ctx, agents.Request{PlayerID: "p", AgentID: "savinykh", Text: "Объясни проще"})
	if err != nil || simpler.Analysis == nil {
		t.Fatal("follow-up lost game context", err)
	}
	delete(m.ready, "p")
	m.exportCrewView()
	if _, err := ReadCrewView(path, "p"); err == nil {
		t.Fatal("disconnected player retained live access")
	}
}
