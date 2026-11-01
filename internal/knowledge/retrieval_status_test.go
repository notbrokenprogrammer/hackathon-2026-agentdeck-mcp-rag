package knowledge

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"feeders/internal/rag"
)

func TestRAGStatusReportsConfigurationWithoutHiddenInventory(t *testing.T) {
	store, err := rag.Read(strings.NewReader(`{"id":"public","text":"Открытое руководство.","agents":["savinykh"],"source":{"id":"manual","title":"Руководство"}}
{"id":"hidden","text":"ЗАКРЫТАЯ_УЛИКА","requires_facts":["private_fact"],"agents":["savinykh"],"source":{"id":"manual","title":"Руководство"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.WithHybrid(rag.HybridConfig{URL: "http://127.0.0.1:11434", Model: "embeddinggemma", CachePath: "/private/not-a-real-cache"}); err != nil {
		t.Fatal(err)
	}
	result, err := (Catalog{Store: store}).Call(context.Background(), "rag_status", nil)
	if err != nil {
		t.Fatal(err)
	}
	status := result.(map[string]any)
	if status["chunks"] != 1 || status["visible_chunks"] != 1 {
		t.Fatal("closed facts affected public count", status)
	}
	encoded, _ := json.Marshal(status["retrieval"])
	if !strings.Contains(string(encoded), "embeddinggemma") || !strings.Contains(string(encoded), `"configured":true`) {
		t.Fatal(string(encoded))
	}
	for _, secret := range []string{"total", "count", "private", "not-a-real-cache", "ЗАКРЫТАЯ"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatal("private retrieval metadata leaked", string(encoded))
		}
	}
}
