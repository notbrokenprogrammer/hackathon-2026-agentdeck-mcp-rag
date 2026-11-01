package npc

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"feeders/internal/knowledge"
	"feeders/internal/rag"
)

// Real shipped documents, real search/catalog/client code, mock HTTP inference.
// This checks the integration boundary without claiming to run model weights.
func TestThreeCrewCorporaReachConfiguredModel(t *testing.T) {
	store, err := rag.Load("../../knowledge")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ id, query string }{
		{"dzhanibekov", "Как Джанибеков проверял ручное управление при отказе автоматики?"},
		{"ryazansky", "Где Рязанский изучал биохимию и работал до космических полётов?"},
		{"krikalev", "Как Крикалёв поддерживал любительскую радиосвязь со школьниками?"},
	} {
		t.Run(tc.id, func(t *testing.T) {
			var calls atomic.Int32
			h := newModelHTTP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				var input struct {
					Model    string    `json:"model"`
					Messages []message `json:"messages"`
					Tools    []struct {
						Function struct {
							Name string `json:"name"`
						} `json:"function"`
					} `json:"tools"`
				}
				if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
					t.Error(err)
					w.WriteHeader(500)
					return
				}
				if r.URL.Path != "/api/chat" || input.Model != "crew-test-model" {
					t.Error("request did not reach the configured Ollama API")
				}
				for _, tool := range input.Tools {
					if !strings.HasPrefix(tool.Function.Name, tc.id+"_") {
						t.Error("another persona's tool exposed to the model")
					}
				}
				data := strings.SplitN(input.Messages[len(input.Messages)-1].Content, "\n", 2)
				var found rag.SearchResult
				if len(data) != 2 || json.Unmarshal([]byte(data[1]), &found) != nil {
					t.Error("no retrieved evidence in the model request")
					w.WriteHeader(500)
					return
				}
				if len(found.Hits) == 0 {
					for _, m := range input.Messages {
						if m.Role == "assistant" {
							t.Error("other player's history leaked")
						}
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"message": message{Role: "assistant", Content: "О какой теме рассказать проще?"}})
					return
				}
				for _, hit := range found.Hits {
					if len(hit.Agents) != 1 || hit.Agents[0] != tc.id {
						t.Errorf("cross-persona evidence: %s", hit.ID)
					}
					if hit.Source.ID != "survival-scenario-v1" && (hit.Source.URL == "" || hit.Source.ContentMode == "" || hit.Source.RelationToPerson == "" || hit.Source.Language == "") {
						t.Error("source provenance was dropped before inference")
					}
				}
				first := found.Hits[0]
				_ = json.NewEncoder(w).Encode(map[string]any{"message": message{Role: "assistant", Content: first.Text + " [" + first.ID + "]"}})
			}))
			defer h.Close()
			s, err := NewLocal(knowledge.Catalog{Store: store}, h.URL, "crew-test-model")
			s.client.Transport = h
			if err != nil {
				t.Fatal(err)
			}
			for _, question := range []string{tc.query, "Объясни проще", "Подробнее"} {
				reply, err := s.Talk(context.Background(), Request{Scope: "", PlayerID: "player-one", AgentID: tc.id, Text: question})
				if err != nil || reply.Mode != "model" || reply.Status != "grounded" || len(reply.Evidence) == 0 {
					t.Fatalf("model path failed: mode=%s status=%s err=%v", reply.Mode, reply.Status, err)
				}
			}
			if calls.Load() != 3 {
				t.Fatalf("expected inference on question and two follow-ups, got %d", calls.Load())
			}
			reply, err := s.Talk(context.Background(), Request{Scope: "", PlayerID: "another-player", AgentID: tc.id, Text: "Объясни проще"})
			if err != nil || reply.Mode != "model" || reply.Grounding != "conversation" || calls.Load() != 4 {
				t.Fatal("another player's topic leaked", reply, err)
			}
		})
	}
}

func TestCrewDocumentInventoryAndScope(t *testing.T) {
	store, err := rag.Load("../../knowledge")
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"dzhanibekov", "ryazansky", "krikalev"} {
		count := 0
		for _, source := range store.Sources(id) {
			if source.ID == "survival-scenario-v1" || source.ID == "ship-lab-lesson-v1" {
				continue
			}
			count++
			if source.ContentMode == "" || source.RelationToPerson == "" || !strings.HasPrefix(source.URL, "http") {
				t.Fatalf("incomplete source: %+v", source)
			}
		}
		if count != 20 {
			t.Fatalf("%s has %d historical/scientific documents; want 20", id, count)
		}
		other := "krikalev"
		if id == other {
			other = "ryazansky"
		}
		if _, err := store.Get(id, other+"_doc_001_c001"); err == nil {
			t.Fatal("another person's document was readable")
		}
		found, err := store.Search(context.Background(), id, "несуществующийквазикварковыйборщ", "", 3)
		if err != nil || found.Status != "no_matches" {
			t.Fatal("invented topic should not produce evidence", found, err)
		}
	}
}
