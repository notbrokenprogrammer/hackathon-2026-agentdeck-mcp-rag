// Package retrievalconfig shares local hybrid-search configuration between the
// game host, diagnostic CLI and short-lived MCP processes.
package retrievalconfig

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"feeders/internal/rag"
)

const DefaultModel = "embeddinggemma"
const PreparationTimeout = 10 * time.Minute

type Config struct {
	Enabled bool
	Hybrid  rag.HybridConfig
}

// Load only reads environment variables. Configure may read an existing cache;
// neither function creates directories, downloads models or makes HTTP calls.
func Load(dataDir string) (Config, error) {
	if dataDir == "" {
		dataDir = Env("DATA_DIR", "data")
	}
	enabled, err := strconv.ParseBool(Env("RAG_HYBRID_ENABLED", "true"))
	if err != nil {
		return Config{}, fmt.Errorf("RAG_HYBRID_ENABLED must be true or false")
	}
	return Config{Enabled: enabled, Hybrid: rag.HybridConfig{
		URL:          Env("RAG_EMBEDDING_URL", Env("OLLAMA_URL", "http://127.0.0.1:11434")),
		Model:        Env("RAG_EMBEDDING_MODEL", DefaultModel),
		CachePath:    Env("RAG_EMBEDDING_CACHE", filepath.Join(dataDir, "rag-vectors.json")),
		QueryTimeout: 8 * time.Second,
	}}, nil
}

func Configure(store *rag.Store, dataDir string) error {
	cfg, err := Load(dataDir)
	if err != nil {
		return err
	}
	if !cfg.Enabled {
		return nil
	}
	if err := store.WithHybrid(cfg.Hybrid); err != nil {
		return fmt.Errorf("hybrid retrieval: %w", err)
	}
	return nil
}

// PrepareAsync keeps serving startup independent of model loading/indexing.
// The parent context cancels shutdown work; the finite limit also bounds a
// disconnected or unusually slow local embedding service.
func PrepareAsync(parent context.Context, store *rag.Store) <-chan error {
	done := make(chan error, 1)
	go func() {
		defer close(done)
		ctx, cancel := context.WithTimeout(parent, PreparationTimeout)
		defer cancel()
		done <- store.Prepare(ctx)
	}()
	return done
}

// PublicStatus excludes total/indexed counts because the shared index also
// contains locked mission documents. Readiness means prepared vectors, not
// proof that a future query or a chat generation will succeed.
type PublicStatus struct {
	Configured bool   `json:"configured"`
	Ready      bool   `json:"ready"`
	Code       string `json:"code"`
	Message    string `json:"message"`
	Model      string `json:"model,omitempty"`
}

type StatusSource interface{ HybridStatus() rag.HybridStatus }

func Status(source StatusSource) PublicStatus {
	if source == nil {
		return PublicStatus{Code: "disabled", Message: "Семантический поиск выключен. Доступен поиск по словам (BM25)."}
	}
	value := source.HybridStatus()
	message := "Семантический индекс подготавливается. Пока доступен поиск по словам (BM25)."
	switch value.Code {
	case "disabled":
		message = "Семантический поиск выключен. Доступен поиск по словам (BM25)."
	case "ready":
		message = "Семантический индекс готов. Конкретный поиск ещё требует доступную embedding-модель; результат отмечается отдельно в ответе."
	case "embedding_model_missing":
		message = "Embedding-модель не установлена на хосте. Пока используется поиск по словам (BM25)."
	case "embedding_service_unavailable":
		message = "Служба embeddings недоступна на хосте. Пока используется поиск по словам (BM25)."
	case "embedding_timeout":
		message = "Семантический поиск не успел завершиться. Пока используется поиск по словам (BM25)."
	case "embedding_response_invalid":
		message = "Embedding-модель вернула некорректные векторы. Пока используется поиск по словам (BM25)."
	case "embedding_cache_invalid":
		message = "Кэш векторов требует перестроения. Пока доступен поиск по словам (BM25)."
	case "embedding_cache_unwritable":
		message = "Индекс работает в памяти, но кэш не сохранён. После перезапуска потребуется повторная подготовка."
	}
	return PublicStatus{Configured: value.Configured, Ready: value.Ready, Code: value.Code, Message: message, Model: value.Model}
}

func Env(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
