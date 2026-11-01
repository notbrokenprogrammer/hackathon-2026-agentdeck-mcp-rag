package npc

import (
	"context"
	"feeders/internal/knowledge"
	"feeders/internal/rag"
	"feeders/internal/storage"
	"path/filepath"
	"testing"
)

func TestAllPersonasMemorySurvivesSQLiteRestart(t *testing.T) {
	store, e := storage.NewSQLiteStore(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer store.Close()
	corpus, e := rag.Load(filepath.Join("..", "..", "knowledge"))
	if e != nil {
		t.Fatal(e)
	}
	makeLocal := func() *Local {
		s, e := NewLocal(knowledge.Catalog{Store: corpus}, "http://127.0.0.1:11434", "test")
		if e != nil {
			t.Fatal(e)
		}
		s.WithoutModel()
		if e = s.WithMemory(store); e != nil {
			t.Fatal(e)
		}
		return s
	}
	one := makeLocal()
	for _, id := range []string{"savinykh", "dzhanibekov", "ryazansky", "krikalev"} {
		r := Request{Scope: "room1", PlayerID: "one", AgentID: id, Text: "Питание и охлаждение"}
		if _, e = one.Talk(context.Background(), r); e != nil {
			t.Fatal(id, e)
		}
	}
	two := makeLocal()
	for _, id := range []string{"savinykh", "dzhanibekov", "ryazansky", "krikalev"} {
		r := Request{Scope: "room1", PlayerID: "one", AgentID: id, Text: "Объясни проще"}
		if q := two.retrievalQuery(r); q == r.Text {
			t.Fatal("topic lost", id)
		}
		r.Scope = "room2"
		if q := two.retrievalQuery(r); q != r.Text {
			t.Fatal("cross-room memory", id)
		}
		r.Scope = "room1"
		r.PlayerID = "other"
		if q := two.retrievalQuery(r); q != r.Text {
			t.Fatal("cross-player memory", id)
		}
	}
}
