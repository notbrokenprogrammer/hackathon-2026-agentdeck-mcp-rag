package mission

import (
	"context"
	"encoding/json"
	"errors"
)

// Commands are an in-process capability bound by the authenticated game handler.
// Neither retrieved text nor model tool arguments can choose the player or room.
type CommandResult struct {
	Status string `json:"status"`
	Text   string `json:"text"`
	Action string `json:"action,omitempty"`
	State  any    `json:"state,omitempty"`
}
type commandBinding struct {
	scope   string
	execute func(context.Context, string, json.RawMessage) (CommandResult, error)
}
type commandKey struct{}

func WithCommands(ctx context.Context, scope string, execute func(context.Context, string, json.RawMessage) (CommandResult, error)) context.Context {
	return context.WithValue(ctx, commandKey{}, commandBinding{scope, execute})
}
func CommandScope(ctx context.Context) string {
	b, _ := ctx.Value(commandKey{}).(commandBinding)
	return b.scope
}
func ExecuteCommand(ctx context.Context, name string, args json.RawMessage) (CommandResult, error) {
	if err := ctx.Err(); err != nil {
		return CommandResult{}, err
	}
	b, ok := ctx.Value(commandKey{}).(commandBinding)
	if !ok || b.execute == nil {
		return CommandResult{}, errors.New("поручения доступны после входа в экспедицию")
	}
	return b.execute(ctx, name, args)
}
