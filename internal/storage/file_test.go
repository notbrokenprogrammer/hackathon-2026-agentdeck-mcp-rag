package storage

import (
	"context"
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"testing"

	"feeders/internal/model"
)

func TestFileStoreRestartAndOverwrite(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s, err := NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	account := model.Account{User: model.User{ID: "player-1", Username: "explorer"}, PasswordHash: []byte("hash")}
	if err := s.CreateAccount(ctx, account); err != nil {
		t.Fatal(err)
	}
	state := model.PlayerState{PlayerID: account.User.ID, Position: model.Position{X: 1}}
	if err := s.SavePlayer(ctx, state); err != nil {
		t.Fatal(err)
	}
	state.Position.X = 42
	if err := s.SavePlayer(ctx, state); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	got, err := reopened.LoadPlayer(ctx, state.PlayerID)
	if err != nil || got.Position.X != 42 {
		t.Fatalf("reopen: %+v %v", got, err)
	}
	user, err := reopened.FindAccount(ctx, "explorer")
	if err != nil || user.User.ID != "player-1" {
		t.Fatalf("account: %+v %v", user, err)
	}
	if _, err := s.LoadPlayer(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.path("players", state.PlayerID), []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.LoadPlayer(ctx, state.PlayerID); err == nil || errors.Is(err, ErrNotFound) {
		t.Fatalf("corruption silently ignored: %v", err)
	}
}

func TestCreateAccountConcurrent(t *testing.T) {
	s, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var successes atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := s.CreateAccount(context.Background(), model.Account{User: model.User{Username: "same"}})
			if err == nil {
				successes.Add(1)
			} else if !errors.Is(err, ErrConflict) {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if successes.Load() != 1 {
		t.Fatalf("created %d accounts", successes.Load())
	}
}
