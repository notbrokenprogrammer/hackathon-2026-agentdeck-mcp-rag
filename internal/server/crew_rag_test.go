package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"feeders/internal/agents"
	"feeders/internal/knowledge"
	"feeders/internal/npc"
	"feeders/internal/rag"
	"feeders/pkg/protocol"
)

func TestThreeCrewRAGAnswersThroughAuthenticatedWebSocket(t *testing.T) {
	store, err := rag.Load("../../knowledge")
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Error(err)
			w.WriteHeader(500)
			return
		}
		parts := strings.SplitN(input.Messages[len(input.Messages)-1].Content, "\n", 2)
		var found rag.SearchResult
		if len(parts) != 2 || json.Unmarshal([]byte(parts[1]), &found) != nil || len(found.Hits) == 0 {
			t.Error("missing RAG evidence in inference request")
			w.WriteHeader(500)
			return
		}
		calls.Add(1)
		hit := found.Hits[0]
		_ = json.NewEncoder(w).Encode(map[string]any{"message": map[string]string{"role": "assistant", "content": hit.Text + " [" + hit.ID + "]"}})
	}))
	defer model.Close()
	local, err := npc.NewLocal(knowledge.Catalog{Store: store}, model.URL, "ws-test-model")
	if err != nil {
		t.Fatal(err)
	}
	mentor, err := agents.NewMentor(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	host, _ := fullHost(t, agents.NewCrewService(local, mentor))
	conn, _ := registerSocket(t, host, "rag-crew-test")
	sendPacket(t, conn, "player.init", "init-rag", map[string]any{})
	readPacket(t, conn, func(e protocol.Envelope) bool { return e.Type == "player.ready" })
	for _, tc := range []struct{ id, question string }{
		{"dzhanibekov", "Джанибеков и ручное управление при отказе автоматики"},
		{"ryazansky", "Рязанский биохимия ИМБП образование"},
		{"krikalev", "Крикалёв радиосвязь со школьниками"},
	} {
		requestID := "ask-" + tc.id
		sendPacket(t, conn, "agent.talk", requestID, map[string]string{"agent_id": tc.id, "text": tc.question})
		e := readPacket(t, conn, func(e protocol.Envelope) bool { return e.RequestID == requestID })
		if e.Type != "agent.reply" {
			t.Fatalf("%s: %s %s", tc.id, e.Type, e.Payload)
		}
		var reply agents.Reply
		if err := json.Unmarshal(e.Payload, &reply); err != nil {
			t.Fatal(err)
		}
		var wire protocol.AgentReply
		if err := protocol.DecodePayload(e.Payload, &wire); err != nil {
			t.Fatalf("wire contract rejected the RAG response: %v", err)
		}
		if reply.AgentID != tc.id || reply.Mode != "model" || len(reply.Citations) == 0 || !strings.HasPrefix(reply.Citations[0].DocumentID, tc.id+"_doc_") {
			t.Fatalf("wrong RAG/model/source in WebSocket response: %+v", reply)
		}
	}
	if calls.Load() != 3 {
		t.Fatalf("not all three characters reached model API: %d", calls.Load())
	}
}
