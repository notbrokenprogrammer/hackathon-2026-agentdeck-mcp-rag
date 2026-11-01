package storage

import (
	"context"
	"errors"
	"feeders/internal/model"
)

var ErrNotFound = errors.New("not found")
var ErrConflict = errors.New("already exists")

// Implementations must be safe for concurrent calls. CreateAccount is atomic.
type Accounts interface {
	CreateAccount(context.Context, model.Account) error
	FindAccount(context.Context, string) (model.Account, error)
}

type Players interface {
	LoadPlayer(context.Context, string) (model.PlayerState, error)
	SavePlayer(context.Context, model.PlayerState) error
}
