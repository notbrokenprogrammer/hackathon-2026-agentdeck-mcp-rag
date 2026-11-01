// Package game - loop.go
// Этап 2 для Разработчика 2 (Network / Realtime):
// - Высокопроизводительный Server Tick Loop 20Hz (каждые 50 мс)
// - Валидация перемещений игрока (скорость, границы станции, монотонность sequence)
// - Формирование и рассылка снимков мира (world.snapshot) подключенным игрокам
package game

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"sync"
	"time"

	"feeders/internal/model"
	"feeders/internal/storage"
	"feeders/pkg/protocol"
)

type Broadcaster interface {
	Broadcast(kind string, payload any) int
	Publish(playerID, kind string, payload any) bool
}

type LivePlayer struct {
	User           model.User
	Position       model.Position
	Yaw            float64
	LastSeq        uint64
	LastInputTime  time.Time
	Dirty          bool
	MovementBudget float64
}

// ServerLoop управляет активным игровым циклом 20 Гц и валидацией перемещений.
type ServerLoop struct {
	RoomOf      func(string) string
	Walkable    func(float64, float64) bool
	repo        storage.Players
	broadcaster Broadcaster
	log         *slog.Logger
	mu          sync.RWMutex
	saveMu      sync.Mutex
	players     map[string]*LivePlayer
	stopCh      chan struct{}
	running     bool

	// Константы валидации перемещений
	maxSpeed     float64 // 25.0 м/с (бег 16 м/с + резерв на лаг пакетов)
	maxFrameDist float64 // 30.0 м макс. скачок за один пакет
	minBoundX    float64
	maxBoundX    float64
	minBoundZ    float64
	maxBoundZ    float64
	minBoundY    float64
	maxBoundY    float64
}

func NewServerLoop(repo storage.Players, b Broadcaster, log *slog.Logger) *ServerLoop {
	return &ServerLoop{
		repo:         repo,
		broadcaster:  b,
		log:          log,
		players:      make(map[string]*LivePlayer),
		stopCh:       make(chan struct{}),
		maxSpeed:     25.0,
		maxFrameDist: 30.0,
		minBoundX:    -150.0,
		maxBoundX:    150.0,
		minBoundZ:    -150.0,
		maxBoundZ:    150.0,
		minBoundY:    0.0,
		maxBoundY:    25.0,
	}
}

// Start запускает цикл тиков 20 Гц (50 мс) в фоновой горутине.
func (l *ServerLoop) Start(ctx context.Context) {
	l.mu.Lock()
	if l.running {
		l.mu.Unlock()
		return
	}
	l.running = true
	l.mu.Unlock()

	l.log.Info("server tick loop started", "frequency_hz", 20, "tick_interval_ms", 50)

	ticker := time.NewTicker(50 * time.Millisecond) // 20 Гц
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-l.stopCh:
				return
			case <-ticker.C:
				l.tick()
			}
		}
	}()
}

// Stop останавливает цикл тиков.
func (l *ServerLoop) Stop() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.running {
		return
	}
	l.running = false
	close(l.stopCh)
}

// tick собирает снимки всех активных игроков и рассылает world.snapshot.
func (l *ServerLoop) tick() {
	l.mu.RLock()

	roomOf := l.RoomOf
	snapshotItems := make([]protocol.PlayerSnapshotItem, 0, len(l.players))
	for id, p := range l.players {
		snapshotItems = append(snapshotItems, protocol.PlayerSnapshotItem{
			ID:       id,
			Username: p.User.Username,
			X:        p.Position.X,
			Y:        p.Position.Y,
			Z:        p.Position.Z,
			Yaw:      p.Yaw,
		})
	}
	l.mu.RUnlock()

	// Формируем пакет world.snapshot по контракту ТЗ
	snapshot := protocol.WorldSnapshot{
		ServerTime: time.Now().UnixMilli(),
		Players:    snapshotItems,
	}

	if l.broadcaster != nil {
		if roomOf == nil {
			l.broadcaster.Broadcast("world.snapshot", snapshot)
		} else {
			for _, target := range snapshotItems {
				room := roomOf(target.ID)
				filtered := []protocol.PlayerSnapshotItem{}
				for _, item := range snapshotItems {
					if (room != "" && roomOf(item.ID) == room) || item.ID == target.ID {
						filtered = append(filtered, item)
					}
				}
				l.broadcaster.Publish(target.ID, "world.snapshot", protocol.WorldSnapshot{ServerTime: snapshot.ServerTime, Players: filtered})
			}
		}
	}
}

// Join регистрирует игрока в цикле симуляции.
func (l *ServerLoop) Join(ctx context.Context, user model.User, state model.PlayerState) (model.PlayerState, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.Walkable != nil {
		state.Position.Y = 1.7
	}
	if l.Walkable != nil && !l.Walkable(state.Position.X, state.Position.Z) {
		state.Position = model.Position{X: -15, Y: 1.7, Z: 0}
	}
	l.players[user.ID] = &LivePlayer{
		User:           user,
		Position:       state.Position,
		Yaw:            state.Yaw,
		LastSeq:        0, // Новый сокет сбрасывает sequence
		LastInputTime:  time.Now(),
		Dirty:          false,
		MovementBudget: 1.5,
	}

	l.log.Info("player joined simulation loop", "player_id", user.ID, "username", user.Username, "pos", state.Position)
	return state, nil
}

// Input валидирует входные данные перемещения игрока (скорость, границы, монотонность seq).
func (l *ServerLoop) Input(ctx context.Context, playerID string, input protocol.PlayerInput) error {
	if !input.Valid() {
		return errors.New("invalid movement")
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	p, exists := l.players[playerID]
	if !exists {
		return errors.New("player not in simulation")
	}

	// 1. Проверка монотонности seq
	if input.Seq <= p.LastSeq {
		// Устаревший или дублирующий пакет
		return nil
	}

	// 2. Проверка границ станции «Гелиос-7»
	if input.X < l.minBoundX || input.X > l.maxBoundX ||
		input.Z < l.minBoundZ || input.Z > l.maxBoundZ ||
		input.Y < l.minBoundY || input.Y > l.maxBoundY {
		l.log.Warn("player out of bounds", "player_id", playerID, "x", input.X, "z", input.Z)
		return errors.New("coordinates out of bounds")
	}

	if l.Walkable != nil && math.Abs(input.Y-1.7) > .02 {
		return errors.New("player must remain on deck")
	}
	if l.Walkable != nil && !l.Walkable(input.X, input.Z) {
		return errors.New("оборудование или граница отсека")
	}
	// Sweep the whole segment; endpoint-only tests allow thin-wall tunneling.
	if l.Walkable != nil {
		steps := int(math.Ceil(math.Hypot(input.X-p.Position.X, input.Z-p.Position.Z) / .08))
		for i := 1; i < steps; i++ {
			t := float64(i) / float64(steps)
			if !l.Walkable(p.Position.X+(input.X-p.Position.X)*t, p.Position.Z+(input.Z-p.Position.Z)*t) {
				return errors.New("wall crossing")
			}
		}
	}
	// 3. Проверка допустимой скорости (анти-телепортация)
	now := time.Now()
	dt := now.Sub(p.LastInputTime).Seconds()
	if dt <= 0 {
		dt = 0.001
	}

	dx := input.X - p.Position.X
	dz := input.Z - p.Position.Z
	dy := input.Y - p.Position.Y
	dist := math.Sqrt(dx*dx + dz*dz + dy*dy)

	if dist > l.maxFrameDist {
		l.log.Warn("player movement delta exceeded", "player_id", playerID, "dist", dist)
		return errors.New("movement delta too large")
	}

	budget := math.Min(3, p.MovementBudget+dt*l.maxSpeed)
	if dist > budget {
		return errors.New("speed limit exceeded")
	}

	// Обновляем проверенное состояние
	p.Position.X = input.X
	p.Position.Y = input.Y
	p.Position.Z = input.Z
	p.Yaw = input.Yaw
	p.LastSeq = input.Seq
	p.LastInputTime = now
	p.Dirty = true
	p.MovementBudget = budget - dist

	return nil
}

// Leave удаляет игрока из цикла симуляции и сохраняет его актуальное состояние на диск.
func (l *ServerLoop) Leave(ctx context.Context, playerID string) error {
	l.saveMu.Lock()
	defer l.saveMu.Unlock()
	l.mu.Lock()
	p, exists := l.players[playerID]
	if exists {
		delete(l.players, playerID)
	}
	l.mu.Unlock()

	if exists && l.repo != nil {
		return l.repo.SavePlayer(ctx, model.PlayerState{
			PlayerID:  playerID,
			Position:  p.Position,
			Yaw:       p.Yaw,
			LastSeq:   p.LastSeq,
			UpdatedAt: time.Now().UTC(),
		})
	}
	return nil
}

func (l *ServerLoop) Persist(ctx context.Context) {
	l.saveMu.Lock()
	defer l.saveMu.Unlock()
	for _, state := range l.GetAllPlayersStates() {
		if err := l.repo.SavePlayer(ctx, state); err != nil {
			l.log.Error("save player", "error", err)
		}
	}
}

// GetDirtyPlayers возвращает копию состояний игроков, чьи позиции изменились с прошлого сохранения.
func (l *ServerLoop) GetDirtyPlayers() []model.PlayerState {
	l.mu.Lock()
	defer l.mu.Unlock()

	dirty := make([]model.PlayerState, 0)
	now := time.Now().UTC()

	for id, p := range l.players {
		if p.Dirty {
			dirty = append(dirty, model.PlayerState{
				PlayerID:  id,
				Position:  p.Position,
				Yaw:       p.Yaw,
				LastSeq:   p.LastSeq,
				UpdatedAt: now,
			})
			p.Dirty = false
		}
	}

	return dirty
}

// GetAllPlayersStates возвращает актуальное состояние всех активных игроков.
func (l *ServerLoop) GetAllPlayersStates() []model.PlayerState {
	l.mu.RLock()
	defer l.mu.RUnlock()

	all := make([]model.PlayerState, 0, len(l.players))
	now := time.Now().UTC()

	for id, p := range l.players {
		all = append(all, model.PlayerState{
			PlayerID:  id,
			Position:  p.Position,
			Yaw:       p.Yaw,
			LastSeq:   p.LastSeq,
			UpdatedAt: now,
		})
	}

	return all
}

func (l *ServerLoop) ConfigureWorld(roomOf func(string) string, walkable func(float64, float64) bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.RoomOf = roomOf
	l.Walkable = walkable
}
