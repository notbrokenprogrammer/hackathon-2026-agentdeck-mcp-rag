package rag

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAllRAGRoutesRespectFactGates(t *testing.T) {
	c := Chunk{ID: "secret", Text: "микрометеорит", Agents: []string{"savinykh"}, Source: Source{ID: "private-source", Title: "private"}, RequiresFacts: []string{"cause_confirmed"}}
	s, err := Read(strings.NewReader(JSON(c)))
	if err != nil {
		t.Fatal(err)
	}
	if s.Inventory() != 1 || s.Total() != 0 || len(s.Sources("savinykh")) != 0 {
		t.Fatal("metadata leak")
	}
	if _, err = s.Get("savinykh", "secret"); err == nil {
		t.Fatal("direct read bypass")
	}
	r, err := s.Search(context.Background(), "savinykh", "микрометеорит", "", 3)
	if err != nil || len(r.Hits) != 0 {
		t.Fatal(r, err)
	}
	scoped := s.Scope([]string{"cause_confirmed"})
	if _, err = scoped.Get("savinykh", "secret"); err != nil {
		t.Fatal(err)
	}
	if s.Total() != 0 {
		t.Fatal("scope mutated parent")
	}
}
func TestDirectoryMergingDuplicateRejectionAndRussianForms(t *testing.T) {
	dir := t.TempDir()
	a := fixture("a", "savinykh", "Капитан проверяет батарею")
	if err := os.WriteFile(filepath.Join(dir, "a.jsonl"), []byte(a), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.Search(context.Background(), "savinykh", "капитану батареи", "", 3)
	if err != nil || len(r.Hits) != 1 {
		t.Fatal(r, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.jsonl"), []byte(a), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir); err == nil {
		t.Fatal("duplicate across files accepted")
	}
}
