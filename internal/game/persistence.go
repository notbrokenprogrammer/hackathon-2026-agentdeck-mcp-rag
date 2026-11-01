// Package game - persistence.go
// Этап 3 для Разработчика 2 (Network / Persistence):
// - Периодическое автосохранение проверенных перемещений игроков в хранилище (storage.Players)
// - Спавн игрока в каюте криосна станции «Гелиос-7» [-15, 1.7, 0]
// - Восстановление сохранённого положения и ориентации при повторном входе
package game

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"feeders/internal/model"
	"feeders/internal/storage"
)

// PersistenceManager отвечает за периодическое сохранение позиций и инициализацию спавна.
type PersistenceManager struct {
	repo         storage.Players
	loop         *ServerLoop
	log          *slog.Logger
	saveInterval time.Duration
	stopCh       chan struct{}
	running      bool
	mu           sync.Mutex
}

func NewPersistenceManager(repo storage.Players, loop *ServerLoop, saveInterval time.Duration, log *slog.Logger) *PersistenceManager {
	if saveInterval <= 0 {
		saveInterval = 5 * time.Second
	}
	return &PersistenceManager{
		repo:         repo,
		loop:         loop,
		log:          log,
		saveInterval: saveInterval,
		stopCh:       make(chan struct{}),
	}
}

// Start запускает периодический таймер автосохранения измененных позиций игроков.
func (pm *PersistenceManager) Start(ctx context.Context) {
	pm.mu.Lock()
	if pm.running {
		pm.mu.Unlock()
		return
	}
	pm.running = true
	pm.mu.Unlock()

	pm.log.Info("periodic persistence auto-save started", "interval", pm.saveInterval)

	ticker := time.NewTicker(pm.saveInterval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				pm.Flush(context.Background())
				return
			case <-pm.stopCh:
				return
			case <-ticker.C:
				pm.saveDirtyPlayers(ctx)
			}
		}
	}()
}

// Stop останавливает таймер и выполняет финальный сброс состояния на диск.
func (pm *PersistenceManager) Stop() {
	pm.mu.Lock()
	if !pm.running {
		pm.mu.Unlock()
		return
	}
	pm.running = false
	close(pm.stopCh)
	pm.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pm.Flush(ctx)
}

// saveDirtyPlayers сохраняет только тех игроков, чьи координаты изменились.
func (pm *PersistenceManager) saveDirtyPlayers(ctx context.Context) {
	if pm.loop == nil || pm.repo == nil {
		return
	}

	pm.loop.Persist(ctx)
}

// Flush принудительно сохраняет актуальное состояние всех активных игроков на диск.
func (pm *PersistenceManager) Flush(ctx context.Context) {
	if pm.loop == nil || pm.repo == nil {
		return
	}

	pm.loop.Persist(ctx)
}

// SpawnPlayer загружает сохраненное состояние игрока либо инициализирует точку спавна в криокапсуле станции.
func (pm *PersistenceManager) SpawnPlayer(ctx context.Context, playerID string) (model.PlayerState, error) {
	state, err := pm.repo.LoadPlayer(ctx, playerID)
	if errors.Is(err, storage.ErrNotFound) {
		// Координаты спавна в криокапсуле станции «Гелиос-7» по ТЗ:
		// X: -15.0 (жилой отсек), Y: 1.7 (уровень глаз), Z: 0.0, Yaw: 0 (лицом к выходу)
		state = model.PlayerState{
			PlayerID: playerID,
			Position: model.Position{
				X: -15.0,
				Y: 1.7,
				Z: 0.0,
			},
			Yaw:       0,
			LastSeq:   0,
			UpdatedAt: time.Now().UTC(),
		}
		if err := pm.repo.SavePlayer(ctx, state); err != nil {
			return state, err
		}
		pm.log.Info("new player spawned in cryo bay", "player_id", playerID, "position", state.Position)
		return state, nil
	}

	if err != nil {
		return state, err
	}

	pm.log.Info("returning player state restored", "player_id", playerID, "position", state.Position)
	return state, nil
}
