package retrievalconfig

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"feeders/internal/rag"
)

type testTransport func(*http.Request) (*http.Response, error)

func (f testTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type privateStatus struct{}

func (privateStatus) HybridStatus() rag.HybridStatus {
	return rag.HybridStatus{Configured: true, Code: "embedding_model_missing", Model: "embeddinggemma", Message: "/private/secret-cache", Count: 7, Total: 81}
}

func TestSharedDefaultsOverridesAndNoConfigurationSideEffects(t *testing.T) {
	for _, key := range []string{"DATA_DIR", "OLLAMA_URL", "RAG_HYBRID_ENABLED", "RAG_EMBEDDING_URL", "RAG_EMBEDDING_MODEL", "RAG_EMBEDDING_CACHE"} {
		t.Setenv(key, "")
	}
	data := filepath.Join(t.TempDir(), "not-created")
	config, err := Load(data)
	if err != nil || !config.Enabled || config.Hybrid.Model != "embeddinggemma" || config.Hybrid.URL != "http://127.0.0.1:11434" || config.Hybrid.CachePath != filepath.Join(data, "rag-vectors.json") {
		t.Fatal(config, err)
	}
	store, _ := rag.Read(strings.NewReader(""))
	if err := Configure(store, data); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(data); !os.IsNotExist(err) {
		t.Fatal("configuration created data directory", err)
	}
	t.Setenv("DATA_DIR", data)
	t.Setenv("OLLAMA_URL", "http://localhost:12345")
	config, _ = Load("")
	if config.Hybrid.URL != "http://localhost:12345" || config.Hybrid.CachePath != filepath.Join(data, "rag-vectors.json") {
		t.Fatal(config)
	}
	t.Setenv("RAG_EMBEDDING_URL", "http://127.0.0.1:11435")
	t.Setenv("RAG_EMBEDDING_MODEL", "custom-embedding:latest")
	t.Setenv("RAG_EMBEDDING_CACHE", filepath.Join(data, "explicit.json"))
	config, _ = Load("")
	if config.Hybrid.URL != "http://127.0.0.1:11435" || config.Hybrid.Model != "custom-embedding:latest" || config.Hybrid.CachePath != filepath.Join(data, "explicit.json") {
		t.Fatal(config)
	}
	t.Setenv("RAG_HYBRID_ENABLED", "false")
	store, _ = rag.Read(strings.NewReader(""))
	if err := Configure(store, data); err != nil || store.HybridStatus().Configured {
		t.Fatal("disabled configuration activated index", err)
	}
	t.Setenv("RAG_HYBRID_ENABLED", "typo")
	if _, err := Load(data); err == nil {
		t.Fatal("invalid enabled flag accepted")
	}
	t.Setenv("RAG_HYBRID_ENABLED", "true")
	t.Setenv("RAG_EMBEDDING_URL", "https://example.invalid")
	if err := Configure(store, data); err == nil {
		t.Fatal("nonlocal embedding endpoint accepted")
	}
}

func TestPublicIndexStatusOmitsPrivateInventoryAndPaths(t *testing.T) {
	encoded, _ := json.Marshal(Status(privateStatus{}))
	for _, private := range []string{"count", "total", "private", "secret-cache"} {
		if strings.Contains(string(encoded), private) {
			t.Fatal("private index data exposed", string(encoded))
		}
	}
	if !strings.Contains(string(encoded), "embedding_model_missing") || !strings.Contains(string(encoded), "embeddinggemma") {
		t.Fatal(string(encoded))
	}
}

func TestPreparationStartsAsynchronouslyAndCancelsWithoutSocket(t *testing.T) {
	store, _ := rag.Read(strings.NewReader(`{"id":"one","text":"Питание корабля.","agents":["savinykh"],"source":{"id":"manual","title":"Руководство"}}`))
	entered := make(chan struct{})
	client := &http.Client{Transport: testTransport(func(r *http.Request) (*http.Response, error) {
		if _, ok := r.Context().Deadline(); !ok {
			t.Error("background batch not bounded")
		}
		close(entered)
		<-r.Context().Done()
		return nil, r.Context().Err()
	})}
	if err := store.WithHybrid(rag.HybridConfig{URL: "http://127.0.0.1:11434", Model: "embeddinggemma", HTTPClient: client}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := PrepareAsync(ctx, store)
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("background preparation not started")
	}
	select {
	case <-done:
		t.Fatal("blocked embedding unexpectedly completed")
	default:
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancellation lost")
		}
	case <-time.After(time.Second):
		t.Fatal("background preparation ignored shutdown")
	}
}
