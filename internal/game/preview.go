package game

import (
	"context"
	"feeders/internal/realtime"
	"feeders/pkg/protocol"
)

// Preview uses the same transition as execution and never writes live state.
func (m *Module) previewAction(ctx context.Context, s realtime.Session, e protocol.Envelope) {
	var input protocol.GameActionRequest
	if protocol.DecodePayload(e.Payload, &input) != nil || input.Action == "get_state" {
		sendError(s, e, "invalid_payload", "Выберите операцию для расчёта")
		return
	}
	if m.strictRooms && m.RoomOf(s.User().ID) == "" {
		sendError(s, e, "not_joined", "Сначала присоединитесь к экспедиции")
		return
	}
	view, err := m.View(s.User().ID)
	if err != nil {
		sendError(s, e, "not_initialized", "Сначала подключитесь к кораблю")
		return
	}
	if input.ExpectedVersion != view.State.Version {
		sendError(s, e, "stale_state", "Состояние изменилось. Откройте операцию снова.")
		return
	}
	s.Send("game.preview", e.RequestID, previewExisting(view, input.Action))
}
