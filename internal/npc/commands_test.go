package npc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"feeders/internal/knowledge"
	"feeders/internal/mission"
	"feeders/internal/rag"
)

func TestMissionModelUsesBoundToolAndServerReceipt(t *testing.T) {
	corpus, err := rag.Load("../../knowledge")
	if err != nil {
		t.Fatal(err)
	}
	local, err := NewLocal(knowledge.Catalog{Store: corpus}, "http://127.0.0.1:11434", "test")
	if err != nil {
		t.Fatal(err)
	}
	var mutated bool
	ctx := mission.WithCommands(context.Background(), "ROOM:1", func(_ context.Context, name string, args json.RawMessage) (mission.CommandResult, error) {
		if name == "mission_status" {
			return mission.CommandResult{Status: "status", Text: "Данные сервера", State: map[string]any{"version": 4}}, nil
		}
		if name != "execute_action" {
			t.Fatal(name)
		}
		var v map[string]string
		_ = json.Unmarshal(args, &v)
		if v["action"] != "repair_cooling" {
			t.Fatal(v)
		}
		mutated = true
		return mission.CommandResult{Status: "completed", Text: "Подтверждён ремонт", Action: v["action"]}, nil
	})
	local.client.Transport = newModelHTTP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Tools []struct {
				Function struct {
					Name string `json:"name"`
				} `json:"function"`
			} `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		found := false
		for _, tool := range request.Tools {
			if tool.Function.Name == "dzhanibekov_execute_action" {
				found = true
			}
		}
		if !found {
			t.Error("mutation tool was not exposed")
		}
		fmt.Fprint(w, `{"message":{"role":"assistant","content":"Я уже всё починил!","tool_calls":[{"function":{"name":"dzhanibekov_execute_action","arguments":{"action":"repair_cooling"}}}]}}`)
	}))
	reply, err := local.Talk(ctx, Request{PlayerID: "p", AgentID: "dzhanibekov", DialogueMode: "mission", Text: "Почини систему охлаждения корабля"})
	if err != nil || !mutated || reply.Text != "Подтверждён ремонт" || reply.Command.Status != "completed" {
		t.Fatal(reply, err)
	}
}

func TestMissionQuestionsNegationAndModelClaimsNeverExecute(t *testing.T) {
	corpus, _ := rag.Load("../../knowledge")
	local, _ := NewLocal(knowledge.Catalog{Store: corpus}, "http://127.0.0.1:11434", "test")
	calls := 0
	ctx := mission.WithCommands(context.Background(), "ROOM:1", func(_ context.Context, name string, _ json.RawMessage) (mission.CommandResult, error) {
		calls++
		if name != "mission_status" {
			t.Fatal("question caused a mutation", name)
		}
		return mission.CommandResult{Status: "status", Text: "Состояние"}, nil
	})
	local.client.Transport = newModelHTTP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request map[string]any
		_ = json.NewDecoder(r.Body).Decode(&request)
		data, _ := json.Marshal(request["tools"])
		if strings.Contains(string(data), "execute_action") {
			t.Error("mutation tool exposed for a question")
		}
		fmt.Fprint(w, `{"message":{"role":"assistant","content":"Готово, реактор включён!","tool_calls":[{"function":{"name":"savinykh_execute_action","arguments":{"action":"start_reactor"}}}]}}`)
	}))
	for _, question := range []string{"Как запустить реактор?", "Не запускай реактор", "Что будет если запустить реактор?"} {
		reply, err := local.Talk(ctx, Request{PlayerID: "p", AgentID: "savinykh", DialogueMode: "mission", Text: question})
		if err != nil || reply.Command.Status != "status" || strings.Contains(reply.Text, "реактор включён") {
			t.Fatal(reply, err)
		}
	}
	if calls != 3 {
		t.Fatal(calls)
	}
}

func TestMissionDirectCommandsOfflineAndFailures(t *testing.T) {
	corpus, _ := rag.Load("../../knowledge")
	local, _ := NewLocal(knowledge.Catalog{Store: corpus}, "http://127.0.0.1:11434", "test")
	local.WithoutModel()
	mutations := 0
	ctx := mission.WithCommands(context.Background(), "ROOM:1", func(_ context.Context, name string, _ json.RawMessage) (mission.CommandResult, error) {
		if name == "mission_status" {
			return mission.CommandResult{Status: "status"}, nil
		}
		mutations++
		return mission.CommandResult{}, errors.New("недостаточно энергии")
	})
	reply, err := local.Talk(ctx, Request{PlayerID: "p", AgentID: "savinykh", DialogueMode: "mission", Text: "Запусти реактор"})
	if err != nil || mutations != 1 || reply.Command.Status != "failed" || !strings.Contains(reply.Text, "не выполнено") {
		t.Fatal(reply, err)
	}
	reply, err = local.Talk(context.Background(), Request{PlayerID: "p", AgentID: "savinykh", DialogueMode: "mission", Text: "Запусти реактор"})
	if err != nil || reply.Command.Status != "failed" || mutations != 1 {
		t.Fatal("unbound actor executed", reply, err)
	}
}

func TestMissionEvidenceMustBeNamedByPlayer(t *testing.T) {
	args := json.RawMessage(`{"clues":["damaged_filter","impact_mark","protective_chronology"]}`)
	if namedEvidence("Сопоставь три правильные улики", args) {
		t.Fatal("model chose the solution instead of the player")
	}
	if !namedEvidence("Сопоставь повреждённый фильтр, отметку удара и хронологию", args) {
		t.Fatal("explicit evidence not accepted")
	}
}
