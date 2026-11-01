package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"feeders/internal/model"
	_ "modernc.org/sqlite"
)

// StateStore persists versioned application records atomically with SQLite WAL.
type StateStore interface {
	LoadState(string, any) error
	SaveState(string, any) error
}

type SQLiteStore struct{ db *sql.DB }

func NewSQLiteStore(dir string) (*SQLiteStore, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "last-crew.sqlite"))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	for _, q := range []string{"PRAGMA journal_mode=WAL", "PRAGMA busy_timeout=5000", "PRAGMA foreign_keys=ON", `CREATE TABLE IF NOT EXISTS accounts(username TEXT PRIMARY KEY, user_id TEXT NOT NULL UNIQUE, record BLOB NOT NULL)`, `CREATE TABLE IF NOT EXISTS players(user_id TEXT PRIMARY KEY, record BLOB NOT NULL)`, `CREATE TABLE IF NOT EXISTS app_state(key TEXT PRIMARY KEY, record BLOB NOT NULL)`} {
		if _, err = db.Exec(q); err != nil {
			db.Close()
			return nil, err
		}
	}
	s := &SQLiteStore{db: db}
	// Import the previous JSON repository once. Keep originals as a backup.
	var done bool
	if errors.Is(s.LoadState("legacy_import_v1", &done), ErrNotFound) {
		for _, kind := range []string{"accounts", "players"} {
			files, _ := filepath.Glob(filepath.Join(dir, kind, "*.json"))
			for _, p := range files {
				b, e := os.ReadFile(p)
				if e != nil {
					db.Close()
					return nil, e
				}
				if kind == "accounts" {
					var a model.Account
					if e = json.Unmarshal(b, &a); e == nil {
						e = s.CreateAccount(context.Background(), a)
					}
					if e != nil && !errors.Is(e, ErrConflict) {
						db.Close()
						return nil, e
					}
				} else {
					var v model.PlayerState
					if e = json.Unmarshal(b, &v); e == nil {
						e = s.SavePlayer(context.Background(), v)
					}
					if e != nil {
						db.Close()
						return nil, e
					}
				}
			}
		}
		if err = s.SaveState("legacy_import_v1", true); err != nil {
			db.Close()
			return nil, err
		}
	}
	return s, nil
}
func (s *SQLiteStore) Close() error { return s.db.Close() }
func (s *SQLiteStore) CreateAccount(ctx context.Context, a model.Account) error {
	b, e := json.Marshal(a)
	if e != nil {
		return e
	}
	result, e := s.db.ExecContext(ctx, "INSERT INTO accounts(username,user_id,record) VALUES(?,?,?) ON CONFLICT DO NOTHING", a.User.Username, a.User.ID, b)
	if e != nil {
		return e
	}
	n, e := result.RowsAffected()
	if e == nil && n != 1 {
		return ErrConflict
	}
	return e
}
func (s *SQLiteStore) FindAccount(ctx context.Context, name string) (model.Account, error) {
	var a model.Account
	var b []byte
	e := s.db.QueryRowContext(ctx, "SELECT record FROM accounts WHERE username=?", name).Scan(&b)
	if errors.Is(e, sql.ErrNoRows) {
		return a, ErrNotFound
	}
	if e == nil {
		e = json.Unmarshal(b, &a)
	}
	return a, e
}
func (s *SQLiteStore) LoadPlayer(ctx context.Context, id string) (model.PlayerState, error) {
	var a model.PlayerState
	var b []byte
	e := s.db.QueryRowContext(ctx, "SELECT record FROM players WHERE user_id=?", id).Scan(&b)
	if errors.Is(e, sql.ErrNoRows) {
		return a, ErrNotFound
	}
	if e == nil {
		e = json.Unmarshal(b, &a)
	}
	return a, e
}
func (s *SQLiteStore) SavePlayer(ctx context.Context, a model.PlayerState) error {
	b, e := json.Marshal(a)
	if e != nil {
		return e
	}
	_, e = s.db.ExecContext(ctx, "INSERT INTO players(user_id,record) VALUES(?,?) ON CONFLICT(user_id) DO UPDATE SET record=excluded.record", a.PlayerID, b)
	return e
}
func (s *SQLiteStore) LoadState(key string, v any) error {
	var b []byte
	e := s.db.QueryRow("SELECT record FROM app_state WHERE key=?", key).Scan(&b)
	if errors.Is(e, sql.ErrNoRows) {
		return ErrNotFound
	}
	if e != nil {
		return e
	}
	return json.Unmarshal(b, v)
}
func (s *SQLiteStore) SaveState(key string, v any) error {
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	_, e = s.db.Exec("INSERT INTO app_state(key,record) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET record=excluded.record", key, b)
	return e
}
