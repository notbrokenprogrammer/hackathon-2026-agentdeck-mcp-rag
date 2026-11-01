package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"feeders/internal/knowledge"
	"feeders/internal/rag"
	"feeders/internal/retrievalconfig"
)

func prepareArticles(store *rag.Store, out, diagnostics io.Writer) int {
	at := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), retrievalconfig.PreparationTimeout)
	err := store.Prepare(ctx)
	cancel()
	state := retrievalconfig.Status(store)
	_ = json.NewEncoder(out).Encode(map[string]any{"operation": "prepare", "retrieval": state, "elapsed_ms": time.Since(at).Milliseconds()})
	if state.Code == "embedding_cache_unwritable" {
		fmt.Fprintln(diagnostics, "Индекс вычислен, но кэш не сохранён. Укажите доступный для записи RAG_EMBEDDING_CACHE и повторите -prepare.")
		return 1
	}
	if err != nil || !state.Configured || !state.Ready {
		fmt.Fprintln(diagnostics, "Индекс не подготовлен. Проверьте статус и ollama pull", retrievalconfig.Env("RAG_EMBEDDING_MODEL", retrievalconfig.DefaultModel))
		return 1
	}
	return 0
}

func searchArticles(store *rag.Store, agent, question string, out, diagnostics io.Writer) int {
	catalog := knowledge.Catalog{Store: store}
	failed := false
	for _, tc := range []struct{ id, query string }{
		{"savinykh", "Что произошло на станции Салют-7 в 1985 году?"},
		{"dzhanibekov", "Как Джанибеков проверял ручное управление при отказе автоматики?"},
		{"ryazansky", "Где Рязанский изучал биохимию и работал до космических полётов?"},
		{"krikalev", "Как Крикалёв поддерживал любительскую радиосвязь со школьниками?"},
	} {
		if agent != "" && tc.id != agent {
			continue
		}
		query := tc.query
		if question != "" {
			query = question
		}
		at := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		value, err := catalog.Call(ctx, tc.id+"_search", json.RawMessage(rag.JSON(map[string]any{"query": query, "limit": 3})))
		cancel()
		result := map[string]any{"operation": "search", "agent": tc.id, "question": query, "elapsed_ms": time.Since(at).Milliseconds(), "result": value}
		if err != nil {
			result["error"] = err.Error()
			failed = true
		}
		if found, ok := value.(rag.SearchResult); ok && store.HybridStatus().Configured && found.Retrieval.Mode != "hybrid" {
			failed = true
		}
		_ = json.NewEncoder(out).Encode(result)
	}
	if failed {
		fmt.Fprintln(diagnostics, "Поиск не подтверждён как гибридный: смотрите result.retrieval и error.")
		return 1
	}
	return 0
}
