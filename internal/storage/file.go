package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"feeders/internal/model"
)

// FileStore is a small, single-process repository. The host owns its data directory.
// Each record is replaced using a temporary file in the same directory.
type FileStore struct {
	mu  sync.Mutex
	dir string
}

func NewFileStore(dir string) (*FileStore, error) {
	for _, name := range []string{"accounts", "players"} {
		if err := os.MkdirAll(filepath.Join(dir, name), 0700); err != nil {
			return nil, err
		}
	}
	return &FileStore{dir: dir}, nil
}

// Hex encoding prevents usernames/IDs from becoming paths, including on Windows.
func (s *FileStore) path(kind, key string) string {
	return filepath.Join(s.dir, kind, fmt.Sprintf("%x.json", []byte(key)))
}

func readRecord(path string, target any) error {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(b, target)
}

func writeRecord(path string, value any) error {
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".record-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

func (s *FileStore) CreateAccount(ctx context.Context, account model.Account) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	path := s.path("accounts", account.User.Username)
	if _, err := os.Stat(path); err == nil {
		return ErrConflict
	} else if !os.IsNotExist(err) {
		return err
	}
	return writeRecord(path, account)
}

func (s *FileStore) FindAccount(ctx context.Context, username string) (model.Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var account model.Account
	if err := ctx.Err(); err != nil {
		return account, err
	}
	err := readRecord(s.path("accounts", username), &account)
	return account, err
}

func (s *FileStore) LoadPlayer(ctx context.Context, id string) (model.PlayerState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var state model.PlayerState
	if err := ctx.Err(); err != nil {
		return state, err
	}
	err := readRecord(s.path("players", id), &state)
	return state, err
}

func (s *FileStore) SavePlayer(ctx context.Context, state model.PlayerState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	return writeRecord(s.path("players", state.PlayerID), state)
}
