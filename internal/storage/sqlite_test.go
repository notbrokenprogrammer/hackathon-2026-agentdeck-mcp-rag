package storage

import (
	"context"
	"errors"
	"feeders/internal/model"
	"testing"
)

func TestSQLiteMigrationAndDurability(t *testing.T) {
	dir := t.TempDir()
	legacy, e := NewFileStore(dir)
	if e != nil {
		t.Fatal(e)
	}
	a := model.Account{User: model.User{ID: "one", Username: "explorer"}, PasswordHash: []byte("hash")}
	legacy.CreateAccount(context.Background(), a)
	legacy.SavePlayer(context.Background(), model.PlayerState{PlayerID: "one", Position: model.Position{X: 42}})
	store, e := NewSQLiteStore(dir)
	if e != nil {
		t.Fatal(e)
	}
	if got, e := store.LoadPlayer(context.Background(), "one"); e != nil || got.Position.X != 42 {
		t.Fatal(got, e)
	}
	if e = store.CreateAccount(context.Background(), a); !errors.Is(e, ErrConflict) {
		t.Fatal("duplicate accepted", e)
	}
	if e = store.SaveState("mission", map[string]int{"version": 9}); e != nil {
		t.Fatal(e)
	}
	store.Close()
	store, e = NewSQLiteStore(dir)
	if e != nil {
		t.Fatal(e)
	}
	defer store.Close()
	var v map[string]int
	if e = store.LoadState("mission", &v); e != nil || v["version"] != 9 {
		t.Fatal(v, e)
	}
}
