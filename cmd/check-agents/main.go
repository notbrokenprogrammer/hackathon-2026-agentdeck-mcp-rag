// Run against the installed local Ollama, through the same RAG/persona client
// as the game. Does not create accounts, change mission data or require a port.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"feeders/internal/crew"
	"feeders/internal/knowledge"
	"feeders/internal/npc"
	"feeders/internal/rag"
	"feeders/internal/retrievalconfig"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, out, diagnostics io.Writer) int {
	flags := flag.NewFlagSet("check-agents", flag.ContinueOnError)
	flags.SetOutput(diagnostics)
	statusOnly := flags.Bool("status", false, "Проверить службу и наличие выбранной модели без генерации")
	agentID := flags.String("agent", "", "Проверить только savinykh, dzhanibekov, ryazansky или krikalev")
	question := flags.String("question", "", "Задать свой вопрос вместо приветствия и проверочного вопроса")
	mode := flags.String("mode", npc.DialogueArticles, "Режим: articles или casual")
	prepare := flags.Bool("prepare", false, "Подготовить и сохранить векторный индекс без генерации")
	search := flags.Bool("search", false, "Проверить поиск статей без генерации ответа")
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 || (*mode != npc.DialogueArticles && *mode != npc.DialogueCasual) || (*statusOnly && (*prepare || *search)) || (*search && *mode == npc.DialogueCasual) {
		fmt.Fprintln(diagnostics, "Используйте -mode articles|casual; -status отдельно; -search только для articles.")
		return 2
	}
	if *agentID != "" {
		if _, ok := crew.Find(*agentID); !ok {
			fmt.Fprintln(diagnostics, "Неизвестный персонаж:", *agentID)
			return 2
		}
	}
	model := os.Getenv("OLLAMA_MODEL")
	if model == "" {
		model = "qwen3:4b-instruct-2507-q4_K_M"
	}
	url := os.Getenv("OLLAMA_URL")
	if url == "" {
		url = "http://127.0.0.1:11434"
	}
	store, err := rag.Load(retrievalconfig.Env("RAG_CHUNKS_FILE", "knowledge"))
	if err != nil {
		fmt.Fprintln(diagnostics, "Не удалось открыть knowledge; запускайте команду из корня проекта:", err)
		return 1
	}
	if err := retrievalconfig.Configure(store, ""); err != nil {
		fmt.Fprintln(diagnostics, err)
		return 1
	}
	write := func(value any) { _ = json.NewEncoder(out).Encode(value) }
	if *prepare {
		if code := prepareArticles(store, out, diagnostics); code != 0 {
			return code
		}
		if !*search {
			return 0
		}
	}
	if *search {
		return searchArticles(store, *agentID, *question, out, diagnostics)
	}
	local, err := npc.NewLocal(knowledge.Catalog{Store: store}, url, model)
	if err != nil {
		fmt.Fprintln(diagnostics, err)
		return 1
	}
	if os.Getenv("OLLAMA_ENABLED") == "false" {
		local.WithoutModel()
	}
	status := local.Status(context.Background())
	write(struct {
		npc.ModelStatus
		Retrieval retrievalconfig.PublicStatus `json:"retrieval"`
	}{status, retrievalconfig.Status(store)})
	if !status.Ready {
		return 1
	}
	if *statusOnly {
		return 0
	}
	failed := false
	for _, tc := range []struct{ id, q string }{{"savinykh", "Что произошло на станции Салют-7 в 1985 году?"}, {"dzhanibekov", "Как Джанибеков проверял ручное управление при отказе автоматики?"}, {"ryazansky", "Где Рязанский изучал биохимию и работал до космических полётов?"}, {"krikalev", "Как Крикалёв поддерживал любительскую радиосвязь со школьниками?"}} {
		if *agentID != "" && tc.id != *agentID {
			continue
		}
		questions := []string{tc.q}
		if *mode == npc.DialogueCasual {
			questions = []string{"Привет!", "Что тебе нравится в космосе?"}
		}
		if *question != "" {
			questions = []string{*question}
		}
		for _, q := range questions {
			at := time.Now()
			answer, err := local.Talk(context.Background(), npc.Request{Scope: "local-model-check", PlayerID: "diagnostic", AgentID: tc.id, Text: q, DialogueMode: *mode})
			result := map[string]any{"agent": tc.id, "question": q, "model": model, "mode": answer.Mode, "dialogue_mode": *mode, "status": answer.Status, "retrieval": answer.Retrieval, "grounding": answer.Grounding, "elapsed_ms": time.Since(at).Milliseconds(), "evidence_count": len(answer.Evidence), "text": answer.Text, "fallback_reason": answer.FallbackReason}
			if err != nil {
				result["error"] = err.Error()
			}
			if err != nil || answer.Mode != "model" {
				failed = true
			}
			if *mode == npc.DialogueArticles && store.HybridStatus().Configured && (answer.Retrieval == nil || answer.Retrieval.Mode != "hybrid") {
				failed = true
			}
			write(result)
		}
	}
	if failed {
		fmt.Fprintln(diagnostics, "Проверка неполная: смотрите fallback_reason, status и retrieval. Справочный ответ не считается генерацией; BM25 fallback не считается гибридным поиском.")
		return 1
	}

	return 0
}
