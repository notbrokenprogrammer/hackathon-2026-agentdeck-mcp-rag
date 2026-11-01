package knowledge

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"feeders/internal/mission"
	"feeders/internal/rag"
)

func TestRealScenarioCorpusAndNoSpoilers(t *testing.T) {
	s, err := rag.Load("../../knowledge")
	if err != nil {
		t.Fatal(err)
	}
	c := Catalog{Store: s}
	value, err := c.Call(context.Background(), "savinykh_search", json.RawMessage(`{"query":"комбинированный план","topic":"rescue"}`))
	if err != nil {
		t.Fatal(err)
	}
	found := value.(rag.SearchResult)
	if len(found.Hits) == 0 || found.Hits[0].ID != "scenario-combined-plan" {
		t.Fatal(found)
	}
	if _, err = c.Call(context.Background(), "savinykh_read_chunk", json.RawMessage(`{"chunk_id":"scenario-confirmed-cause"}`)); err == nil {
		t.Fatal("unbound MCP leaked secret")
	}
	svc, _ := mission.New("")
	cap, _ := svc.Create(context.Background(), "cap", 4, nil)
	_, _ = svc.Join(context.Background(), "engineer", mission.Engineer)
	ctx := mission.WithView(context.Background(), cap)
	for _, tool := range []string{"dzhanibekov_read_chunk", "savinykh_read_chunk"} {
		if _, err = c.Call(ctx, tool, json.RawMessage(`{"chunk_id":"scenario-engineer"}`)); err == nil {
			t.Fatal("captain obtained engineer's private initial fact")
		}
	}
	if _, err = c.Call(ctx, "savinykh_read_chunk", json.RawMessage(`{"chunk_id":"scenario-captain"}`)); err != nil {
		t.Fatal(err)
	}
	eng, _ := svc.View("engineer")
	ctx = mission.WithView(context.Background(), eng)
	if _, err = c.Call(ctx, "dzhanibekov_read_chunk", json.RawMessage(`{"chunk_id":"scenario-engineer"}`)); err != nil {
		t.Fatal(err)
	}
	for _, tool := range c.List() {
		if tool.Annotations["readOnlyHint"] != true {
			t.Fatal("write tool exposed", tool.Name)
		}
	}
	if len(c.List()) != 40 {
		t.Fatal(len(c.List()))
	}
}
func TestScenarioToolsUseTrustedViewAndNeverMutate(t *testing.T) {
	s, _ := rag.Read(strings.NewReader(""))
	c := Catalog{Store: s}
	if _, err := c.Call(context.Background(), "savinykh_rescue_plans", nil); err == nil {
		t.Fatal("unbound state accepted")
	}
	state, _ := mission.Initial(nil)
	v := mission.View{State: state, Roles: []string{mission.Captain}, Players: 4}
	ctx := mission.WithView(context.Background(), v)
	if _, err := c.Call(ctx, "savinykh_rescue_plans", json.RawMessage(`{"state":{"energy":100}}`)); err == nil {
		t.Fatal("model replaced authoritative state")
	}
	value, err := c.Call(ctx, "savinykh_preview_action", json.RawMessage(`{"name":"start_reactor"}`))
	if err != nil {
		t.Fatal(err)
	}
	f := value.(mission.Forecast)
	if !f.Allowed || f.After.O != 56 || v.State.Cycle != 0 {
		t.Fatal(f)
	}
	if _, err = c.Call(ctx, "krikalev_compare_navigation", nil); err == nil {
		t.Fatal("hidden navigation answer")
	}
	if _, err = c.Call(ctx, "dzhanibekov_drone_route", json.RawMessage(`{"path":["А","В","Г"]}`)); err == nil {
		t.Fatal("hidden drone answer")
	}
}
