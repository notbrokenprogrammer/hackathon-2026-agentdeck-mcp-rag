package mission

import "fmt"

type Action struct {
	Name   string   `json:"name"`
	Choice string   `json:"choice,omitempty"`
	Path   []string `json:"path,omitempty"`
}
type Forecast struct {
	Action       Action   `json:"action"`
	Allowed      bool     `json:"allowed"`
	Reasons      []string `json:"reasons"`
	Role         string   `json:"required_role"`
	Confirmation string   `json:"confirmation"`
	EnergyCost   int      `json:"energy_cost"`
	OxygenCost   int      `json:"oxygen_cost"`
	HullCost     int      `json:"hull_cost"`
	Cycles       int      `json:"cycles"`
	After        State    `json:"after"`
	Warnings     []string `json:"warnings"`
}

var Actions = []string{"start_reactor", "repair_cooling", "start_garden", "use_battery", "restore_archive", "prepare_course", "wait", "emergency_burn", "ecosystem_plan", "combined_plan", "evacuate", "leak_patch", "leak_isolate", "drone_dispatch", "garden_preserve", "garden_consume", "capsule_recover", "capsule_oxygen", "power_rewire", "power_charge"}

func RequiredRole(s State, name string) string {
	switch name {
	case "start_reactor", "repair_cooling", "use_battery", "leak_patch", "leak_isolate", "drone_dispatch", "power_rewire", "power_charge":
		return Engineer
	case "start_garden", "garden_preserve", "garden_consume":
		return Biologist
	case "restore_archive", "prepare_course":
		return Navigator // captain inherits in 1..3-player mode
	default:
		return Captain
	}
}
func mayOperate(v View, role string) bool {
	return v.Owns(role)
}

// Preview is pure: it uses the same transition as a confirmed human command.
// Exact prices, the normal cycle, quest modifiers and endings never come from LLM.
func Preview(v View, a Action) Forecast {
	s := clone(v.State)
	f := Forecast{Action: a, Role: RequiredRole(s, a.Name), Confirmation: "captain", Reasons: []string{}, After: s, Warnings: []string{}}
	require := func(ok bool, reason string) {
		if !ok {
			f.Reasons = append(f.Reasons, reason)
		}
	}
	require(s.Status == "active", "Партия уже завершена")
	require(s.Cycle < 8, "Все восемь циклов использованы")
	require(contains(Actions, a.Name), "Неизвестная операция")
	require(s.O > 0 && s.E > 0 && s.H > 0, "Критическое состояние ресурсов")
	require(a.Choice == "" || a.Name == "prepare_course" || a.Name == "repair_cooling", "choice не поддерживается этой операцией")
	require(len(a.Path) == 0 || a.Name == "drone_dispatch", "path поддерживается только для дрона")
	quest := func(q string) {
		require(s.Quests[q] == "active", "Квест "+q+" не активен или уже завершён")
	}
	normal := true
	f.Cycles = 1
	switch a.Name {
	case "start_reactor":
		f.EnergyCost = 8
		require(!s.Reactor, "Реактор уже включён")
		s.Reactor = true
	case "repair_cooling":
		f.EnergyCost = 6
		require(!s.Cooling, "Охлаждение уже исправно")
		if _, selected := s.Quests["door"]; selected {
			if a.Choice == "force" {
				f.HullCost = 8
				s.Quests["door"] = "forced"
			} else {
				require(a.Choice == "" || a.Choice == "normal", "Выберите normal или force")
				require(s.PumpOff && s.DoorOpen, "Инженер должен снять питание, затем капитан открыть дверь")
				s.Quests["door"] = "coordinated"
			}
		} else {
			require(a.Choice == "" || a.Choice == "normal", "Аварийный привод доступен только в квесте двери")
		}
		s.Cooling = true
	case "start_garden":
		f.EnergyCost = 12
		require(!s.Garden, "Оранжерея уже включена")
		s.Garden = true
	case "use_battery":
		require(s.Battery, "Заряд батареи уже использован")
		s.E += 25
		s.Battery = false
		s.Transmitter = false
		f.Warnings = append(f.Warnings, "Передатчик отключается до конца партии; комбинированный план станет недоступен")
	case "restore_archive":
		f.EnergyCost = 2
		require(!s.Archive, "Архив уже восстановлен")
		s.Archive = true
	case "prepare_course":
		require(!s.Course, "Курс уже рассчитан")
		f.EnergyCost = 4
		if _, selected := s.Quests["beacon"]; selected {
			switch a.Choice {
			case "signal_a", "signal_b":
				require(v.HasFact("navigation_records"), "Сначала осмотрите обе навигационные записи")
				s.Course = a.Choice == "signal_a"
				s.Quests["beacon"] = a.Choice
			case "manual":
				f.EnergyCost = 2
				f.HullCost = 8
				s.Course = true
				s.Quests["beacon"] = "manual"
			default:
				require(false, "Для квеста маяка выберите signal_a, signal_b или manual")
			}
		} else {
			require(a.Choice == "", "Навигационные сигналы относятся к отключённому квесту")
			s.Course = true
		}
	case "wait":
	case "emergency_burn":
		normal = false
		f.EnergyCost = 22
		require(s.Reactor, "Нужен работающий реактор")
		if s.Cooling {
			f.HullCost = 10
		} else {
			f.HullCost = 45
			s.Reactor = false
			f.Warnings = append(f.Warnings, "Без охлаждения реактор остановится")
		}
	case "ecosystem_plan":
		f.EnergyCost = 16
		f.Cycles = 2
		require(s.Reactor && s.Cooling && s.Garden && s.Course, "Нужны реактор, охлаждение, оранжерея и рассчитанный курс")
		require(s.E >= 18 && s.O >= 25 && s.H >= 40, "Нужны E≥18, O≥25 и H≥40")
		require(s.Cycle <= 6, "Для длительного плана нужны два оставшихся цикла")
	case "combined_plan":
		normal = false
		f.EnergyCost = 10
		f.OxygenCost = 12
		f.HullCost = 5
		require(s.Reactor && s.Cooling && s.Battery, "Нужны реактор, охлаждение и неиспользованная батарея")
		require(s.H >= 30, "Нужно H≥30")
		s.Battery = false
	case "evacuate":
		normal = false
		f.EnergyCost = 4
		f.OxygenCost = 6
		require(s.Module, "Аварийный модуль недоступен")
	case "leak_patch":
		quest("leak")
		require(v.HasFact("valves_inspected"), "Сначала осмотрите клапаны")
		f.EnergyCost = 6
		s.Quests["leak"] = "patched"
	case "leak_isolate":
		quest("leak")
		require(v.HasFact("valves_inspected"), "Сначала осмотрите клапаны")
		f.HullCost = 6
		s.Quests["leak"] = "isolated"
	case "drone_dispatch":
		quest("drone")
		f.EnergyCost = 3
		require(v.HasFact("drone_sensor"), "Сначала откройте показание датчика дрона")
		valid, safe := CheckRoute(a.Path)
		require(valid, "Путь должен идти от А до Г по доступным связям")
		if safe {
			s.H += 8
			s.Quests["drone"] = "returned"
		} else {
			s.Quests["drone"] = "lost"
		}
	case "garden_preserve":
		quest("garden")
		f.EnergyCost = 4
		s.O += 5
		s.Quests["garden"] = "propagated"
	case "garden_consume":
		quest("garden")
		f.EnergyCost = 2
		s.O += 12
		s.Quests["garden"] = "consumed"
	case "capsule_recover":
		quest("capsule")
		require(s.Cycle == 4, "Капсула доступна только в пятом цикле")
		f.EnergyCost = 6
		f.HullCost = 4
		s.O += 12
		s.Quests["capsule"] = "samples_saved"
	case "capsule_oxygen":
		quest("capsule")
		require(s.Cycle == 4, "Капсула доступна только в пятом цикле")
		f.EnergyCost = 2
		s.O += 8
		s.Quests["capsule"] = "samples_lost"
	case "power_rewire":
		quest("power")
		require(v.HasFact("power_audit"), "Сначала проведите аудит нагрузки")
		f.EnergyCost = 2
		s.BaseDrain = 2
		s.Quests["power"] = "rewired"
	case "power_charge":
		quest("power")
		require(v.HasFact("power_audit"), "Сначала проведите аудит нагрузки")
		s.E += 6
		s.Quests["power"] = "charged"
	}
	// Prices must be payable BEFORE a reward from this operation is credited.
	require(v.State.E-f.EnergyCost > 0, "После оплаты энергия должна оставаться больше нуля")
	require(v.State.O-f.OxygenCost > 0, "После оплаты кислород должен оставаться больше нуля")
	if a.Name != "emergency_burn" {
		require(v.State.H-f.HullCost > 0, "Нельзя сразу обнулить корпус")
	}
	if len(f.Reasons) > 0 {
		return f
	}
	s.E -= f.EnergyCost
	s.O -= f.OxygenCost
	s.H -= f.HullCost
	// The scenario clamps resource awards to 100 before the normal drain.
	clamp(&s)
	if normal {
		for i := 0; i < f.Cycles; i++ {
			ordinary(&s)
			if s.Status == "lost" {
				break
			}
		}
		if a.Name == "ecosystem_plan" && s.Status != "lost" {
			s.Status = "won"
			s.Ending = "ecosystem_plan"
		}
	} else {
		s.Cycle++
		switch a.Name {
		case "combined_plan":
			s.Status = "won"
			s.Ending = a.Name
		case "evacuate":
			s.Status = "evacuated"
			s.Ending = a.Name
		case "emergency_burn":
			if s.H <= 0 {
				s.Status = "lost"
				s.Ending = "hull"
			} else if s.H >= 25 {
				s.Status = "won"
				s.Ending = a.Name
			} else {
				f.Warnings = append(f.Warnings, "Безопасная орбита не достигнута; при наличии времени возможна эвакуация")
			}
		}
	}
	if s.Status == "active" && s.Cycle >= 8 {
		s.Status = "lost"
		s.Ending = "deadline"
	}
	clamp(&s)
	activate(&s)
	f.Allowed = true
	f.After = s
	return f
}
func ordinary(s *State) {
	if s.Reactor {
		s.E += 8
	}
	s.E -= s.BaseDrain
	if s.Garden {
		s.E -= 3
		s.O -= 3
	} else {
		s.O -= 8
	}
	if s.Quests["leak"] == "active" && s.Cycle >= 1 {
		s.O -= 2
	}
	if s.Reactor && !s.Cooling {
		s.H -= 12
	}
	s.Cycle++
	clamp(s)
	if s.O == 0 || s.E == 0 || s.H == 0 {
		s.Status = "lost"
		s.Ending = "critical_resource"
	}
}
func clamp(s *State) {
	s.O = max(0, min(100, s.O))
	s.E = max(0, min(100, s.E))
	s.H = max(0, min(100, s.H))
}
func activate(s *State) {
	for q, status := range s.Quests {
		if q == "capsule" && s.Cycle >= 5 && (status == "active" || status == "pending") {
			s.Quests[q] = "declined"
			continue
		}
		if status != "pending" {
			continue
		}
		active := q == "leak" && s.Cycle >= 1 || q == "drone" && s.Cooling || (q == "garden" || q == "power") && s.Garden || q == "capsule" && s.Cycle == 4 || q == "memory" && s.Archive
		if active {
			s.Quests[q] = "active"
		}
	}
}
func CheckRoute(path []string) (bool, bool) {
	if len(path) < 2 || len(path) > 8 || path[0] != "А" || path[len(path)-1] != "Г" {
		return false, false
	}
	edges := map[string]bool{"АБ": true, "АВ": true, "БГ": true, "ВГ": true, "БА": true, "ВА": true, "ГБ": true, "ГВ": true}
	safe := true
	for i, node := range path {
		if node == "Б" {
			safe = false
		}
		if i > 0 && !edges[path[i-1]+node] {
			return false, false
		}
	}
	return true, safe
}
func Plans(v View) []Forecast {
	out := []Forecast{}
	for _, name := range []string{"emergency_burn", "ecosystem_plan", "combined_plan", "evacuate"} {
		out = append(out, Preview(v, Action{Name: name}))
	}
	return out
}
func Summary(v View) string {
	s := v.State
	return fmt.Sprintf("Завершено циклов: %d/8. O=%d, E=%d, H=%d. Состояние: %s. Одна подтверждённая операция расходует цикл; просмотр и вопросы бесплатны.", s.Cycle, s.O, s.E, s.H, s.Status)
}
