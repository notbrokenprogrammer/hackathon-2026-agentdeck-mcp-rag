package game

import (
	"encoding/json"
	"feeders/internal/model"
	"os"
	"path/filepath"
	"testing"
)

// Check completeness as well as the budget: an empty scene also fits a limit.
func TestReferenceWorldCoverage(t *testing.T) {
	m, store := roomFixture(t, t.TempDir())
	defer store.Close()
	if err := m.WithWorld(filepath.Join("..", "..", "world"), nil); err != nil {
		t.Fatal(err)
	}
	w := m.streamWorld
	views := []struct {
		Name string
		X, Z float64
	}{
		{"bridge", 3.3, 3.7}, {"window", 6, -3}, {"window-angle", 2.5, -1.5},
		{"cryo", -16, 2}, {"reactor", 13, 3}, {"bio", -18, -13.5},
		{"corridor", -10, 9}, {"research", 0, 12.5}, {"cargo", 15.5, 12}, {"habitat", -15, 12.5},
		{"bridge-alt", -3, 3}, {"cryo-alt", -20.5, 1.5}, {"reactor-alt", 21, 2.5}, {"bio-alt", -11.5, -21},
		{"corridor-alt", 10, 9}, {"research-alt", 7, 13}, {"cargo-alt", 13, 19.5}, {"habitat-alt", -9.5, 19},
	}
	output := map[string]WorldPlan{}
	used := map[string]bool{}
	for _, view := range views {
		if !deckWalkable(view.X, view.Z) {
			t.Fatal("review camera overlaps a wall or equipment", view.Name)
		}
		output[view.Name] = m.planAt(model.Position{X: view.X, Y: 1.7, Z: view.Z})
	}
	for x := -21.; x <= 22; x += 2 {
		for z := -22.; z <= 20; z += 2 {
			if !deckWalkable(x, z) {
				continue
			}
			plan := m.planAt(model.Position{X: x, Y: 1.7, Z: z})
			for _, r := range plan.Resources {
				used[r.ID] = true
			}
			objects := map[string]bool{}
			for _, lod := range plan.Chunks {
				objects[lod.Object] = true
			}
			for _, a := range deck.Areas {
				if !deckContains(a, x, z) {
					continue
				}
				for _, c := range w.Chunks {
					if c.Role != "shell" && c.Role != "ceiling" && c.Role != "equipment" {
						continue
					}
					for _, id := range c.Areas {
						if id == a.ID && !objects[c.Low.Object] && !objects[c.High.Object] {
							t.Fatalf("missing %s in occupied room %s at %.1f,%.1f", c.Role, id, x, z)
						}
					}
				}
			}
		}
	}
	usedBytes := 0
	for id := range used {
		usedBytes += w.byID[id].Size
	}
	t.Logf("route uses %d/%d resource bytes", usedBytes, w.Bytes)
	before := m.worldPosition("view-test")
	for _, area := range deck.Areas {
		plan := m.worldPlanForView("view-test", area.ID)
		if plan.Position.X != area.X || plan.Position.Z != area.Z {
			t.Fatal("overview did not select requested room", area.ID)
		}
	}
	if m.worldPosition("view-test") != before {
		t.Fatal("overview moved player")
	}
	if m.worldPlanForView("view-test", "unknown").Position != before {
		t.Fatal("unbounded overview target accepted")
	}
	for _, p := range []struct {
		ID   string
		X, Z float64
	}{{"savinykh", 3, -1}, {"krikalev", -6.7, -.5}, {"dzhanibekov", 13, 3}, {"ryazansky", -19, -14}} {
		plan := m.planAt(model.Position{X: p.X, Y: 1.7, Z: p.Z})
		found := false
		for _, c := range w.Chunks {
			if c.NPC == p.ID {
				for _, lod := range plan.Chunks {
					if lod.Object == c.Low.Object || lod.Object == c.High.Object {
						found = true
					}
				}
			}
		}
		if !found {
			t.Errorf("nearby mentor %s missing from AOI", p.ID)
		}
	}
	if destination := os.Getenv("LASTCREW_REVIEW_PLANS"); destination != "" {
		b, err := json.MarshalIndent(output, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(destination, b, 0644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRealWorldBudgetsAcrossAllCells(t *testing.T) {
	m, store := roomFixture(t, t.TempDir())
	defer store.Close()
	if err := m.WithWorld(filepath.Join("..", "..", "world"), nil); err != nil {
		t.Fatal(err)
	}
	w := m.streamWorld
	if w.Bytes <= 0 || len(w.Chunks) < 4 {
		t.Fatal("empty world")
	}
	maxBytes := 0
	for x := -24.; x <= 24; x += 2 {
		for z := -24.; z <= 8; z += 2 {
			plan := m.planAt(model.Position{X: x, Y: 1.7, Z: z})
			seen := map[string]bool{}
			bytes := 0
			for _, r := range plan.Resources {
				if seen[r.ID] {
					t.Fatal("dependency counted twice")
				}
				seen[r.ID] = true
				bytes += r.Size
			}
			if bytes > plan.Budget || float64(bytes) > float64(w.Bytes)*worldLimitRatio {
				t.Fatalf("budget exceeded at %g,%g: %d/%d", x, z, bytes, w.Bytes)
			}
			for _, chunk := range plan.Chunks {
				for _, id := range chunk.Deps {
					if !seen[id] {
						t.Fatal("missing admitted dependency", id)
					}
				}
			}
			if bytes > maxBytes {
				maxBytes = bytes
			}
		}
	}
	t.Logf("real world %d bytes; peak plan %d bytes (%.3f%%)", w.Bytes, maxBytes, float64(maxBytes)/float64(w.Bytes)*100)
}

func TestMentorsPrefetchedBeforeLeavingCryo(t *testing.T) {
	m, store := roomFixture(t, t.TempDir())
	defer store.Close()
	if err := m.WithWorld(filepath.Join("..", "..", "world"), nil); err != nil {
		t.Fatal(err)
	}
	plan := m.planAt(model.Position{X: -16, Y: 1.7, Z: 2})
	objects := map[string]bool{}
	for _, lod := range plan.Chunks {
		objects[lod.Object] = true
	}
	mentors := 0
	for _, c := range m.streamWorld.Chunks {
		if c.NPC != "" {
			mentors++
			if !objects[c.Low.Object] && !objects[c.High.Object] {
				t.Errorf("mentor %s is not prefetched while the player is still in cryo", c.NPC)
			}
		}
	}
	if mentors != 4 {
		t.Fatalf("expected four mentors, got %d", mentors)
	}
}
