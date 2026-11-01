package agents

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
)

func TestKnowledgeRetrieval(t *testing.T) {
	// 1. Поиск для инженера
	art, found := FindKnowledge("engineer", "реактор охлаждение")
	if !found {
		t.Fatal("expected article to be found for engineer")
	}
	if art.AgentID != "engineer" {
		t.Fatalf("expected engineer article, got %s", art.AgentID)
	}

	// 2. Поиск для биолога
	artBio, foundBio := FindKnowledge("biologist", "растения хлорелла кислород")
	if !foundBio {
		t.Fatal("expected article to be found for biologist")
	}
	if artBio.AgentID != "biologist" {
		t.Fatalf("expected biologist article, got %s", artBio.AgentID)
	}

	// 3. Капитан видит темы энергобаланса
	artCap, foundCap := FindKnowledge("captain", "дилемма 20 МВт")
	if !foundCap {
		t.Fatal("expected article to be found for captain")
	}
	if artCap.ID != "cap-dilemma-1" {
		t.Fatalf("expected cap-dilemma-1, got %s", artCap.ID)
	}

	// 4. АСТРА видит расследование аварии
	artAstra, foundAstra := FindKnowledge("astra", "авария метеорит")
	if !foundAstra {
		t.Fatal("expected article to be found for astra")
	}
	if artAstra.AgentID != "astra" {
		t.Fatalf("expected astra article, got %s", artAstra.AgentID)
	}
}

func TestBuildPromptAndPersona(t *testing.T) {
	persona := Personas["astra"]
	art, _ := FindKnowledge("astra", "авария")
	prompt := BuildPrompt(persona, art, "Почему отключена оранжерея?", []string{"Пробуждение", "Статус"})
	if len(prompt) == 0 {
		t.Fatal("empty prompt generated")
	}
}

func TestOrchestratorOfflineAndConcurrency(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	orch := NewOrchestrator("http://127.0.0.1:54321", "test-model", log)

	ctx := context.Background()

	// 1. Проверяем оффлайн-генерацию с карточкой цитаты
	reply, err := orch.Talk(ctx, Request{
		PlayerID: "p1",
		AgentID:  "astra",
		Text:     "Доложи обстановку",
	})
	if err != nil {
		t.Fatalf("orchestrator talk failed: %v", err)
	}
	if reply.AgentID != "astra" || len(reply.Text) == 0 {
		t.Fatalf("invalid reply: %+v", reply)
	}
	if reply.Citation == nil || len(reply.Citation.SourceTitle) == 0 {
		t.Fatalf("expected citation, got nil")
	}

	// 2. Параллельные запросы (проверка отсутствия race conditions)
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, _ = orch.Talk(ctx, Request{
				PlayerID: "p1",
				AgentID:  "engineer",
				Text:     "Сколько нужно энергии оранжерее?",
			})
		}(i)
	}
	wg.Wait()
}
