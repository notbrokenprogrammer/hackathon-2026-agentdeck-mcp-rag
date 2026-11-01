// Package world defines the extension boundary; no world simulation is implemented.
package world

import (
	"context"
	"feeders/internal/model"
	"feeders/pkg/protocol"
)

// Service owns authoritative state, movement validation, AOI and periodic saves.
// Calls can arrive concurrently for different players. One active socket per player.
// Input coordinates are proposals: validate speed, bounds and sequence before use.
// Join must not leave a partial player behind on failure. Leave must persist state.
type Service interface {
	Join(context.Context, model.User, model.PlayerState) (model.PlayerState, error)
	Input(context.Context, string, protocol.PlayerInput) error
	Leave(context.Context, string) error
}

// Publisher lets a world emit snapshots/chunks to selected players without knowing
// about sockets. Never broadcast all world data: choose recipients using server AOI.
type Publisher interface {
	Publish(playerID, messageType string, payload any) bool
}
