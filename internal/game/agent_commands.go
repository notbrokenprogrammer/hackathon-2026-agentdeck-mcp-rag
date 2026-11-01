package game

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"feeders/internal/mission"
	"feeders/internal/realtime"
	"feeders/pkg/protocol"
)

var missionActionNames = map[string]string{
	"repair_cooling": "Восстановить охлаждение", "start_reactor": "Запустить реактор",
	"start_greenhouse": "Запустить оранжерею", "restore_archive": "Восстановить архив",
	"calculate_course": "Рассчитать безопасный курс", "use_battery": "Подключить резервную батарею",
	"wait_cycle": "Завершить цикл", "final_emergency": "Аварийный запуск",
	"final_ecosystem": "Длительный безопасный манёвр", "final_combined": "Комбинированный импульс",
	"final_evacuation": "Эвакуировать экипаж",
}

// Called while roomMu is held, at the same point as authorization and mutation.
func (m *Module) validCommandScope(ctx context.Context, r *Room, id string) bool {
	scope := mission.CommandScope(ctx)
	return scope == "" || (ctx.Err() == nil && r != nil && scope == fmt.Sprintf("%s:%d", r.Code, r.Generation) && m.isReady(id))
}

type commandSession struct {
	realtime.Session
	kind    string
	payload any
}

func (s *commandSession) Send(kind, _ string, payload any) bool {
	s.kind = kind
	s.payload = payload
	return true
}

func (m *Module) agentCommandContext(ctx context.Context, s realtime.Session, e protocol.Envelope, scope, agent string) context.Context {
	// Capture the version before model inference; never execute a stale plan.
	version := m.engineFor(s.User().ID).GetState().Version
	m.roomMu.Lock()
	var worldVersion uint64
	if r := m.rooms[m.membership[s.User().ID]]; r != nil {
		worldVersion = universeOf(r).Version
	}
	m.roomMu.Unlock()
	used := false
	return mission.WithCommands(ctx, scope, func(ctx context.Context, name string, args json.RawMessage) (mission.CommandResult, error) {
		var p struct {
			Action string   `json:"action,omitempty"`
			Clues  []string `json:"clues,omitempty"`
			Text   string   `json:"text,omitempty"`
		}
		if len(args) == 0 {
			args = json.RawMessage(`{}`)
		}
		if err := protocol.DecodePayload(args, &p); err != nil {
			return mission.CommandResult{}, err
		}
		if used {
			return mission.CommandResult{}, errors.New("одно поручение за сообщение; следующий шаг отправьте отдельно")
		}
		m.roomMu.Lock()
		r := m.rooms[m.membership[s.User().ID]]
		valid := scope != "" && r != nil && r.Started && m.validCommandScope(ctx, r, s.User().ID)
		var state GameState
		if valid {
			state = r.engine.GetState()
		}
		m.roomMu.Unlock()
		if !valid {
			return mission.CommandResult{}, errors.New("экспедиция изменилась или ещё не начата; откройте текущую миссию")
		}
		if name == "mission_status" {
			return mission.CommandResult{Status: "status", Text: missionNextStep(state), State: state}, nil
		}
		target := &commandSession{Session: s}
		requestID := "agent:" + e.RequestID
		request := func(kind string, value any) protocol.Envelope {
			env, _ := protocol.Message(kind, requestID, value)
			return env
		}
		switch name {
		case "execute_action", "preview_action":
			if _, ok := missionActionNames[p.Action]; !ok {
				return mission.CommandResult{}, errors.New("выберите существующую операцию корабля")
			}
			payload := protocol.GameActionRequest{Action: p.Action, ExpectedVersion: version}
			if name == "preview_action" {
				m.previewAction(ctx, target, request("game.preview", payload))
			} else {
				used = true
				m.roomAction(ctx, target, request("game.action", payload), payload)
			}
		case "solve_investigation":
			used = true
			payload := protocol.InvestigationSolveRequest{Clues: p.Clues, ExpectedVersion: version}
			m.roomSolve(ctx, target, request("investigation.solve", payload), payload)
		case "start_experiment":
			used = true
			experiment, err := m.startExperimentInScope(ctx, s.User().ID)
			if err != nil {
				return mission.CommandResult{}, err
			}
			return mission.CommandResult{Status: "completed", Action: name, Text: "Световые часы созданы. Откройте лабораторию и выберите роли наблюдателя и оператора.", State: experiment}, nil
		case "world_command":
			used = true
			m.universeCommand(ctx, target, request("universe.command", map[string]any{"agent_id": agent, "text": p.Text, "expected_version": worldVersion}))
		default:
			return mission.CommandResult{}, errors.New("неизвестный инструмент поручения")
		}
		result := mission.CommandResult{Action: p.Action, State: target.payload}
		switch target.kind {
		case "error":
			problem, ok := target.payload.(protocol.Error)
			if ok {
				return result, errors.New(problem.Message)
			}
			return result, errors.New("сервер отклонил поручение")
		case "game.state":
			result.Status = "completed"
			result.Text = "Выполнено: " + missionActionNames[p.Action] + ". Результат сохранён в миссии."
		case "decision.pending":
			result.Status = "pending"
			result.Text = "Манёвр предложен экипажу. Жду согласия остальных участников; он ещё не выполнен."
		case "game.preview":
			result.Status = "preview"
			result.Text = "Расчёт: " + missionActionNames[p.Action] + ". Действие пока не выполнено."
			if v, ok := target.payload.(map[string]any); ok {
				if allowed, _ := v["allowed"].(bool); !allowed {
					result.Text += " " + fmt.Sprint(v["reason"])
				} else if after, ok := v["after"].(GameState); ok {
					result.Text += fmt.Sprintf(" После операции: кислород %d, энергия %d, корпус %d, цикл %d.", after.Oxygen, after.Energy, after.Hull, after.Cycle)
				}
			}
		case "investigation.result":
			v := target.payload.(map[string]any)
			result.Status = "not_completed"
			if v["solved"] == true {
				result.Status = "completed"
			}
			result.Text = fmt.Sprint(v["message"])
		case "universe.state":
			result.Status = "completed"
			result.Action = name
			result.Text = "Поручение окружению выполнено и сохранено."
		default:
			return result, errors.New("нет подтверждения выполнения от сервера")
		}
		return result, nil
	})
}

func missionNextStep(s GameState) string {
	if s.GameOver {
		if s.Victory {
			return "Миссия завершена: экипаж спасён. Выполненные задания сохранены в журнале."
		}
		return "Миссия завершилась неудачей. Выполненные этапы остались в журнале."
	}
	next := "Выберите спасательный манёвр в журнале заданий."
	switch {
	case !s.CoolingRepaired:
		next = "Следующий шаг: восстановить охлаждение. Напишите «Восстанови охлаждение»."
	case !s.ReactorActive:
		next = "Следующий шаг: запустить реактор. Напишите «Запусти реактор»."
	case !s.GreenhouseActive:
		next = "Следующий шаг: запустить оранжерею. Напишите «Запусти оранжерею»."
	case !s.ArchiveRestored:
		next = "Следующий шаг: восстановить архив. Напишите «Восстанови архив»."
	case !s.InvestigationSolved:
		next = "Откройте расследование в журнале заданий и сопоставьте три открытых свидетельства."
	case !s.CourseCalculated:
		next = "Следующий шаг: рассчитать безопасный курс. Напишите «Рассчитай курс»."
	}
	return fmt.Sprintf("Кислород: %d · энергия: %d · корпус: %d · цикл: %d/%d.\n%s", s.Oxygen, s.Energy, s.Hull, s.Cycle, s.MaxCycles, next)
}
