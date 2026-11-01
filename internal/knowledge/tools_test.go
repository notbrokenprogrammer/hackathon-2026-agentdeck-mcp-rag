package knowledge

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"feeders/internal/crew"
	"feeders/internal/rag"
)

func TestEveryRoleHasValidatedTools(t *testing.T) {
	s, _ := rag.Read(strings.NewReader(""))
	c := Catalog{Store: s}
	ctx := context.Background()
	for _, p := range crew.All() {
		if len(c.ForAgent(p.ID)) < 6 {
			t.Fatal(p.ID)
		}
		v, err := c.Call(ctx, p.ID+"_search", json.RawMessage(`{"query":"материалы"}`))
		if err != nil || v.(rag.SearchResult).Status != "empty_corpus" {
			t.Fatal(v, err)
		}
		for _, args := range []string{`{}`, `null`, `[]`, `{"query":null}`, `{"query":"x","limit":0}`, `{"query":"x","limit":1.5}`, `{"query":"x","agent_id":"dzhanibekov"}`} {
			if _, err = c.Call(ctx, p.ID+"_search", json.RawMessage(args)); err == nil {
				t.Fatal("accepted", args)
			}
		}
		if _, err = c.Call(ctx, p.ID+"_read_chunk", json.RawMessage(`{"chunk_id":"missing"}`)); err == nil {
			t.Fatal("missing chunk")
		}
		if _, err = c.Call(ctx, p.ID+"_list_sources", json.RawMessage(`{"offset":0,"limit":20}`)); err != nil {
			t.Fatal(err)
		}
	}
}
