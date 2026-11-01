package npc

import (
	"errors"
	"feeders/internal/storage"
	"time"
)

type memoryStorage interface {
	LoadState(string, any) error
	SaveState(string, any) error
}
type memoryRecord struct {
	Player   string    `json:"player"`
	Agent    string    `json:"agent"`
	Messages []message `json:"messages"`
	Updated  time.Time `json:"updated"`
	Scope    string    `json:"scope"`
}

func (s *Local) WithMemory(store memoryStorage) error {
	s.memoryStore = store
	var rows []memoryRecord
	if err := store.LoadState("npc_memory_v1", &rows); err != nil && !errors.Is(err, storage.ErrNotFound) {
		return err
	}
	for _, r := range rows {
		s.history[key{r.Player, r.Agent}] = conversation{messages: r.Messages, updated: r.Updated, scope: r.Scope}
	}
	return nil
}
func (s *Local) saveMemoryLocked() {
	if s.memoryStore == nil {
		return
	}
	rows := []memoryRecord{}
	for k, c := range s.history {
		rows = append(rows, memoryRecord{k.player, k.agent, c.messages, c.updated, c.scope})
	}
	s.memoryError = s.memoryStore.SaveState("npc_memory_v1", rows)
}
