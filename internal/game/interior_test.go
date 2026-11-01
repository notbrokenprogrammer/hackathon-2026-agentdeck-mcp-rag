package game

import (
	"context"
	"feeders/internal/model"
	"feeders/pkg/protocol"
	"io"
	"log/slog"
	"testing"
	"time"
)

func TestInteriorAllCompartmentsAndDoorGaps(t *testing.T) {
	type point struct{ x, z int }
	q := []point{{-75, 0}}
	seen := map[point]bool{q[0]: true}
	rooms := map[string]bool{}
	for i := 0; i < len(q); i++ {
		p := q[i]
		x, z := float64(p.x)*.2, float64(p.z)*.2
		for _, a := range deck.Areas {
			if deckContains(a, x, z) {
				rooms[a.ID] = true
			}
		}
		for _, d := range []point{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
			n := point{p.x + d.x, p.z + d.z}
			if !seen[n] && deckWalkable(float64(n.x)*.2, float64(n.z)*.2) {
				seen[n] = true
				q = append(q, n)
			}
		}
	}
	for _, a := range deck.Areas {
		if !rooms[a.ID] {
			t.Fatal("unreachable compartment", a.ID)
		}
	}
	if len(seen) != 24289 {
		t.Fatalf("client/server capsule floor differs: %d cells", len(seen))
	}
	for _, o := range architecturalDoors() {
		if !deckWalkable(o.X, o.Z) {
			t.Fatal("obstructed doorway", o.ID)
		}
		if !doorBlocks(o, o.X, o.Z, 1000) {
			t.Fatal("closed door passed", o.ID)
		}
		setDoorTarget(&o, true, 1000)
		if !doorBlocks(o, o.X, o.Z, 1050) {
			t.Fatal("door collision released before gap", o.ID)
		}
		if doorBlocks(o, o.X, o.Z, 1900) {
			t.Fatal("open doorway blocked", o.ID)
		}
	}
}

func TestInteriorServerSweepRejectsWallTunneling(t *testing.T) {
	loop := NewServerLoop(nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	loop.ConfigureWorld(nil, deckWalkable)
	_, err := loop.Join(context.Background(), model.User{ID: "walker"}, model.PlayerState{Position: model.Position{X: 3, Y: 1.7, Z: 6}})
	if err != nil {
		t.Fatal(err)
	}
	// Both endpoints lie on valid floors, but the segment crosses a solid bulkhead.
	if !deckWalkable(3, 8) {
		t.Fatal("test endpoint invalid")
	}
	if loop.Input(context.Background(), "walker", protocol.PlayerInput{Seq: 1, X: 3, Y: 1.7, Z: 8}) == nil {
		t.Fatal("tunneled through wall")
	}
}

func TestInteriorDoorSafetySynchronizationAndPersistence(t *testing.T) {
	dir := t.TempDir()
	m, store := roomFixture(t, dir)
	a := attach(t, m, "alice")
	b := attach(t, m, "boris")
	startUniverseRoom(t, m, a)
	code := m.RoomOf("alice")
	roomCmd(t, m, b, "room.join", map[string]string{"code": code})
	loop := NewServerLoop(store, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	m.world = loop
	for _, id := range []string{"alice", "boris"} {
		_, err := loop.Join(context.Background(), model.User{ID: id}, model.PlayerState{Position: model.Position{X: 0, Y: 1.7, Z: 5.8}})
		if err != nil {
			t.Fatal(err)
		}
	}
	request := func() protocol.Envelope {
		return universeCmd(t, m, a, "universe.interact", map[string]any{"expected_version": m.rooms[code].Universe.Version, "object_id": "door-main", "action": "toggle"})
	}
	if got := request(); got.Type != "universe.state" {
		t.Fatal(string(got.Payload))
	}
	index := 0
	for i, o := range m.rooms[code].Universe.Objects {
		if o.ID == "door-main" {
			index = i
		}
	}
	if !m.universeBlocksMovement("alice", 0, 7.5) {
		t.Fatal("passed still closed leaves")
	}
	universeError(t, request(), "door_moving")
	m.rooms[code].Universe.Objects[index].MotionAt -= 1000
	if m.universeBlocksMovement("alice", 0, 7.5) {
		t.Fatal("open door blocked")
	}
	// Reconnect inside a closed doorway is repaired outside its collision volume.
	closed := m.rooms[code].Universe.Objects[index]
	closed.Open = false
	closed.MotionAt = 0
	m.rooms[code].Universe.Objects[index] = closed
	repaired := m.repairDoorSpawn(model.PlayerState{PlayerID: "alice", Position: model.Position{X: 0, Y: 1.7, Z: 7}})
	if doorBlocks(closed, repaired.Position.X, repaired.Position.Z, time.Now().UnixMilli()) {
		t.Fatal("reconnected player stuck in closed door")
	}
	m.rooms[code].Universe.Objects[index].Open = true
	loop.players["alice"].Position = model.Position{X: 0, Y: 1.7, Z: 5.8}
	// A second player in the doorway prevents the first player from closing it.
	loop.players["boris"].Position.Z = 7
	universeError(t, request(), "door_occupied")
	loop.players["boris"].Position.Z = 9
	if got := request(); got.Type != "universe.state" {
		t.Fatal(string(got.Payload))
	}
	// Entry during closing must reverse the common authoritative state.
	m.rooms[code].Universe.Objects[index].MotionAt = time.Now().UnixMilli() - 100
	m.universeBlocksMovement("boris", 0, 7.6)
	if !m.rooms[code].Universe.Objects[index].Open {
		t.Fatal("closing sensor did not reopen for remote player")
	}
	universeError(t, universeCmd(t, m, a, "universe.admin", map[string]any{"expected_version": m.rooms[code].Universe.Version, "remove_id": "door-main"}), "fixed_architecture")
	saved := m.rooms[code].Universe.Objects[index]
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	restored, reopened := roomFixture(t, dir)
	defer reopened.Close()
	if restored.rooms[code].Universe.Objects[index] != saved {
		t.Fatal("door motion lost on persistence/reconnect")
	}
}

func TestInteriorMigrationPreservesMissionAndObjects(t *testing.T) {
	u := &Universe{Version: 7, Settings: UniverseSettings{AmbientColor: "#123456", NPCCount: 22}, Objects: []UniverseObject{{ID: "door-bridge", Kind: "door", Open: true}, {ID: "custom", Kind: "crate", X: 0, Z: -17}}}
	r := &Room{Universe: u}
	universeOf(r)
	if u.Version != 8 || u.Settings.AmbientColor != "#123456" || !u.Objects[0].Open || u.Objects[1].X != -16 {
		t.Fatal("migration lost customization")
	}
	count := len(u.Objects)
	universeOf(r)
	if len(u.Objects) != count || u.Version != 8 {
		t.Fatal("migration repeated")
	}
}
