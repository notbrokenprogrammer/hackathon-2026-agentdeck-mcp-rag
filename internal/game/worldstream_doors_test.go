package game

import (
	"feeders/internal/model"
	"math"
	"path/filepath"
	"testing"
)

// Requiring only the occupied room hides holes immediately beyond an open door.
// Every plan must also retain the complete shell of all directly joined areas.
func TestWorldDoorApproachCoverage(t *testing.T) {
	m, store := roomFixture(t, t.TempDir())
	defer store.Close()
	if err := m.WithWorld(filepath.Join("..", "..", "world"), nil); err != nil {
		t.Fatal(err)
	}
	w := m.streamWorld
	check := func(t *testing.T, p model.Position) WorldPlan {
		t.Helper()
		plan := m.planAt(p)
		occupied, adjoining := map[string]bool{}, map[string]bool{}
		for _, area := range deck.Areas {
			if deckContains(area, p.X, p.Z) {
				occupied[area.ID], adjoining[area.ID] = true, true
			}
		}
		for _, gate := range deck.Gates {
			for _, area := range gate.Areas {
				if occupied[area] {
					for _, next := range gate.Areas {
						adjoining[next] = true
					}
				}
			}
		}
		objects, resources := map[string]bool{}, map[string]bool{}
		bytes := 0
		for _, r := range plan.Resources {
			if resources[r.ID] {
				t.Fatal("resource counted twice", r.ID)
			}
			resources[r.ID] = true
			bytes += r.Size
		}
		if bytes > plan.Budget || bytes > int(float64(w.Bytes)*worldLimitRatio) {
			t.Fatalf("door plan exceeded budget: %d/%d", bytes, plan.Budget)
		}
		for _, lod := range append([]WorldLOD{plan.Background}, plan.Chunks...) {
			objects[lod.Object] = true
			for _, id := range lod.Deps {
				if !resources[id] {
					t.Fatal("missing dependency", id)
				}
			}
		}
		for _, c := range w.Chunks {
			for _, area := range c.Areas {
				required := adjoining[area] && (c.Role == "shell" || c.Role == "ceiling" || c.Role == "equipment" || c.NPC != "")
				if required && !objects[c.Low.Object] && !objects[c.High.Object] {
					t.Fatalf("missing %s/%s in %s while approaching door at %.2f,%.2f", c.Role, c.NPC, area, p.X, p.Z)
				}
			}
		}
		return plan
	}
	for _, gate := range deck.Gates {
		t.Run(gate.ID, func(t *testing.T) {
			var previous map[string]bool
			for _, offset := range []float64{-.8, -.3, 0, .3, .8} {
				p := model.Position{X: gate.X + math.Cos(gate.Rotation)*offset, Y: 1.7, Z: gate.Z - math.Sin(gate.Rotation)*offset}
				if !deckWalkable(p.X, p.Z) {
					t.Fatal("door approach is not walkable", p)
				}
				plan := check(t, p)
				objects := map[string]bool{}
				for _, lod := range plan.Chunks {
					objects[lod.Object] = true
				}
				if previous != nil {
					for _, c := range w.Chunks {
						if c.Role != "shell" && c.Role != "ceiling" {
							continue
						}
						for _, area := range c.Areas {
							for _, side := range gate.Areas {
								if area == side && !(previous[c.Low.Object] && objects[c.Low.Object]) && !(previous[c.High.Object] && objects[c.High.Object]) {
									t.Fatalf("resident %s in %s would be dropped during door crossing at offset %.2f", c.Role, area, offset)
								}
							}
						}
					}
				}
				previous = objects
			}
		})
	}
}
