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

func catalog(t *testing.T, empty bool) knowledge.Catalog {
	t.Helper()
	data := ""
	if !empty {
		data = `{"id":"power-1","text":"Питание батареи","agents":["dzhanibekov"],"source":{"id":"test","title":"Синтетический тест"}}`
	}
	s, err := rag.Read(strings.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	return knowledge.Catalog{Store: s}
}
func TestNoCorpusAndNoMatchesStillAttemptModel(t *testing.T) {
	for _, empty := range []bool{true, false} {
		var calls atomic.Int32
		h := newModelHTTP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(500) }))
		s, _ := NewLocal(catalog(t, empty), h.URL, "test-model")
		s.client.Transport = h
		reply, err := s.Talk(context.Background(), Request{Scope: "", PlayerID: "p", AgentID: "dzhanibekov", Text: "Неизвестно"})
		h.Close()
		if err != nil || calls.Load() != 1 || reply.Text == "" {
			t.Fatal(reply, err, calls.Load())
		}
	}
}
func TestToolRoundTripAndIsolatedHistory(t *testing.T) {
	var calls atomic.Int32
	h := newModelHTTP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Model string `json:"model"`
			Tools []struct {
				Function struct {
					Name string `json:"name"`
				} `json:"function"`
			} `json:"tools"`
			Messages []message `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Error(err)
		}
		if r.URL.Path != "/api/chat" || input.Model != "configured-qwen" || len(input.Tools) < 6 {
			t.Error("wrong API request")
		}
		for _, tool := range input.Tools {
			if !strings.HasPrefix(tool.Function.Name, "dzhanibekov_") {
				t.Error("other role exposed")
			}
		}
		n := calls.Add(1)
		if n == 1 {
			_, _ = w.Write([]byte(`{"message":{"role":"assistant","tool_calls":[{"function":{"name":"dzhanibekov_read_chunk","arguments":{"chunk_id":"power-1"}}}]}}`))
			return
		}
		if n == 2 && input.Messages[len(input.Messages)-1].Role != "tool" {
			t.Error("missing tool result")
		}
		if n == 3 {
			for _, m := range input.Messages {
				if m.Role == "assistant" {
					t.Error("player history leaked")
				}
			}
		}
		_, _ = w.Write([]byte(`{"message":{"role":"assistant","content":"Проверьте питание [power-1]."}}`))
	}))
	defer h.Close()
	s, _ := NewLocal(catalog(t, false), h.URL, "configured-qwen")
	s.client.Transport = h
	for _, player := range []string{"p1", "p2"} {
		reply, err := s.Talk(context.Background(), Request{Scope: "", PlayerID: player, AgentID: "dzhanibekov", Text: "Питание"})
		if err != nil || reply.Status != "grounded" || len(reply.Evidence) != 1 {
			t.Fatal(reply, err)
		}
	}
	if calls.Load() != 3 || len(s.history) != 2 {
		t.Fatal(calls.Load(), s.history)
	}
}
func TestRejectCrossRoleToolAndModelFailures(t *testing.T) {
	for _, body := range []string{`{"message":{"role":"assistant","tool_calls":[{"function":{"name":"ryazansky_search","arguments":{"query":"x"}}}]}}`, `{"message":{"role":"assistant","content":"Ответ без ссылки"}}`, `not json`, `{"error":"unavailable"}`} {
		h := newModelHTTP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) }))
		s, _ := NewLocal(catalog(t, false), h.URL, "test")
		s.client.Transport = h
		if reply, err := s.Talk(context.Background(), Request{Scope: "", PlayerID: "p", AgentID: "dzhanibekov", Text: "Питание"}); err != nil || reply.Mode != "knowledge_base" || reply.Status != "grounded" {
			t.Error("expected safe knowledge fallback", reply, err)
		}
		h.Close()
	}
}
func TestCancelAndBusy(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	h := newModelHTTP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(started); <-release }))
	defer h.Close()
	s, _ := NewLocal(catalog(t, false), h.URL, "test")
	s.client.Transport = h
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := s.Talk(ctx, Request{Scope: "", PlayerID: "p", AgentID: "dzhanibekov", Text: "Питание"})
		done <- err
	}()
	<-started
	if _, err := s.Talk(context.Background(), Request{Scope: "", PlayerID: "p", AgentID: "dzhanibekov", Text: "Питание"}); err != ErrBusy {
		t.Fatal(err)
	}
	cancel()
	err := <-done
	close(release)
	if err == nil {
		t.Fatal("cancel ignored")
	}
}
func TestLocalOnlyURL(t *testing.T) {
	for _, url := range []string{"https://example.com", "http://127.0.0.1@evil.example", "http://127.0.0.1/a", "http://127.0.0.1?x=y"} {
		if _, err := NewLocal(catalog(t, true), url, "x"); err == nil {
			t.Fatal(url)
		}
	}
}
