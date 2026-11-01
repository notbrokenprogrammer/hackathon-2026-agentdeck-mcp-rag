package game

import (
	"bytes"
	"encoding/json"
	"net/url"
	"os/exec"
	"path/filepath"
	"testing"
)

// Exercise the frontend's real placement helper against authoritative server
// commands. No network or browser is involved; Node is only a test dependency.
func TestUniverseAdminFrontendSpawnAcceptedInEveryRoom(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node is unavailable: frontend/server spawn integration not run")
	}
	path, err := filepath.Abs("../../web/src/ui/UniversePanel.js")
	if err != nil {
		t.Fatal(err)
	}
	objects, err := json.Marshal(newUniverse().Objects)
	if err != nil {
		t.Fatal(err)
	}
	const script = `
import {readFileSync} from 'node:fs';
const {findSpawnPoint}=await import(process.argv[1]);
const {layout}=await import(new URL('../interior/DeckLayout.js',process.argv[1]));
const objects=JSON.parse(readFileSync(0,'utf8')),points=[];
for(const room of layout.areas.filter(area=>area.kind==='room')) {
  for(const kind of ['door','crate','plant','beacon']) {
    const point=findSpawnPoint(room.id,objects,kind);
    if(!point)throw new Error('No safe spawn for '+room.id+'/'+kind);
    points.push({room:room.id,kind,...point});
  }
}
process.stdout.write(JSON.stringify(points));`
	cmd := exec.Command(node, "--input-type=module", "-e", script, (&url.URL{Scheme: "file", Path: filepath.ToSlash(path)}).String())
	cmd.Stdin = bytes.NewReader(objects)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("frontend placement failed: %v\n%s", err, stderr.String())
	}
	var points []struct {
		Room, Kind string
		X, Y, Z    float64
	}
	if err := json.Unmarshal(output, &points); err != nil {
		t.Fatal(err)
	}
	if len(points) != 28 {
		t.Fatalf("want all seven rooms and four object kinds, got %d", len(points))
	}
	m, store := roomFixture(t, t.TempDir())
	defer store.Close()
	owner := attach(t, m, "spawn-owner")
	startUniverseRoom(t, m, owner)
	room := m.rooms[m.RoomOf(owner.user.ID)]
	for _, point := range points {
		t.Run(point.Room+"/"+point.Kind, func(t *testing.T) {
			inside := false
			for _, area := range deck.Areas {
				if area.ID == point.Room && area.Kind == "room" && deckContains(area, point.X, point.Z) {
					inside = true
				}
			}
			if !inside || !universeFloor(point.X, point.Z) {
				t.Fatalf("frontend selected invalid server floor: %+v", point)
			}
			payload := map[string]any{"expected_version": room.Universe.Version, "spawn": map[string]any{"kind": point.Kind, "x": point.X, "y": point.Y, "z": point.Z}}
			response := universeCmd(t, m, owner, "universe.admin", payload)
			if response.Type != "universe.state" {
				t.Fatalf("server rejected frontend spawn: %s %s", response.Type, response.Payload)
			}
			created := room.Universe.Objects[len(room.Universe.Objects)-1]
			if created.Kind != point.Kind || created.X != point.X || created.Y != point.Y || created.Z != point.Z {
				t.Fatalf("server changed placement: %+v", created)
			}
			response = universeCmd(t, m, owner, "universe.admin", map[string]any{"expected_version": room.Universe.Version, "remove_id": created.ID})
			if response.Type != "universe.state" {
				t.Fatalf("test cleanup rejected: %s %s", response.Type, response.Payload)
			}
		})
	}
}
