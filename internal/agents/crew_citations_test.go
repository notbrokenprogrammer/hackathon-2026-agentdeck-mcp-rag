package agents

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"feeders/internal/knowledge"
	"feeders/internal/npc"
	"feeders/internal/rag"
)

func TestCrewCitationPointsToActuallyCitedDocument(t *testing.T) {
	var rows []string
	for _, id := range []string{"first", "second"} {
		rows = append(rows, rag.JSON(rag.Chunk{ID: id, Text: "Питание аккумуляторов", Agents: []string{"dzhanibekov"},
			Source: rag.Source{ID: id + "-source", Title: id, URL: "https://www.nasa.gov/", ContentMode: "public_domain_excerpt", RelationToPerson: "role_background", Language: "ru"}}))
	}
	store, err := rag.Read(strings.NewReader(strings.Join(rows, "\n")))
	if err != nil {
		t.Fatal(err)
	}
	h := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Search ties put first before second. Cite only the second document.
		_ = json.NewEncoder(w).Encode(map[string]any{"message": map[string]string{"role": "assistant", "content": "Питание аккумуляторов [second]"}})
	}))
	defer h.Close()
	local, err := npc.NewLocal(knowledge.Catalog{Store: store}, h.URL, "test")
	if err != nil {
		t.Fatal(err)
	}
	reply, err := NewCrewService(local, nil).Talk(context.Background(), Request{Scope: "", PlayerID: "p", AgentID: "dzhanibekov", Text: "Питание"})
	if err != nil || reply.Mode != "model" || len(reply.Citations) != 1 || reply.Citation == nil {
		t.Fatal(reply, err)
	}
	if reply.Citation.DocumentID != "second-source" || reply.Citation.ContentMode != "public_domain_excerpt" || reply.Citation.RelationToPerson != "role_background" {
		t.Fatalf("UI citation does not describe the document the model used: %+v", reply.Citation)
	}
}
