package agents

import (
	"context"
	"strings"
	"testing"

	"feeders/internal/knowledge"
	"feeders/internal/npc"
	"feeders/internal/rag"
)

func TestCrewExplicitModesPreserveMetadataAndBypassHistoricalFallback(t *testing.T) {
	store, err := rag.Read(strings.NewReader(""))
	if err != nil {
		t.Fatal(err)
	}
	local, err := npc.NewLocal(knowledge.Catalog{Store: store}, "http://127.0.0.1:11434", "test")
	if err != nil {
		t.Fatal(err)
	}
	local.WithoutModel()
	// A nil old mentor makes accidental delegation on the historical trigger
	// immediately visible. Explicit modes must use the current NPC pipeline.
	service := NewCrewService(local, nil)
	for _, mode := range []string{npc.DialogueArticles, npc.DialogueCasual} {
		reply, err := service.Talk(context.Background(), Request{PlayerID: "p", AgentID: "savinykh", Text: "История Салюта-7 в 1985 году", DialogueMode: mode})
		if err != nil || reply.DialogueMode != mode || reply.Model != "test" || reply.Citation != nil || len(reply.Citations) != 0 {
			t.Fatal(reply, err)
		}
		if mode == npc.DialogueArticles && (reply.Status != "no_sources" || reply.Retrieval == nil) {
			t.Fatal("articles metadata missing", reply)
		}
		if mode == npc.DialogueCasual && (reply.Status != "model_unavailable" || reply.Retrieval != nil) {
			t.Fatal("casual unexpectedly used documents", reply)
		}
	}
}
