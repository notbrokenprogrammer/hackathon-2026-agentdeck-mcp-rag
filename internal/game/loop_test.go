package game

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"feeders/internal/model"
	"feeders/internal/storage"
	"feeders/pkg/protocol"
)

type mockBroadcaster struct {
	lastKind    string
	lastPayload any
	count       int
}

func (m *mockBroadcaster) Broadcast(kind string, payload any) int {
	m.lastKind = kind
	m.lastPayload = payload
	m.count++
	return 1
}

func (m *mockBroadcaster) Publish(playerID, kind string, payload any) bool {
	m.lastKind = kind
	m.lastPayload = payload
	m.count++
	return true
}

func TestServerLoopValidationAndTick(t *testing.T) {
	tempDir := t.TempDir()
	repo, err := storage.NewFileStore(tempDir)
	if err != nil {
		t.Fatal(err)
	}

	b := &mockBroadcaster{}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	loop := NewServerLoop(repo, b, log)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	user := model.User{ID: "usr-1", Username: "Vance"}
	initState := model.PlayerState{
		PlayerID: "usr-1",
		Position: model.Position{X: 0, Y: 1.7, Z: 0},
		Yaw:      0,
	}

	// 1. Join
	state, err := loop.Join(ctx, user, initState)
	if err != nil {
		t.Fatalf("Join failed: %v", err)
	}
	if state.Position.X != 0 {
		t.Fatalf("unexpected state: %+v", state)
	}

	// 2. Valid Input
	err = loop.Input(ctx, "usr-1", protocol.PlayerInput{
		Seq: 1,
		X:   1.0,
		Y:   1.7,
		Z:   1.0,
		Yaw: 0.5,
	})
	if err != nil {
		t.Fatalf("valid input rejected: %v", err)
	}

	// 3. Stale sequence should be ignored without error
	err = loop.Input(ctx, "usr-1", protocol.PlayerInput{
		Seq: 1, // Same seq
		X:   2.0,
		Y:   1.7,
		Z:   2.0,
		Yaw: 0.5,
	})
	if err != nil {
		t.Fatalf("stale input caused error: %v", err)
	}

	// 4. Out of bounds check
	err = loop.Input(ctx, "usr-1", protocol.PlayerInput{
		Seq: 2,
		X:   500.0, // Out of bounds
		Y:   1.7,
		Z:   0.0,
		Yaw: 0,
	})
	if err == nil {
		t.Fatal("expected error for out of bounds coordinates")
	}

	// 5. Massive teleport distance check
	err = loop.Input(ctx, "usr-1", protocol.PlayerInput{
		Seq: 3,
		X:   50.0, // Delta > 30m
		Y:   1.7,
		Z:   0.0,
		Yaw: 0,
	})
	if err == nil {
		t.Fatal("expected error for speed/distance violation")
	}

	// 6. Test tick snapshot generation
	loop.tick()
	if b.count == 0 || b.lastKind != "world.snapshot" {
		t.Fatalf("expected world.snapshot broadcast, got kind=%s, count=%d", b.lastKind, b.count)
	}

	snapshot, ok := b.lastPayload.(protocol.WorldSnapshot)
	if !ok || len(snapshot.Players) != 1 {
		t.Fatalf("unexpected snapshot payload: %+v", b.lastPayload)
	}
	if snapshot.Players[0].ID != "usr-1" || snapshot.Players[0].Username != "Vance" {
		t.Fatalf("unexpected player in snapshot: %+v", snapshot.Players[0])
	}

	// 7. Leave
	err = loop.Leave(ctx, "usr-1")
	if err != nil {
		t.Fatalf("Leave failed: %v", err)
	}
	loop.tick()
	snapshotAfterLeave := b.lastPayload.(protocol.WorldSnapshot)
	if len(snapshotAfterLeave.Players) != 0 {
		t.Fatalf("expected 0 players after leave, got %d", len(snapshotAfterLeave.Players))
	}
}

func TestPersistenceManagerAutoSaveAndSpawn(t *testing.T) {
	tempDir := t.TempDir()
	repo, err := storage.NewFileStore(tempDir)
	if err != nil {
		t.Fatal(err)
	}

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	b := &mockBroadcaster{}
	loop := NewServerLoop(repo, b, log)
	pm := NewPersistenceManager(repo, loop, 100*time.Millisecond, log)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 1. Spawn new player -> should get cryo-bay coordinates [-15, 1.7, 0]
	spawnState, err := pm.SpawnPlayer(ctx, "astro-1")
	if err != nil {
		t.Fatalf("SpawnPlayer failed: %v", err)
	}
	if spawnState.Position.X != -15.0 || spawnState.Position.Y != 1.7 || spawnState.Position.Z != 0.0 {
		t.Fatalf("expected cryo-bay spawn [-15, 1.7, 0], got: %+v", spawnState.Position)
	}

	// Join to loop
	user := model.User{ID: "astro-1", Username: "Explorer"}
	_, _ = loop.Join(ctx, user, spawnState)

	// Move player
	_ = loop.Input(ctx, "astro-1", protocol.PlayerInput{
		Seq: 1,
		X:   -14.0,
		Y:   1.7,
		Z:   0.5,
		Yaw: 0.1,
	})

	// Flush to storage
	pm.Flush(ctx)

	// Load from storage to verify it saved
	savedState, err := repo.LoadPlayer(ctx, "astro-1")
	if err != nil {
		t.Fatalf("LoadPlayer failed: %v", err)
	}
	if savedState.Position.X != -14.0 || savedState.Position.Z != 0.5 {
		t.Fatalf("expected saved position X=-14.0, Z=0.5, got: %+v", savedState.Position)
	}

	// 2. Spawn existing player -> should restore saved coordinates
	restoredState, err := pm.SpawnPlayer(ctx, "astro-1")
	if err != nil {
		t.Fatalf("SpawnPlayer on existing player failed: %v", err)
	}
	if restoredState.Position.X != -14.0 {
		t.Fatalf("expected restored position X=-14.0, got: %+v", restoredState.Position)
	}
}

func TestMovementBudgetRejectsRepeatedSmallTeleports(t *testing.T) {
	repo, err := storage.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	l := NewServerLoop(repo, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx := context.Background()
	u := model.User{ID: "budget", Username: "pilot"}
	l.Join(ctx, u, model.PlayerState{PlayerID: u.ID, Position: model.Position{Y: 1.7}})
	if err := l.Input(ctx, u.ID, protocol.PlayerInput{Seq: 1, X: 1, Y: 1.7}); err != nil {
		t.Fatal(err)
	}
	if err := l.Input(ctx, u.ID, protocol.PlayerInput{Seq: 2, X: 2, Y: 1.7}); err == nil {
		t.Fatal("repeated small teleport bypassed speed validation")
	}
	if err := l.Leave(ctx, u.ID); err != nil {
		t.Fatal(err)
	}
	s, err := repo.LoadPlayer(ctx, u.ID)
	if err != nil || s.Position.X != 1 {
		t.Fatalf("rejected coordinates saved: %+v %v", s, err)
	}
}
