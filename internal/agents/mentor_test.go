package agents

import (
	"context"
	"strings"
	"testing"
)

func TestMentorSourcesMemoryAndIsolation(t *testing.T) {
	dir := t.TempDir()
	m, err := NewMentor(dir)
	if err != nil {
		t.Fatal(err)
	}
	r := Request{PlayerID: "one", AgentID: "savinykh", Text: "Как восстанавливали питание и аккумуляторы Салюта-7?"}
	a, err := m.Talk(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	if a.Citation == nil || !strings.HasPrefix(a.Citation.SourceURL, "https://") || a.Citation.ContentMode == "" {
		t.Fatalf("missing traceable source: %+v", a)
	}
	b, err := m.Talk(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	if a.Text == b.Text {
		t.Fatal("repeat did not adapt")
	}
	r.PlayerID = "two"
	c, err := m.Talk(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	if c.Text != a.Text {
		t.Fatal("another player's first response depends on prior player")
	}
	m2, err := NewMentor(dir)
	if err != nil {
		t.Fatal(err)
	}
	if m2.memory["one"].Turns != 2 || m2.memory["two"].Turns != 1 {
		t.Fatal("memory did not survive reopen")
	}
	r.PlayerID = "one"
	r.Text = "Объясни проще"
	d, err := m2.Talk(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	if d.Citation == nil || d.Citation.DocumentID != b.Citation.DocumentID {
		t.Fatal("follow-up lost previous source")
	}
	m2.Observe("one", "repair_cooling")
	r.Text = "Что дальше?"
	d, err = m2.Talk(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(d.Text, "ремонт охлаждения") {
		t.Fatal("actions not taken into account")
	}
}

func TestMentorDeclinesUnknownAndRespectsCancellation(t *testing.T) {
	m, err := NewMentor(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r := Request{PlayerID: "one", AgentID: "savinykh", Text: "рецепт борща"}
	a, err := m.Talk(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	if a.Citation != nil || !strings.Contains(a.Text, "нет надёжного ответа") {
		t.Fatalf("unsupported claim: %+v", a)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = m.Talk(ctx, r); err == nil {
		t.Fatal("cancellation ignored")
	}
	if m.memory["one"].Turns != 0 {
		t.Fatal("canceled request persisted")
	}
}
