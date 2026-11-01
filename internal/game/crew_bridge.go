package game

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"feeders/internal/crew"
	"feeders/internal/mission"
	"feeders/pkg/protocol"
)

// View projects the existing game state for read-only tools. There is no second
// mission simulation and no alternative resource balance in the active host.
func (m *Module) View(playerID string) (mission.View, error) {
	m.mu.Lock()
	_, ok := m.ready[playerID]
	count := len(m.ready)
	m.mu.Unlock()
	if !ok {
		return mission.View{}, mission.ErrNotJoined
	}
	m.roomMu.Lock()
	if r := m.rooms[m.membership[playerID]]; r != nil {
		count = 0
		for id := range r.Members {
			if m.isReady(id) {
				count++
			}
		}
	}
	m.roomMu.Unlock()
	s := m.engineFor(playerID).GetState()
	status := "active"
	if s.GameOver {
		status = "lost"
		if s.Victory {
			status = "won"
		}
	}
	v := mission.View{State: mission.State{Version: s.Version, Cycle: s.Cycle - 1, O: s.Oxygen, E: s.Energy, H: s.Hull, Reactor: s.ReactorActive, Cooling: s.CoolingRepaired, Garden: s.GreenhouseActive, Battery: s.BatteryAvailable, Course: s.CourseCalculated, Archive: s.ArchiveRestored, Investigated: s.InvestigationSolved, BaseDrain: 4, Status: status, Quests: map[string]string{}}, Players: count, Roles: []string{mission.Captain, mission.Engineer, mission.Biologist, mission.Navigator}, Facts: []mission.Fact{}}
	aliases := map[string]string{"temp_spike": "temperature_spike", "impact_mark": "impact_record", "protective_chronology": "archive_chronology"}
	texts := map[string]string{"temperature_spike": "Рост температуры зафиксирован до отключения нагрузки.", "no_infection": "Биопроба не выявила инфекции.", "damaged_filter": "После ремонта открыт результат осмотра фильтра.", "impact_record": "В восстановленном архиве найдена отметка удара.", "archive_chronology": "Архив позволяет сопоставить последовательность защитных отключений."}
	for _, id := range s.UnlockedClues {
		if a := aliases[id]; a != "" {
			id = a
		}
		v.Facts = append(v.Facts, mission.Fact{ID: id, Text: texts[id], Source: "Текущее состояние игры"})
	}
	if s.InvestigationSolved {
		v.Facts = append(v.Facts, mission.Fact{ID: "cause_confirmed", Text: "Причина подтверждена игроками.", Source: "Результат расследования"})
	}
	m.roomMu.Lock()
	if room := m.rooms[m.membership[playerID]]; room != nil {
		if lesson, ok := room.Learning[playerID]; ok {
			v.Learning = lesson
		}
	}
	m.roomMu.Unlock()
	return v, nil
}

// The saved view is a projection only, used by the separate stdio MCP process.
func (m *Module) WithCrewProjection(path string) *Module {
	m.crewProjection = path
	m.exportCrewView()
	return m
}
func (m *Module) exportCrewView() {
	if m.crewProjection == "" {
		return
	}
	m.projectionMu.Lock()
	defer m.projectionMu.Unlock()
	m.mu.Lock()
	ids := []string{}
	for id := range m.ready {
		ids = append(ids, id)
	}
	m.mu.Unlock()
	views := map[string]mission.View{}
	for _, id := range ids {
		if v, err := m.View(id); err == nil {
			views[id] = v
		}
	}
	if err := writeProjection(m.crewProjection, views); err != nil {
		m.log.Error("export MCP view", "error", err)
	}
}
func writeProjection(path string, views map[string]mission.View) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	b, err := json.Marshal(views)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), "crew-view-*.tmp")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err = f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}
func ReadCrewView(path, playerID string) (mission.View, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return mission.View{}, err
	}
	var views map[string]mission.View
	if err = json.Unmarshal(b, &views); err != nil {
		return mission.View{}, err
	}
	v, ok := views[playerID]
	if !ok {
		return v, mission.ErrNotJoined
	}
	return v, nil
}

func legacyFromView(v mission.View) GameState {
	s := GameState{Version: v.State.Version, Cycle: v.State.Cycle + 1, MaxCycles: 8, Oxygen: v.State.O, Energy: v.State.E, Hull: v.State.H, ReactorActive: v.State.Reactor, CoolingRepaired: v.State.Cooling, GreenhouseActive: v.State.Garden, BatteryAvailable: v.State.Battery, CourseCalculated: v.State.Course, ArchiveRestored: v.State.Archive, InvestigationSolved: v.State.Investigated, GameOver: v.State.Status != "active", Logs: []string{}}
	for _, f := range v.Facts {
		if f.ID == "cause_confirmed" {
			continue
		}
		id := f.ID
		if legacy := (map[string]string{"temperature_spike": "temp_spike", "impact_record": "impact_mark", "archive_chronology": "protective_chronology"})[id]; legacy != "" {
			id = legacy
		}
		s.UnlockedClues = append(s.UnlockedClues, id)
	}
	return s
}
func previewExisting(v mission.View, name string) map[string]any {
	names := map[string]string{"start_garden": "start_greenhouse", "prepare_course": "calculate_course", "wait": "wait_cycle", "emergency_burn": "final_emergency", "ecosystem_plan": "final_ecosystem", "combined_plan": "final_combined", "evacuate": "final_evacuation"}
	action := name
	if mapped := names[name]; mapped != "" {
		action = mapped
	}
	before := legacyFromView(v)
	engine := &ScenarioEngine{state: before}
	after, err := engine.ExecuteAction(action)
	result := map[string]any{"action": name, "allowed": err == nil, "before": before, "after": after, "read_only": true, "units": "игровые единицы"}
	if err != nil {
		result["reason"] = err.Error()
	} else {
		result["delta_energy"] = after.Energy - before.Energy
		result["delta_oxygen"] = after.Oxygen - before.Oxygen
		result["delta_hull"] = after.Hull - before.Hull
	}
	return result
}

// CallCrewTool bridges all scenario advice to this project's actual transitions.
// The donor's optional-quest rules remain a library, not a parallel live game.
func CallCrewTool(ctx context.Context, name string, args json.RawMessage) (any, bool, error) {
	suffix := ""
	for _, p := range crew.All() {
		if strings.HasPrefix(name, p.ID+"_") {
			suffix = strings.TrimPrefix(name, p.ID+"_")
			break
		}
	}
	if suffix == "" || suffix == "search" || suffix == "read_chunk" || suffix == "list_sources" {
		return nil, false, nil
	}
	v, ok := mission.FromContext(ctx)
	if !ok {
		return nil, true, mission.ErrNotJoined
	}
	if suffix == "preview_action" {
		var a mission.Action
		if err := protocol.DecodePayload(args, &a); err != nil {
			return nil, true, err
		}
		if a.Choice != "" || len(a.Path) != 0 {
			return nil, true, errors.New("варианты дополнительных квестов не включены в текущей партии")
		}
		return previewExisting(v, a.Name), true, nil
	}
	if suffix == "quest_options" || suffix == "drone_route" || suffix == "door_check" || suffix == "garden_options" || suffix == "power_audit" || suffix == "compare_navigation" || suffix == "memory_check" {
		return nil, true, errors.New("дополнительный квест не включён в текущей версии партии")
	}
	if len(args) > 0 {
		if err := protocol.DecodePayload(args, &struct{}{}); err != nil {
			return nil, true, err
		}
	}
	switch suffix {
	case "mission_status":
		return v, true, nil
	case "rescue_plans":
		out := []any{}
		for _, a := range []string{"emergency_burn", "ecosystem_plan", "combined_plan", "evacuate"} {
			out = append(out, previewExisting(v, a))
		}
		return map[string]any{"plans": out, "read_only": true}, true, nil
	case "systems_diagnosis":
		warnings := []string{}
		if v.State.Reactor && !v.State.Cooling {
			warnings = append(warnings, "Реактор без охлаждения: -12 H за обычный цикл")
		}
		return map[string]any{"state": v.State, "warnings": warnings, "facts": v.Facts}, true, nil
	case "oxygen_forecast":
		return map[string]any{"current_oxygen": v.State.O, "without_garden_per_cycle": 8, "with_garden_per_cycle": 3, "garden_active": v.State.Garden, "units": "игровые единицы"}, true, nil
	case "analyze_samples":
		if !v.HasFact("no_infection") {
			return nil, true, errors.New("сначала откройте биопробу")
		}
		return map[string]any{"infection": false, "does_not_prove": "результат не устанавливает первопричину перегрева"}, true, nil
	case "archive_timeline":
		if !v.State.Archive {
			return nil, true, errors.New("сначала восстановите архив")
		}
		return map[string]any{"facts": v.Facts}, true, nil
	case "check_evidence":
		return map[string]any{"confirmed": v.State.Investigated, "ready": v.HasFact("damaged_filter") && v.HasFact("impact_record") && v.HasFact("archive_chronology"), "next_check": "сопоставьте найденные технические улики и архив"}, true, nil
	}
	return nil, false, nil
}
