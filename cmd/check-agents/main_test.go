package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"feeders/internal/rag"
)

type diagnosticTransport func(*http.Request) (*http.Response, error)

func (f diagnosticTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func diagnosticEnvironment(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	data, corpus := filepath.Join(root, "data"), filepath.Join(root, "articles.jsonl")
	text := `{"id":"public","text":"Питание корабля обеспечивают батареи.","agents":["savinykh"],"source":{"id":"manual","title":"Руководство"}}
{"id":"private","text":"Питание: ЗАКРЫТАЯ_УЛИКА.","requires_facts":["locked"],"agents":["savinykh"],"source":{"id":"manual","title":"Руководство"}}`
	if err := os.WriteFile(corpus, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	for key, value := range map[string]string{"DATA_DIR": data, "RAG_CHUNKS_FILE": corpus, "OLLAMA_ENABLED": "false", "OLLAMA_URL": "http://127.0.0.1:11434", "OLLAMA_MODEL": "test-chat", "RAG_HYBRID_ENABLED": "false", "RAG_EMBEDDING_URL": "", "RAG_EMBEDDING_MODEL": "embeddinggemma", "RAG_EMBEDDING_CACHE": ""} {
		t.Setenv(key, value)
	}
	return data, corpus
}

func TestDiagnosticHelpValidationStatusAndSearchDoNotCreateGameData(t *testing.T) {
	data, _ := diagnosticEnvironment(t)
	for _, tc := range []struct {
		args []string
		code int
	}{
		{[]string{"-h"}, 0}, {[]string{"-mode", "unknown"}, 2}, {[]string{"-agent", "unknown"}, 2}, {[]string{"-status", "-prepare"}, 2}, {[]string{"-mode", "casual", "-search"}, 2},
		{[]string{"-status"}, 1}, {[]string{"-mode", "casual", "-agent", "savinykh", "-question", "Привет"}, 1}, {[]string{"-prepare"}, 1},
	} {
		var out, diagnostics bytes.Buffer
		if code := run(tc.args, &out, &diagnostics); code != tc.code {
			t.Fatal(tc.args, code, out.String(), diagnostics.String())
		}
		if _, err := os.Stat(data); !os.IsNotExist(err) {
			t.Fatal("read-only diagnostics created game data", tc.args, err)
		}
	}
	var out, diagnostics bytes.Buffer
	if code := run([]string{"-search", "-agent", "savinykh", "-question", "Питание"}, &out, &diagnostics); code != 0 {
		t.Fatal(code, diagnostics.String())
	}
	if !strings.Contains(out.String(), `"mode":"bm25"`) || !strings.Contains(out.String(), `"id":"public"`) || strings.Contains(out.String(), "ЗАКРЫТАЯ_УЛИКА") {
		t.Fatal("search lost provenance or permissions", out.String())
	}
	if _, err := os.Stat(data); !os.IsNotExist(err) {
		t.Fatal("search created game data", err)
	}
}

func TestDiagnosticPrepareReusesRealValidatedCacheWithoutAccounts(t *testing.T) {
	data, corpus := diagnosticEnvironment(t)
	t.Setenv("RAG_HYBRID_ENABLED", "true")
	store, err := rag.Load(corpus)
	if err != nil {
		t.Fatal(err)
	}
	transport := diagnosticTransport(func(request *http.Request) (*http.Response, error) {
		var input struct {
			Input []string `json:"input"`
		}
		if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
			t.Fatal(err)
		}
		vectors := make([][]float64, len(input.Input))
		for i := range vectors {
			vectors[i] = []float64{1, 0}
		}
		body, _ := json.Marshal(map[string]any{"embeddings": vectors})
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(body)), Header: make(http.Header)}, nil
	})
	cache := filepath.Join(data, "rag-vectors.json")
	if err := store.WithHybrid(rag.HybridConfig{URL: "http://127.0.0.1:11434", Model: "embeddinggemma", CachePath: cache, HTTPClient: &http.Client{Transport: transport}}); err != nil {
		t.Fatal(err)
	}
	if err := store.Prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(cache)
	if err != nil {
		t.Fatal(err)
	}
	var out, diagnostics bytes.Buffer
	if code := run([]string{"-prepare"}, &out, &diagnostics); code != 0 {
		t.Fatal(code, out.String(), diagnostics.String())
	}
	if !strings.Contains(out.String(), `"ready":true`) || !strings.Contains(out.String(), `"operation":"prepare"`) {
		t.Fatal(out.String())
	}
	for _, private := range []string{"total", "count", corpus, cache} {
		if strings.Contains(out.String(), private) {
			t.Fatal("diagnostic public metadata disclosed private inventory", out.String())
		}
	}
	after, _ := os.ReadFile(cache)
	if !bytes.Equal(before, after) {
		t.Fatal("valid cached preparation needlessly rewrote cache")
	}
	files, err := os.ReadDir(data)
	if err != nil || len(files) != 1 || files[0].Name() != "rag-vectors.json" {
		t.Fatal("diagnostics created accounts or mission state", files, err)
	}
	// A one-shot preparation must not claim success if computed vectors will
	// disappear at process exit because the cache could not be written.
	unwritableParent := filepath.Join(t.TempDir(), "ordinary-file")
	if err := os.WriteFile(unwritableParent, []byte("file"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := store.WithHybrid(rag.HybridConfig{URL: "http://127.0.0.1:11434", Model: "embeddinggemma", CachePath: filepath.Join(unwritableParent, "cache.json"), HTTPClient: &http.Client{Transport: transport}}); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	diagnostics.Reset()
	if code := prepareArticles(store, &out, &diagnostics); code != 1 || !strings.Contains(diagnostics.String(), "RAG_EMBEDDING_CACHE") || strings.Contains(diagnostics.String(), "ollama pull") {
		t.Fatal("cache failure was presented as successful durable preparation", code, out.String(), diagnostics.String())
	}
}
