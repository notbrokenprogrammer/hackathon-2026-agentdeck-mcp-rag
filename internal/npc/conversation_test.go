package npc

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"feeders/internal/knowledge"
	"feeders/internal/mission"
	"feeders/internal/rag"
)

type testView struct {
	value mission.View
	err   error
}

func (v *testView) View(string) (mission.View, error) { return v.value, v.err }

func responseJSON(value any, status int) *http.Response {
	encoded, _ := json.Marshal(value)
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(encoded)))}
}

func modelSays(content string) *http.Response {
	return responseJSON(map[string]any{"message": message{Role: "assistant", Content: content}}, 200)
}

func TestConversationCallsModelForEveryPersonaWithoutRAGMatches(t *testing.T) {
	for _, id := range []string{"savinykh", "dzhanibekov", "ryazansky", "krikalev"} {
		t.Run(id, func(t *testing.T) {
			local, _ := NewLocal(catalog(t, true), "http://127.0.0.1:11434", "our-local-model")
			view := &testView{value: mission.View{State: mission.State{Version: 1}, Roles: []string{mission.Captain}}}
			local.WithMission(view)
			calls := 0
			local.client.Transport = testModelTransport(func(req *http.Request) (*http.Response, error) {
				calls++
				var payload struct {
					Model    string
					Messages []message
					Tools    []struct{ Function struct{ Name string } }
				}
				if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
					t.Fatal(err)
				}
				if payload.Model != "our-local-model" || req.URL.Path != "/api/chat" {
					t.Fatal("model was bypassed")
				}
				for _, tool := range payload.Tools {
					if !strings.HasPrefix(tool.Function.Name, id+"_") {
						t.Fatal("cross-persona tool")
					}
				}
				if calls == 1 {
					return modelSays("Привет! Я на связи. Что обсудим?"), nil
				}
				return modelSays("Могу объяснить эту тему в общих чертах. В локальных документах подтверждения нет."), nil
			})
			for _, question := range []string{"Привет!", "Что такое квантовая запутанность?"} {
				reply, err := local.Talk(context.Background(), Request{Scope: "room:1", PlayerID: "player", AgentID: id, Text: question})
				if err != nil || reply.Mode != "model" || reply.Model != "our-local-model" || reply.FallbackReason != "" {
					t.Fatalf("%+v %v", reply, err)
				}
				if len(reply.Evidence) != 0 {
					t.Fatal("fabricated evidence")
				}
				if strings.Contains(reply.Text, "Цикл") {
					t.Fatal("resource report replaced natural conversation")
				}
				if question == "Привет!" && reply.Grounding != "conversation" {
					t.Fatal(reply.Grounding)
				}
				if question != "Привет!" && reply.Grounding != "model_knowledge" {
					t.Fatal(reply.Grounding)
				}
			}
			if calls != 2 {
				t.Fatal("both conversational turns must reach inference", calls)
			}
		})
	}
}

func TestConversationAllowsGroundedMissionParaphrase(t *testing.T) {
	store, _ := rag.Read(strings.NewReader(`{"id":"cooling-1","text":"Перед запуском реактора восстановите охлаждение.","agents":["dzhanibekov"],"source":{"id":"survival-scenario-v1","title":"Правила"}}`))
	local, _ := NewLocal(knowledge.Catalog{Store: store}, "http://127.0.0.1:11434", "model")
	local.WithMission(&testView{value: mission.View{State: mission.State{Version: 1}}})
	local.client.Transport = testModelTransport(func(*http.Request) (*http.Response, error) {
		return modelSays("Сначала восстанови контур охлаждения, а затем включай реактор. [cooling-1]"), nil
	})
	reply, err := local.Talk(context.Background(), Request{Scope: "crew", PlayerID: "p", AgentID: "dzhanibekov", Text: "Как восстановить охлаждение?"})
	if err != nil || reply.Mode != "model" || reply.Grounding != "sources" || reply.MissionVersion != 1 {
		t.Fatalf("paraphrase was incorrectly discarded: %+v %v", reply, err)
	}
	if extractive(reply.Text, reply.Evidence) {
		t.Fatal("fixture accidentally became verbatim")
	}
}

func TestConversationRetainsLockedFactsAndSourceValidation(t *testing.T) {
	store, _ := rag.Read(strings.NewReader(`{"id":"visible","text":"Охлаждение нужно восстановить до запуска реактора.","agents":["dzhanibekov"],"source":{"id":"survival-scenario-v1","title":"Правила"}}
{"id":"secret","text":"СЕКРЕТНЫЙ_МАРКЕР: микрометеорит вызвал аварию.","requires_facts":["cause_confirmed"],"agents":["dzhanibekov"],"source":{"id":"survival-scenario-v1","title":"Правила"}}`))
	for _, answer := range []string{"Микрометеорит вызвал аварию. [visible]", "Охлаждение исправлено. [secret]", "Ответ по неизвестному документу. [invented]"} {
		local, _ := NewLocal(knowledge.Catalog{Store: store}, "http://127.0.0.1:11434", "model")
		local.WithMission(&testView{value: mission.View{State: mission.State{Version: 1}}})
		local.client.Transport = testModelTransport(func(r *http.Request) (*http.Response, error) {
			request, _ := io.ReadAll(r.Body)
			if strings.Contains(string(request), "СЕКРЕТНЫЙ_МАРКЕР") {
				t.Fatal("locked evidence sent to model")
			}
			return modelSays(answer), nil
		})
		reply, err := local.Talk(context.Background(), Request{Scope: "crew", PlayerID: "p", AgentID: "dzhanibekov", Text: "Как восстановить охлаждение?"})
		if err != nil || reply.Mode == "model" || reply.FallbackReason == "" || strings.Contains(reply.Text, "микрометеорит") {
			t.Fatalf("unsupported answer leaked: %+v %v", reply, err)
		}
	}
}

func TestConversationClearsRevokedFactHistoryWithinSameRoom(t *testing.T) {
	local, _ := NewLocal(catalog(t, true), "http://127.0.0.1:11434", "model")
	view := &testView{value: mission.View{State: mission.State{Version: 1}, Facts: []mission.Fact{{ID: "role_fact", Text: "ДОСТУП_ИНЖЕНЕРА"}}}}
	local.WithMission(view)
	calls := 0
	local.client.Transport = testModelTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		request, _ := io.ReadAll(r.Body)
		if calls == 1 {
			return modelSays("Привет! ДОСТУП_ИНЖЕНЕРА"), nil
		}
		if strings.Contains(string(request), "ДОСТУП_ИНЖЕНЕРА") {
			t.Fatal("old private role context leaked after role revocation")
		}
		return modelSays("Привет! Я на связи."), nil
	})
	request := Request{Scope: "same-room", PlayerID: "p", AgentID: "dzhanibekov", Text: "Привет"}
	if _, err := local.Talk(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	view.value = mission.View{State: mission.State{Version: 2}}
	if _, err := local.Talk(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatal(calls)
	}
}

func TestConversationClearsMissionHistoryAfterLeaving(t *testing.T) {
	local, err := NewLocal(catalog(t, true), "http://127.0.0.1:11434", "model")
	if err != nil {
		t.Fatal(err)
	}
	view := &testView{err: mission.ErrNotJoined}
	local.WithMission(view)
	local.history[key{"p", "dzhanibekov"}] = conversation{scope: "1:private_fact", messages: []message{{Role: "assistant", Content: "ПРИВАТНЫЙ_ФАКТ"}}}
	local.client.Transport = testModelTransport(func(r *http.Request) (*http.Response, error) {
		request, _ := io.ReadAll(r.Body)
		if strings.Contains(string(request), "ПРИВАТНЫЙ_ФАКТ") {
			t.Fatal("private mission history survived leaving")
		}
		return modelSays("Привет! Я на связи."), nil
	})
	reply, err := local.Talk(context.Background(), Request{PlayerID: "p", AgentID: "dzhanibekov", Text: "Привет"})
	if err != nil || reply.Mode != "model" {
		t.Fatal(reply, err)
	}
}

func TestConversationModelStatusAndActionableFallback(t *testing.T) {
	local, _ := NewLocal(catalog(t, false), "http://127.0.0.1:11434", "qwen3:4b-instruct-2507-q4_K_M")
	for _, tc := range []struct {
		body       any
		httpStatus int
		code       string
		ready      bool
	}{
		{map[string]any{"models": []any{map[string]string{"name": "qwen3:4b-instruct-2507-q4_K_M"}}}, 200, "ready", true},
		{map[string]any{"models": []any{map[string]string{"name": "qwen3:8b"}}}, 200, "model_missing", false},
		{map[string]string{"error": "unavailable"}, 503, "service_error", false},
	} {
		local.client.Transport = testModelTransport(func(r *http.Request) (*http.Response, error) {
			if r.Method != "GET" || r.URL.Path != "/api/tags" {
				t.Fatal("status must use the local models endpoint")
			}
			deadline, ok := r.Context().Deadline()
			if !ok || time.Until(deadline) > 3*time.Second {
				t.Fatal("probe lacks bounded deadline")
			}
			return responseJSON(tc.body, tc.httpStatus), nil
		})
		got := local.Status(context.Background())
		if got.Code != tc.code || got.Ready != tc.ready {
			t.Fatal(got)
		}
	}
	local.client.Transport = testModelTransport(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("connection refused with private path")
	})
	if got := local.Status(context.Background()); got.Code != "unavailable" || strings.Contains(got.Message, "private path") {
		t.Fatal(got)
	}
	local.client.Transport = testModelTransport(func(*http.Request) (*http.Response, error) {
		return responseJSON(map[string]string{"error": "not found"}, 404), nil
	})
	reply, err := local.Talk(context.Background(), Request{PlayerID: "p", AgentID: "dzhanibekov", Text: "Питание"})
	if err != nil || reply.Mode == "model" || !strings.Contains(reply.FallbackReason, "ollama pull qwen3:4b-instruct-2507-q4_K_M") {
		t.Fatal(reply, err)
	}
	local.WithoutModel()
	local.client.Transport = testModelTransport(func(*http.Request) (*http.Response, error) {
		t.Fatal("disabled status called service")
		return nil, nil
	})
	if got := local.Status(context.Background()); got.Enabled || got.Ready || got.Code != "disabled" {
		t.Fatal(got)
	}
}
