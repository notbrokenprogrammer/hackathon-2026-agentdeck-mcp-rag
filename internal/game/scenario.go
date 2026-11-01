// Package game - scenario.go
// Реализация кооперативного сценария «Последний экипаж: Протокол выживания».
// Управляет ресурсами корабля «Горизонт-7» (O2, Энергия, Корпус), игровыми циклами,
// расследованием причин аварии и доступными финальными манёврами.
package game

import (
	"errors"
	"fmt"
	"sync"
)

type GameState struct {
	Version             uint64   `json:"version"`
	Cycle               int      `json:"cycle"`
	MaxCycles           int      `json:"max_cycles"`
	Oxygen              int      `json:"oxygen"`
	Energy              int      `json:"energy"`
	Hull                int      `json:"hull"`
	ReactorActive       bool     `json:"reactor_active"`
	CoolingRepaired     bool     `json:"cooling_repaired"`
	GreenhouseActive    bool     `json:"greenhouse_active"`
	BatteryAvailable    bool     `json:"battery_available"`
	CourseCalculated    bool     `json:"course_calculated"`
	ArchiveRestored     bool     `json:"archive_restored"`
	InvestigationSolved bool     `json:"investigation_solved"`
	GameOver            bool     `json:"game_over"`
	Victory             bool     `json:"victory"`
	VictoryType         string   `json:"victory_type"` // "none", "safe_orbit", "evacuated", "failed"
	UnlockedClues       []string `json:"unlocked_clues"`
	Logs                []string `json:"logs"`
}

type ScenarioEngine struct {
	mu    sync.RWMutex
	state GameState
}

func NewScenarioEngine() *ScenarioEngine {
	return &ScenarioEngine{
		state: GameState{
			Version:             1,
			Cycle:               1,
			MaxCycles:           8,
			Oxygen:              64,
			Energy:              35,
			Hull:                70,
			ReactorActive:       false,
			CoolingRepaired:     false,
			GreenhouseActive:    false,
			BatteryAvailable:    true,
			CourseCalculated:    false,
			ArchiveRestored:     false,
			InvestigationSolved: false,
			GameOver:            false,
			Victory:             false,
			VictoryType:         "none",
			UnlockedClues: []string{
				"temp_spike",   // Инженер видит ранний скачок температуры
				"no_infection", // Биолог знает об отсутствии инфекции
			},
			Logs: []string{
				"АСТРА: Пробуждение экипажа. Корабль на аварийной траектории. Осталось 8 циклов.",
			},
		},
	}
}

func (s *ScenarioEngine) GetState() GameState {
	s.mu.RLock()
	defer s.mu.RUnlock()

	clues := make([]string, len(s.state.UnlockedClues))
	copy(clues, s.state.UnlockedClues)

	logs := make([]string, len(s.state.Logs))
	copy(logs, s.state.Logs)

	cp := s.state
	cp.UnlockedClues = clues
	cp.Logs = logs
	return cp
}

// ExecuteAction применяет командную операцию цикла.
func (s *ScenarioEngine) ExecuteAction(action string) (GameState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.state.GameOver {
		return copyGameState(s.state), errors.New("игра уже завершена")
	}

	switch action {
	case "start_reactor":
		if s.state.ReactorActive {
			return copyGameState(s.state), errors.New("реактор уже работает")
		}
		if s.state.Energy < 8 {
			return copyGameState(s.state), errors.New("недостаточно энергии (требуется 8 E)")
		}
		s.state.Energy -= 8
		s.state.ReactorActive = true
		s.addLog(fmt.Sprintf("Цикл %d: Запущен термоядерный реактор (-8 E).", s.state.Cycle))
		s.advanceCycleAfterAction()

	case "repair_cooling":
		if s.state.CoolingRepaired {
			return copyGameState(s.state), errors.New("контур охлаждения уже исправен")
		}
		if s.state.Energy < 6 {
			return copyGameState(s.state), errors.New("недостаточно энергии (требуется 6 E)")
		}
		s.state.Energy -= 6
		s.state.CoolingRepaired = true
		s.unlockClue("damaged_filter")
		s.addLog(fmt.Sprintf("Цикл %d: Отремонтирован контур охлаждения (-6 E). В фильтре обнаружен осколок метеорита!", s.state.Cycle))
		s.advanceCycleAfterAction()

	case "start_greenhouse":
		if s.state.GreenhouseActive {
			return copyGameState(s.state), errors.New("оранжерея уже запущена")
		}
		if s.state.Energy < 12 {
			return copyGameState(s.state), errors.New("недостаточно энергии (требуется 12 E)")
		}
		s.state.Energy -= 12
		s.state.GreenhouseActive = true
		s.addLog(fmt.Sprintf("Цикл %d: Запущена биосферная оранжерея (-12 E). Расход кислорода снижен.", s.state.Cycle))
		s.advanceCycleAfterAction()

	case "use_battery":
		if !s.state.BatteryAvailable {
			return copyGameState(s.state), errors.New("резервная батарея уже израсходована")
		}
		s.state.BatteryAvailable = false
		s.state.Energy = minInt(100, s.state.Energy+25)
		s.addLog(fmt.Sprintf("Цикл %d: Подключена аварийная батарея (+25 E). Внешний передатчик обесточен.", s.state.Cycle))

	case "restore_archive":
		if s.state.ArchiveRestored {
			return copyGameState(s.state), errors.New("архив АСТРЫ уже восстановлен")
		}
		if s.state.Energy < 2 {
			return copyGameState(s.state), errors.New("недостаточно энергии (требуется 2 E)")
		}
		s.state.Energy -= 2
		s.state.ArchiveRestored = true
		s.unlockClue("impact_mark")
		s.unlockClue("protective_chronology")
		s.addLog(fmt.Sprintf("Цикл %d: Восстановлен архив бортового ИИ (-2 E). Обнаружены отметка удара и хронология защиты.", s.state.Cycle))
		s.advanceCycleAfterAction()

	case "calculate_course":
		if s.state.CourseCalculated {
			return copyGameState(s.state), errors.New("курс уже рассчитан")
		}
		if s.state.Energy < 4 {
			return copyGameState(s.state), errors.New("недостаточно энергии (требуется 4 E)")
		}
		s.state.Energy -= 4
		s.state.CourseCalculated = true
		s.addLog(fmt.Sprintf("Цикл %d: Рассчитан безопасный гравитационный маршрут (-4 E).", s.state.Cycle))
		s.advanceCycleAfterAction()

	case "wait_cycle":
		s.addLog(fmt.Sprintf("Цикл %d: Экипаж завершил обсуждение без энергозатратных операций.", s.state.Cycle))
		s.advanceCycleAfterAction()

	// ФИНАЛЬНЫЕ МАНЁВРЫ
	case "final_emergency":
		if !s.state.ReactorActive {
			return copyGameState(s.state), errors.New("для аварийного запуска реактор должен быть включён")
		}
		if s.state.Energy < 22 {
			return copyGameState(s.state), errors.New("недостаточно энергии (требуется 22 E)")
		}
		s.state.Energy -= 22
		if s.state.CoolingRepaired {
			s.state.Hull -= 10
		} else {
			s.state.Hull -= 45
			s.state.ReactorActive = false
		}
		s.state.GameOver = true
		if s.state.Hull >= 25 {
			s.state.Victory = true
			s.state.VictoryType = "safe_orbit"
			s.addLog("АСТРА: Аварийный манёвр успешен! Корабль вышел на безопасную орбиту.")
		} else {
			s.state.Victory = false
			s.state.VictoryType = "failed"
			s.addLog("АСТРА: Критическое разрушение корпуса при аварийном манёвре.")
		}

	case "final_ecosystem":
		if !s.state.ReactorActive || !s.state.CoolingRepaired || !s.state.GreenhouseActive || !s.state.CourseCalculated {
			return copyGameState(s.state), errors.New("требуется: реактор, охлаждение, оранжерея и рассчитанный курс")
		}
		if s.state.Energy < 18 || s.state.Oxygen < 25 || s.state.Hull < 40 {
			return copyGameState(s.state), errors.New("недостаточно ресурсов: требуется E>=18, O>=25, H>=40")
		}
		if s.state.Cycle > 7 {
			return copyGameState(s.state), errors.New("длительный манёвр требует минимум 2 оставшихся цикла")
		}
		s.state.Energy -= 16
		s.state.GameOver = true
		s.state.Victory = true
		s.state.VictoryType = "safe_orbit"
		s.addLog("АСТРА: Идеальный баланс экосистемы! Корабль благополучно стабилизирован на безопасной орбите.")

	case "final_combined":
		if !s.state.ReactorActive || !s.state.CoolingRepaired || !s.state.BatteryAvailable {
			return copyGameState(s.state), errors.New("требуется: реактор, охлаждение и сохранённая резервная батарея")
		}
		if s.state.Energy < 10 || s.state.Oxygen < 12 || s.state.Hull < 30 {
			return copyGameState(s.state), errors.New("недостаточно ресурсов: требуется E>=10, O>=12, H>=30")
		}
		s.state.Energy -= 10
		s.state.Oxygen -= 12
		s.state.Hull -= 5
		s.state.BatteryAvailable = false
		s.state.GameOver = true
		s.state.Victory = true
		s.state.VictoryType = "safe_orbit"
		s.addLog("АСТРА: Комбинированный импульс батареи завершён! Траектория стабилизирована. Корабль спасён!")

	case "final_evacuation":
		if s.state.Energy < 4 || s.state.Oxygen < 6 || s.state.Hull <= 0 {
			return copyGameState(s.state), errors.New("недостаточно ресурсов для отстрела модуля: требуется E>=4, O>=6")
		}
		s.state.Energy -= 4
		s.state.Oxygen -= 6
		s.state.GameOver = true
		s.state.Victory = true
		s.state.VictoryType = "evacuated"
		s.addLog("АСТРА: Аварийный модуль отделён! Экипаж в безопасности. «Горизонт-7» утрачен.")

	default:
		return copyGameState(s.state), errors.New("неизвестное действие")
	}

	s.state.Version++
	return copyGameState(s.state), nil
}

// SolveInvestigation проверяет 3 улики для подтверждения причины аварии.
func (s *ScenarioEngine) SolveInvestigation(clues []string) (bool, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.state.InvestigationSolved {
		return true, "Причина аварии уже установлена.", nil
	}
	if s.state.GameOver {
		return false, "", errors.New("игра уже завершена")
	}
	for _, clue := range clues {
		unlocked := false
		for _, available := range s.state.UnlockedClues {
			if clue == available {
				unlocked = true
				break
			}
		}
		if !unlocked {
			return false, "", errors.New("сначала найдите выбранные доказательства")
		}
	}

	hasFilter := false
	hasImpact := false
	hasChronology := false

	for _, c := range clues {
		if c == "damaged_filter" {
			hasFilter = true
		}
		if c == "impact_mark" {
			hasImpact = true
		}
		if c == "protective_chronology" {
			hasChronology = true
		}
	}

	if hasFilter && hasImpact && hasChronology {
		s.state.InvestigationSolved = true
		s.state.Version++
		msg := "Причина аварии подтверждена: микрометеорит повредил внешний радиатор, стружка забила фильтр охлаждения, а ИИ АСТРА выполнила защитное отключение оранжереи для спасения реактора!"
		s.addLog("РАССЛЕДОВАНИЕ: " + msg)
		return true, msg, nil
	}

	return false, "Недостаточно доказательств. Требуются: повреждённый фильтр, отметка удара и хронология защиты.", nil
}

func (s *ScenarioEngine) advanceCycleAfterAction() {
	if s.state.GameOver {
		return
	}

	// 2. Расчет притока и расхода энергии: ΔE = 8R - 4 - 3G
	energyDelta := -4
	if s.state.ReactorActive {
		energyDelta += 8
	}
	if s.state.GreenhouseActive {
		energyDelta -= 3
	}
	s.state.Energy = clamp(s.state.Energy+energyDelta, 0, 100)

	// Расчет расхода кислорода: ΔO = -3 с оранжереей или -8 без неё
	o2Delta := -8
	if s.state.GreenhouseActive {
		o2Delta = -3
	}
	s.state.Oxygen = clamp(s.state.Oxygen+o2Delta, 0, 100)

	// Если реактор работает без охлаждения: -12 H
	if s.state.ReactorActive && !s.state.CoolingRepaired {
		s.state.Hull = clamp(s.state.Hull-12, 0, 100)
		s.addLog("ВНИМАНИЕ: Перегрев реактора без охлаждения! Повреждение корпуса (-12 H).")
	}

	// Проверка критических состояний
	if s.state.Oxygen <= 0 || s.state.Energy <= 0 || s.state.Hull <= 0 {
		s.state.GameOver = true
		s.state.Victory = false
		s.state.VictoryType = "failed"
		s.addLog("АСТРА: Критический предел превышен. Системы жизнеобеспечения отказали.")
		return
	}

	// Переход на следующий цикл
	s.state.Cycle++
	if s.state.Cycle > s.state.MaxCycles {
		s.state.GameOver = true
		s.state.Victory = false
		s.state.VictoryType = "failed"
		s.addLog("АСТРА: Истёк 8-й цикл. Корабль вошёл в радиационную зону Юпитера. Протокол выживания провален.")
	} else if s.state.Cycle == 7 {
		s.addLog("АСТРА: Внимание! До опасной зоны осталось 2 цикла. Выберите финальный манёвр!")
	}
}

func (s *ScenarioEngine) unlockClue(clueID string) {
	for _, c := range s.state.UnlockedClues {
		if c == clueID {
			return
		}
	}
	s.state.UnlockedClues = append(s.state.UnlockedClues, clueID)
}

func (s *ScenarioEngine) addLog(entry string) {
	s.state.Logs = append(s.state.Logs, entry)
	if len(s.state.Logs) > 20 {
		s.state.Logs = s.state.Logs[len(s.state.Logs)-20:]
	}
}

func clamp(v, minV, maxV int) int {
	if v < minV {
		return minV
	}
	if v > maxV {
		return maxV
	}
	return v
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func copyGameState(s GameState) GameState {
	s.Logs = append([]string(nil), s.Logs...)
	s.UnlockedClues = append([]string(nil), s.UnlockedClues...)
	return s
}
