package npc

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"feeders/internal/knowledge"
	"feeders/internal/mission"
	"feeders/internal/rag"
	"feeders/internal/storage"
)

type forbiddenMissionView struct{ t *testing.T }

func (v forbiddenMissionView) View(string) (mission.View, error) {
	v.t.Error("casual conversation requested mission facts")
	return mission.View{}, errors.New("forbidden")
}

func modeCatalog(t *testing.T) knowledge.Catalog {
	t.Helper()
	store, err := rag.Read(strings.NewReader(`{"id":"article-1","text":"Привет! Питание подаётся от батарей. СТАТЬЯ_МАРКЕР.","agents":["savinykh","dzhanibekov","ryazansky","krikalev"],"source":{"id":"article","title":"Энергия корабля"}}`))
	if err != nil {
		t.Fatal(err)
	}
	return knowledge.Catalog{Store: store}
}

func TestArticlesAlwaysSearchEvenGreetingAndNeverUseGeneralKnowledge(t *testing.T) {
	for _, answer := range []string{"Привет! Питание подаётся от батарей. [article-1]", "Привет! Что обсудим?"} {
		local, err := NewLocal(modeCatalog(t), "http://127.0.0.1:11434", "test")
		if err != nil {
			t.Fatal(err)
		}
		calls := 0
		local.client.Transport = testModelTransport(func(r *http.Request) (*http.Response, error) {
			calls++
			body, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(body), "СТАТЬЯ_МАРКЕР") {
				t.Fatal("articles greeting skipped retrieval")
			}
			var payload struct {
				Tools []struct{ Function struct{ Name string } }
			}
			if err := json.Unmarshal(body, &payload); err != nil {
				t.Fatal(err)
			}
			for _, tool := range payload.Tools {
				if !strings.HasSuffix(tool.Function.Name, "_search") && !strings.HasSuffix(tool.Function.Name, "_read_chunk") && !strings.HasSuffix(tool.Function.Name, "_list_sources") {
					t.Fatal("non-document tool in articles mode", tool.Function.Name)
				}
			}
			return modelSays(answer), nil
		})
		reply, err := local.Talk(context.Background(), Request{PlayerID: "p", AgentID: "dzhanibekov", Text: "Привет!", DialogueMode: DialogueArticles})
		if err != nil || calls != 1 || reply.DialogueMode != DialogueArticles || reply.Retrieval == nil || reply.Grounding != "sources" || len(reply.Evidence) == 0 {
			t.Fatal(reply, calls, err)
		}
		if strings.Contains(answer, "[article-1]") && reply.Mode != "model" {
			t.Fatal("grounded answer rejected", reply)
		}
		if !strings.Contains(answer, "[article-1]") && (reply.Mode == "model" || reply.FallbackReason == "") {
			t.Fatal("ungrounded model greeting accepted as article", reply)
		}
	}
	for _, enabled := range []bool{true, false} {
		local, _ := NewLocal(catalog(t, true), "http://127.0.0.1:11434", "test")
		if !enabled {
			local.WithoutModel()
		}
		local.client.Transport = testModelTransport(func(*http.Request) (*http.Response, error) {
			t.Fatal("no_sources must not generate an invented answer")
			return nil, nil
		})
		reply, err := local.Talk(context.Background(), Request{PlayerID: "p", AgentID: "savinykh", Text: "Привет!", DialogueMode: DialogueArticles})
		if err != nil || reply.Status != "no_sources" || reply.Grounding != "none" || reply.Mode == "model" || reply.Retrieval == nil {
			t.Fatal(reply, err)
		}
	}
}

func TestCasualAllPersonasSkipDocumentsToolsAndMissionContext(t *testing.T) {
	for _, id := range []string{"savinykh", "dzhanibekov", "ryazansky", "krikalev"} {
		t.Run(id, func(t *testing.T) {
			catalog := modeCatalog(t)
			catalog.ViewProvider = func(context.Context) (mission.View, error) {
				t.Error("casual bound RAG view")
				return mission.View{}, errors.New("forbidden")
			}
			local, _ := NewLocal(catalog, "http://127.0.0.1:11434", "our-model")
			local.WithMission(forbiddenMissionView{t}).WithMCP("/unavailable/mcp", "private-mission-file", "corpus")
			local.history[key{"p", id}] = conversation{messages: []message{{Role: "assistant", Content: "LEGACY_PRIVATE_MARKER"}}}
			local.history[Request{PlayerID: "p", AgentID: id, DialogueMode: DialogueArticles}.memoryKey()] = conversation{messages: []message{{Role: "assistant", Content: "ARTICLES_PRIVATE_MARKER"}}}
			local.client.Transport = testModelTransport(func(r *http.Request) (*http.Response, error) {
				body, _ := io.ReadAll(r.Body)
				var payload map[string]json.RawMessage
				if err := json.Unmarshal(body, &payload); err != nil {
					t.Fatal(err)
				}
				if _, found := payload["tools"]; found {
					t.Fatal("casual exposed tool definitions")
				}
				for _, secret := range []string{"СТАТЬЯ_МАРКЕР", "LEGACY_PRIVATE_MARKER", "ARTICLES_PRIVATE_MARKER", "MISSION_PRIVATE_MARKER"} {
					if strings.Contains(string(body), secret) {
						t.Fatal("context from another mode leaked", secret)
					}
				}
				return modelSays("Привет! Можем поболтать о космосе. [улыбается]"), nil
			})
			ctx := mission.WithView(context.Background(), mission.View{Facts: []mission.Fact{{ID: "hidden", Text: "MISSION_PRIVATE_MARKER"}}})
			reply, err := local.Talk(ctx, Request{PlayerID: "p", AgentID: id, Text: "Привет!", DialogueMode: DialogueCasual})
			if err != nil || reply.Mode != "model" || reply.DialogueMode != DialogueCasual || reply.Model != "our-model" || reply.Grounding != "conversation" {
				t.Fatal(reply, err)
			}
			if len(reply.Evidence) != 0 || reply.Retrieval != nil || reply.Analysis != nil || reply.ToolTransport != "" {
				t.Fatal("casual answer received source/tool metadata", reply)
			}
		})
	}
}

func TestCasualFailureNeverFallsBackToArticles(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		local, _ := NewLocal(modeCatalog(t), "http://127.0.0.1:11434", "test")
		if !enabled {
			local.WithoutModel()
		}
		local.client.Transport = testModelTransport(func(*http.Request) (*http.Response, error) { return nil, errors.New("offline") })
		reply, err := local.Talk(context.Background(), Request{PlayerID: "p", AgentID: "savinykh", Text: "Питание", DialogueMode: DialogueCasual})
		if err != nil || reply.Mode != "unavailable" || reply.Status != "model_unavailable" || reply.FallbackReason == "" || len(reply.Evidence) != 0 || reply.Retrieval != nil {
			t.Fatal(reply, err)
		}
		if strings.Contains(reply.Text, "СТАТЬЯ_МАРКЕР") || strings.Contains(reply.FallbackReason, "справочный ответ") {
			t.Fatal("casual silently became document mode", reply)
		}
	}
}

func TestLongCasualConversationDoesNotExceedRequestBudget(t *testing.T) {
	local, _ := NewLocal(modeCatalog(t), "http://127.0.0.1:11434", "test")
	r := Request{PlayerID: "p", AgentID: "savinykh", Text: "Продолжим", DialogueMode: DialogueCasual}
	var history []message
	for i := 0; i < 6; i++ {
		history = append(history, message{Role: "user", Content: strings.Repeat("x", 1500)}, message{Role: "assistant", Content: strings.Repeat(`"`, 7000)})
	}
	local.history[r.memoryKey()] = conversation{messages: history}
	calls := 0
	local.client.Transport = testModelTransport(func(req *http.Request) (*http.Response, error) {
		calls++
		body, _ := io.ReadAll(req.Body)
		if len(body) > 64<<10 {
			t.Fatal("history still exceeds request budget", len(body))
		}
		return modelSays("Да, продолжаем."), nil
	})
	reply, err := local.Talk(context.Background(), r)
	if err != nil || reply.Mode != "model" || calls != 1 {
		t.Fatal("long history blocked all further inference", reply, calls, err)
	}
	if len(local.history[r.memoryKey()].messages) != 12 {
		t.Fatal("trimming request destroyed persistent conversation")
	}
}

func TestArticlesReportActualHybridOrDegradedRetrieval(t *testing.T) {
	for _, missing := range []bool{false, true} {
		catalog := modeCatalog(t)
		embeddings := 0
		transport := testModelTransport(func(r *http.Request) (*http.Response, error) {
			embeddings++
			if r.URL.Path != "/api/embed" {
				t.Fatal("wrong embedding endpoint", r.URL.Path)
			}
			if missing {
				return responseJSON(map[string]string{"error": "model missing"}, 404), nil
			}
			var payload struct {
				Input []string `json:"input"`
				Model string   `json:"model"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			if payload.Model != "test-embed" {
				t.Fatal("wrong embedding model")
			}
			vectors := make([][]float64, len(payload.Input))
			for i := range vectors {
				vectors[i] = []float64{1, 0}
			}
			return responseJSON(map[string]any{"embeddings": vectors}, 200), nil
		})
		if err := catalog.Store.WithHybrid(rag.HybridConfig{URL: "http://127.0.0.1:11434", Model: "test-embed", HTTPClient: &http.Client{Transport: transport}}); err != nil {
			t.Fatal(err)
		}
		local, _ := NewLocal(catalog, "http://127.0.0.1:11434", "chat-model")
		local.WithMCP("/not-a-real-mcp-binary", "view", "corpus")
		local.client.Transport = testModelTransport(func(*http.Request) (*http.Response, error) {
			return modelSays("Питание подаётся от батарей. [article-1]"), nil
		})
		reply, err := local.Talk(context.Background(), Request{PlayerID: "p", AgentID: "dzhanibekov", Text: "Питание", DialogueMode: DialogueArticles})
		if err != nil || reply.Mode != "model" || reply.Retrieval == nil || reply.Retrieval.EmbeddingModel != "test-embed" || embeddings == 0 {
			t.Fatal(reply, err)
		}
		if reply.ToolTransport != "Локальный поиск" {
			t.Fatal("articles unexpectedly spawned a separate MCP index", reply.ToolTransport)
		}
		if missing && (reply.Retrieval.Mode != "bm25_fallback" || reply.Retrieval.DegradedReason != "embedding_model_missing") {
			t.Fatal("failed semantic retrieval claimed hybrid", reply.Retrieval)
		}
		if !missing && (reply.Retrieval.Mode != "hybrid" || reply.Retrieval.DegradedReason != "") {
			t.Fatal("successful hybrid metadata lost", reply.Retrieval)
		}
	}
}

func TestRevokedArticleTopicNeverEntersLaterQueryEmbedding(t *testing.T) {
	store, err := rag.Read(strings.NewReader(`{"id":"public","text":"Питание поддерживается батареями.","agents":["dzhanibekov"],"source":{"id":"manual","title":"Руководство"}}
{"id":"private","text":"ДОСТУП_МАРКЕР: резервный аккумулятор установлен.","requires_facts":["private_fact"],"agents":["dzhanibekov"],"source":{"id":"manual","title":"Руководство"}}`))
	if err != nil {
		t.Fatal(err)
	}
	revoked, queriesAfterRevocation := false, 0
	embeddings := &http.Client{Transport: testModelTransport(func(r *http.Request) (*http.Response, error) {
		var payload struct {
			Input []string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		vectors := make([][]float64, len(payload.Input))
		for i, input := range payload.Input {
			if revoked {
				if strings.Contains(input, "ДОСТУП_МАРКЕР") {
					t.Fatal("revoked private topic was sent to embedding service", input)
				}
				if strings.HasPrefix(input, "task: search result | query:") {
					queriesAfterRevocation++
				}
			}
			vectors[i] = []float64{1, 0}
		}
		return responseJSON(map[string]any{"embeddings": vectors}, 200), nil
	})}
	if err := store.WithHybrid(rag.HybridConfig{URL: "http://127.0.0.1:11434", Model: "embeddinggemma", HTTPClient: embeddings}); err != nil {
		t.Fatal(err)
	}
	local, _ := NewLocal(knowledge.Catalog{Store: store}, "http://127.0.0.1:11434", "chat-model")
	view := &testView{value: mission.View{State: mission.State{Version: 1}, Facts: []mission.Fact{{ID: "private_fact", Text: "ДОСТУП_МАРКЕР"}}}}
	local.WithMission(view)
	local.client.Transport = testModelTransport(func(r *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(r.Body)
		if revoked {
			if strings.Contains(string(body), "ДОСТУП_МАРКЕР") {
				t.Fatal("revoked history/source reached chat model")
			}
			return modelSays("Питание поддерживается батареями. [public]"), nil
		}
		return modelSays("ДОСТУП_МАРКЕР: резервный аккумулятор установлен. [private]"), nil
	})
	r := Request{Scope: "same-room", PlayerID: "p", AgentID: "dzhanibekov", Text: "ДОСТУП_МАРКЕР питание", DialogueMode: DialogueArticles}
	first, err := local.Talk(context.Background(), r)
	if err != nil || first.Mode != "model" {
		t.Fatal(first, err)
	}
	revoked = true
	view.value = mission.View{State: mission.State{Version: 2}}
	r.Text = "Объясни проще"
	second, err := local.Talk(context.Background(), r)
	if err != nil || second.Mode != "model" || second.Retrieval == nil || second.Retrieval.Mode != "hybrid" || queriesAfterRevocation == 0 {
		t.Fatal("revocation test did not exercise a real subsequent hybrid query", second, queriesAfterRevocation, err)
	}
	for _, chunk := range second.Evidence {
		if chunk.ID == "private" {
			t.Fatal("cached private vector bypassed access filter")
		}
	}
}

func TestDialogueModeValidationAndMemorySurviveRestartSeparately(t *testing.T) {
	store, err := storage.NewSQLiteStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	makeLocal := func() *Local {
		local, err := NewLocal(modeCatalog(t), "http://127.0.0.1:11434", "test")
		if err != nil {
			t.Fatal(err)
		}
		if err := local.WithMemory(store); err != nil {
			t.Fatal(err)
		}
		return local
	}
	local := makeLocal()
	local.client.Transport = testModelTransport(func(r *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), "РЕЖИМ СТАТЕЙ") {
			return modelSays("Питание подаётся от батарей. [article-1]"), nil
		}
		return modelSays("ЛИЧНЫЙ_ОТВЕТ: давай обсудим музыку."), nil
	})
	for _, mode := range []string{DialogueArticles, DialogueCasual} {
		text := "Питание"
		if mode == DialogueCasual {
			text = "ЛИЧНАЯ_ТЕМА музыка"
		}
		if _, err := local.Talk(context.Background(), Request{Scope: "room", PlayerID: "p", AgentID: "dzhanibekov", Text: text, DialogueMode: mode}); err != nil {
			t.Fatal(err)
		}
	}
	restored := makeLocal()
	if _, err := restored.Talk(context.Background(), Request{PlayerID: "p", AgentID: "dzhanibekov", Text: "Привет", DialogueMode: "invalid"}); err == nil {
		t.Fatal("invalid dialogue mode accepted")
	}
	for _, mode := range []string{DialogueArticles, DialogueCasual} {
		r := Request{Scope: "room", PlayerID: "p", AgentID: "dzhanibekov", Text: "Продолжи", DialogueMode: mode}
		history := restored.history[r.memoryKey()].messages
		joined := rag.JSON(history)
		if len(history) != 2 {
			t.Fatal("mode history was not persisted", mode, history)
		}
		if mode == DialogueArticles && strings.Contains(joined, "ЛИЧН") {
			t.Fatal("casual history mixed into articles")
		}
		if mode == DialogueCasual && (strings.Contains(joined, "article-1") || !strings.Contains(joined, "ЛИЧНАЯ_ТЕМА")) {
			t.Fatal("article history mixed into casual")
		}
		r.DialogueMode = ""
		if len(restored.history[r.memoryKey()].messages) != 0 {
			t.Fatal("explicit history appeared in legacy namespace")
		}
	}
}
