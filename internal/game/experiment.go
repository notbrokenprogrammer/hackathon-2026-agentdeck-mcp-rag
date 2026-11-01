package game

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"feeders/internal/realtime"
	"feeders/internal/server"
	"feeders/pkg/protocol"
	"fmt"
	"math"
	"net"
	"net/http"
	"strings"
	"time"
)

type LearningResult struct {
	Prediction string  `json:"prediction"`
	Correct    bool    `json:"correct"`
	Speed      float64 `json:"speed"`
	Gamma      float64 `json:"gamma"`
	Experiment string  `json:"experiment"`
}
type Experiment struct {
	ID          string            `json:"id"`
	Type        string            `json:"type"`
	Version     uint64            `json:"version"`
	Speed       float64           `json:"speed"`
	Members     map[string]string `json:"members"`
	Predictions map[string]string `json:"predictions"`
	Ran         bool              `json:"ran"`
	Gamma       float64           `json:"gamma"`
	StartedAt   int64             `json:"started_at"`
}

func (m *Module) EnableWorldTools(key string) { m.worldToolKey = key }

// WorldToolsHandler is bound only on a separate loopback HTTP listener. It is
// never reachable through the public/TLS application listener or its tunnel.
func (m *Module) WorldToolsHandler() http.Handler {
	mux := http.NewServeMux()
	m.registerExperimentHTTP(mux)
	return mux
}
func (m *Module) startExperiment(id string) (*Experiment, error) {
	return m.startExperimentInScope(context.Background(), id)
}
func (m *Module) startExperimentInScope(ctx context.Context, id string) (*Experiment, error) {
	m.roomMu.Lock()
	r := m.rooms[m.membership[id]]
	if r == nil || !r.Started || !m.validCommandScope(ctx, r, id) {
		m.roomMu.Unlock()
		return nil, fmt.Errorf("сначала начните миссию")
	}
	if r.Experiment != nil {
		copy := *r.Experiment
		m.roomMu.Unlock()
		return &copy, nil
	}
	r.Experiment = &Experiment{ID: fmt.Sprintf("clock-%s-%d", r.Code, r.Generation), Type: "light_clock", Version: 1, Speed: .6, Members: map[string]string{}, Predictions: map[string]string{}}
	err := m.persistRoomsLocked()
	code := r.Code
	copy := *r.Experiment
	if err != nil {
		r.Experiment = nil
	}
	m.roomMu.Unlock()
	if err != nil {
		return nil, err
	}
	m.publishRoom(code)
	return &copy, nil
}
func (m *Module) experimentStart(ctx context.Context, s realtime.Session, e protocol.Envelope) {
	if !m.isReady(s.User().ID) {
		sendError(s, e, "not_initialized", "Войдите в игру")
		return
	}
	var p struct{}
	if protocol.DecodePayload(e.Payload, &p) != nil {
		sendError(s, e, "invalid_payload", "Некорректный запрос")
		return
	}
	v, err := m.startExperiment(s.User().ID)
	if err != nil {
		sendError(s, e, "unavailable", err.Error())
		return
	}
	s.Send("experiment.started", e.RequestID, v)
}
func (m *Module) experimentCommand(ctx context.Context, s realtime.Session, e protocol.Envelope) {
	var p struct {
		Action     string   `json:"action"`
		Role       string   `json:"role,omitempty"`
		Prediction string   `json:"prediction,omitempty"`
		Speed      *float64 `json:"speed,omitempty"`
		Version    uint64   `json:"expected_version"`
	}
	if protocol.DecodePayload(e.Payload, &p) != nil {
		sendError(s, e, "invalid_payload", "Некорректная команда опыта")
		return
	}
	id := s.User().ID
	m.roomMu.Lock()
	r := m.rooms[m.membership[id]]
	if r == nil || r.Experiment == nil || !m.isReady(id) {
		m.roomMu.Unlock()
		sendError(s, e, "not_joined", "Опыт ещё не создан")
		return
	}
	v := r.Experiment
	fail := func(code, msg string) { m.roomMu.Unlock(); sendError(s, e, code, msg) }
	if v.Version != p.Version || v.Ran {
		fail("stale_version", "Опыт изменился или уже завершён")
		return
	}
	switch p.Action {
	case "join":
		if p.Role != "observer" && p.Role != "operator" {
			fail("invalid_role", "Выберите наблюдателя или оператора")
			return
		}
		for actor, role := range v.Members {
			if actor != id && role == p.Role {
				fail("occupied", "Эта роль уже занята")
				return
			}
		}
		v.Members[id] = p.Role
	case "predict":
		if v.Members[id] == "" || (p.Prediction != "same" && p.Prediction != "different") {
			fail("invalid_prediction", "Выберите роль и прогноз")
			return
		}
		v.Predictions[id] = p.Prediction
	case "speed":
		if v.Members[id] != "operator" || p.Speed == nil || math.IsNaN(*p.Speed) || *p.Speed < 0 || *p.Speed > .8 {
			fail("forbidden", "Только оператор задаёт скорость от 0 до 0,8")
			return
		}
		v.Speed = *p.Speed
	case "run":
		if v.Members[id] != "observer" || len(v.Members) != 2 || len(v.Predictions) != 2 {
			fail("not_ready", "Нужны два разных участника, роли и два прогноза")
			return
		}
		v.Ran = true
		v.Gamma = 1 / math.Sqrt(1-v.Speed*v.Speed)
		v.StartedAt = time.Now().UnixMilli()
		if r.Learning == nil {
			r.Learning = map[string]LearningResult{}
		}
		expected := "different"
		if v.Speed == 0 {
			expected = "same"
		}
		for actor, prediction := range v.Predictions {
			r.Learning[actor] = LearningResult{prediction, prediction == expected, v.Speed, v.Gamma, v.ID}
		}
	default:
		fail("invalid_action", "Неизвестное действие опыта")
		return
	}
	v.Version++
	err := m.persistRoomsLocked()
	code := r.Code
	m.roomMu.Unlock()
	if err != nil {
		m.failure(s, e, err)
		return
	}
	s.Send("experiment.accepted", e.RequestID, map[string]bool{"ok": true})
	m.publishRoom(code)
	m.exportCrewView()
}
func (m *Module) registerExperimentHTTP(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/internal/experiment", func(w http.ResponseWriter, r *http.Request) {
		host, _, _ := net.SplitHostPort(r.RemoteAddr)
		ip := net.ParseIP(host)
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if ip == nil || !ip.IsLoopback() || m.worldToolKey == "" || subtle.ConstantTimeCompare([]byte(token), []byte(m.worldToolKey)) != 1 {
			server.JSON(w, 403, map[string]string{"message": "Действие недоступно"})
			return
		}
		var p struct {
			Player string `json:"player"`
			Type   string `json:"type"`
		}
		r.Body = http.MaxBytesReader(w, r.Body, 2048)
		d := json.NewDecoder(r.Body)
		d.DisallowUnknownFields()
		if d.Decode(&p) != nil || p.Type != "light_clock" || !m.isReady(p.Player) {
			server.JSON(w, 400, map[string]string{"message": "Некорректный контекст опыта"})
			return
		}
		v, err := m.startExperiment(p.Player)
		if err != nil {
			server.JSON(w, 409, map[string]string{"message": err.Error()})
			return
		}
		server.JSON(w, 200, map[string]any{"id": v.ID, "type": v.Type, "version": v.Version, "created": true})
	})
}

func (m *Module) experimentPreview(ctx context.Context, s realtime.Session, e protocol.Envelope) {
	var p struct {
		Speed float64 `json:"speed"`
	}
	if protocol.DecodePayload(e.Payload, &p) != nil || p.Speed < 0 || p.Speed > .8 || math.IsNaN(p.Speed) || !m.isReady(s.User().ID) {
		sendError(s, e, "invalid_speed", "Скорость от 0 до 0,8")
		return
	}
	s.Send("experiment.preview", e.RequestID, map[string]any{"gamma": 1 / math.Sqrt(1-p.Speed*p.Speed), "speed": p.Speed, "cooperation_credit": false})
}
