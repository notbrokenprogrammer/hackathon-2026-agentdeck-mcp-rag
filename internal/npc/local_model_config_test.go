package npc

import (
	"context"
	"encoding/json"
	"feeders/internal/knowledge"
	"feeders/internal/rag"
	"io"
	"net/http"
	"strings"
	"testing"
)

type testModelTransport func(*http.Request) (*http.Response, error)

func (f testModelTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// The real RAG, prompts, tools and Ollama JSON run through an in-process HTTP
// transport. No listening socket or downloaded model is needed for this test.
func TestFourAgentsLocalModelRequestAndEvidence(t *testing.T) {
	store, err := rag.Load("../../knowledge")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ id, q string }{{"savinykh", "Салют-7 1985"}, {"dzhanibekov", "ручное управление отказ автоматики"}, {"ryazansky", "образование биохимия МГУ"}, {"krikalev", "радиосвязь школьники"}} {
		t.Run(tc.id, func(t *testing.T) {
			local, err := NewLocal(knowledge.Catalog{Store: store}, "http://127.0.0.1:11434", "qwen3:4b-instruct-2507-q4_K_M")
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			local.client.Transport = testModelTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				var body struct {
					Model         string
					Stream, Think bool
					Messages      []message
					Tools         []struct{ Function struct{ Name string } }
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				if body.Model != "qwen3:4b-instruct-2507-q4_K_M" || body.Think || body.Stream || r.URL.Path != "/api/chat" {
					t.Fatal("incorrect local model config")
				}
				for _, tool := range body.Tools {
					if !strings.HasPrefix(tool.Function.Name, tc.id+"_") {
						t.Fatal("cross-agent tool")
					}
				}
				parts := strings.SplitN(body.Messages[len(body.Messages)-1].Content, "\n", 2)
				var evidence rag.SearchResult
				if len(parts) != 2 || json.Unmarshal([]byte(parts[1]), &evidence) != nil || len(evidence.Hits) == 0 {
					t.Fatal("missing RAG")
				}
				first := evidence.Hits[0]
				reply, _ := json.Marshal(map[string]any{"message": message{Role: "assistant", Content: first.Text + " [" + first.ID + "]"}})
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(reply))), Header: make(http.Header)}, nil
			})
			reply, err := local.Talk(context.Background(), Request{Scope: "test", PlayerID: "tester", AgentID: tc.id, Text: tc.q})
			if err != nil || reply.Mode != "model" || reply.Model != "qwen3:4b-instruct-2507-q4_K_M" || calls != 1 || len(reply.Evidence) == 0 {
				t.Fatalf("model connection failed: %+v %v", reply, err)
			}
			local.client.Transport = testModelTransport(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 404, Body: io.NopCloser(strings.NewReader("model missing")), Header: make(http.Header)}, nil
			})
			reply, err = local.Talk(context.Background(), Request{PlayerID: "tester", AgentID: tc.id, Text: tc.q})
			if err != nil || reply.Mode != "knowledge_base" || reply.FallbackReason == "" {
				t.Fatal("model failure not identified")
			}
		})
	}
}
