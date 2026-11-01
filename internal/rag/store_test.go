package rag

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(id, agent, text string) string {
	return JSON(Chunk{ID: id, Agents: []string{agent}, Text: text, Source: Source{ID: id, Title: "Тестовый источник"}})
}
func TestScopeSearchAndSources(t *testing.T) {
	s, err := Read(strings.NewReader(fixture("power", "dzhanibekov", "Питание батареи и резерв. Ёмкость.") + "\n" + fixture("plants", "ryazansky", "Растения питание и свет.")))
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.Search(context.Background(), "dzhanibekov", "питание емкость", "", 5)
	if err != nil || r.Status != "ok" || len(r.Hits) != 1 || r.Hits[0].ID != "power" {
		t.Fatalf("%+v %v", r, err)
	}
	if _, err = s.Get("dzhanibekov", "plants"); err == nil {
		t.Fatal("cross-role read allowed")
	}
	if len(s.Sources("dzhanibekov")) != 1 {
		t.Fatal("source scope")
	}
	r, _ = s.Search(context.Background(), "dzhanibekov", "отсутствует", "", 5)
	if r.Status != "no_matches" {
		t.Fatal(r)
	}
	r, _ = s.Search(context.Background(), "krikalev", "питание", "", 5)
	if r.Status != "empty_corpus" {
		t.Fatal(r)
	}
	// Returned slices must not mutate the immutable store.
	c, _ := s.Get("dzhanibekov", "power")
	c.Agents[0] = "ryazansky"
	if _, err = s.Get("ryazansky", "power"); err == nil {
		t.Fatal("store was mutated")
	}
}
func TestValidationAndEmpty(t *testing.T) {
	good := fixture("a", "dzhanibekov", "Батареи")
	for _, input := range []string{`{"id":"a"}`, good + "\n" + good, fixture("a", "unknown", "text"), fixture("a", "dzhanibekov", strings.Repeat("a", MaxChunkBytes+1)), good + "\n{"} {
		if _, err := Read(strings.NewReader(input)); err == nil {
			t.Fatal("accepted invalid corpus", input[:min(len(input), 60)])
		}
	}
	s, err := Load(filepath.Join(t.TempDir(), "missing.jsonl"))
	if err != nil || s.Total() != 0 {
		t.Fatal(s, err)
	}
	path := filepath.Join(t.TempDir(), "bad.jsonl")
	if err := os.WriteFile(path, []byte("not json"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = Load(path); err == nil {
		t.Fatal("corrupt file hidden")
	}
}
func TestSharedChunksTopicLimitAndCancellation(t *testing.T) {
	c := Chunk{ID: "shared", Agents: []string{"dzhanibekov", "savinykh"}, Text: "Резерв питания", Topics: []string{"power"}, Source: Source{ID: "s", Title: "Тест"}}
	s, err := Read(strings.NewReader(JSON(c)))
	if err != nil {
		t.Fatal(err)
	}
	for _, agent := range c.Agents {
		if _, err = s.Get(agent, c.ID); err != nil {
			t.Fatal(err)
		}
	}
	r, _ := s.Search(context.Background(), "dzhanibekov", "резерв", "biology", 5)
	if len(r.Hits) != 0 {
		t.Fatal(r)
	}
	if _, err = s.Search(context.Background(), "dzhanibekov", "резерв", "", 11); err == nil {
		t.Fatal("limit")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = s.Search(ctx, "dzhanibekov", "резерв", "", 5); err == nil {
		t.Fatal("cancellation")
	}
}
