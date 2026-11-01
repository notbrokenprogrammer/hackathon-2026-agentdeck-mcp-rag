package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"feeders/internal/crew"
	"feeders/internal/mission"
)

func (c Catalog) Bind(ctx context.Context) (context.Context, error) {
	if _, ok := mission.FromContext(ctx); ok || c.ViewProvider == nil {
		return ctx, nil
	}
	v, err := c.ViewProvider(ctx)
	if errors.Is(err, mission.ErrNotJoined) {
		return ctx, nil
	}
	if err != nil {
		return ctx, err
	}
	return mission.WithView(ctx, v), nil
}
func (c Catalog) scenarioTools(id string) []Tool {
	makeTool := func(suffix, desc string, params map[string]any) Tool {
		return tool(id+"_"+suffix, desc+" Только чтение доверенного состояния партии; не выполняет действия.", params)
	}
	out := []Tool{
		makeTool("mission_status", "Текущие ресурсы, цикл, роли и только открытые игроку факты", schema(map[string]any{})),
		makeTool("preview_action", "Точная серверная цена и результат одной операции, включая обычный расход и квесты", schema(map[string]any{"name": map[string]any{"type": "string", "enum": mission.Actions}, "choice": str(128), "path": map[string]any{"type": "array", "minItems": 2, "maxItems": 8, "items": map[string]any{"type": "string", "enum": []string{"А", "Б", "В", "Г"}}}}, "name")),
		makeTool("quest_options", "Разрешённые варианты активного квеста с точным прогнозом; скрытые решения не раскрываются", schema(map[string]any{"quest": map[string]any{"type": "string", "enum": mission.Quests}}, "quest")),
	}
	extra := map[string][][2]string{
		mission.Captain:   {{"rescue_plans", "Сравнить четыре спасательных плана, ограничения и остаток ресурсов"}, {"check_evidence", "Проверить полноту доступных доказательств причины аварии"}, {"door_check", "Проверить межблочную блокировку двери и следующий шаг экипажа"}},
		mission.Engineer:  {{"systems_diagnosis", "Проверить реактор, охлаждение и ресурсные угрозы"}, {"drone_route", "Проверить маршрут ремонтного дрона после открытия датчика"}, {"door_check", "Проверить снятие питания и блокировку двери"}, {"power_audit", "Сравнить экономию питания и разовый заряд на оставшиеся обычные циклы"}},
		mission.Biologist: {{"oxygen_forecast", "Сравнить расход кислорода с оранжереей, без неё и с утечкой"}, {"analyze_samples", "Объяснить только доступные результаты биопроб"}, {"garden_options", "Сравнить сохранение и использование коллекции семян"}, {"memory_check", "Проверить вывод АСТРЫ по доступным пробам и хронологии"}},
		mission.Navigator: {{"archive_timeline", "Показать только открытые записи архива и порядок событий"}, {"compare_navigation", "Сопоставить свежесть навигационных сигналов с картой после осмотра"}, {"drone_route", "Проверить связность маршрута дрона после открытия датчика"}},
	}
	for _, item := range extra[id] {
		params := schema(map[string]any{})
		if item[0] == "drone_route" {
			params = schema(map[string]any{"path": map[string]any{"type": "array", "minItems": 2, "maxItems": 8, "items": map[string]any{"type": "string", "enum": []string{"А", "Б", "В", "Г"}}}}, "path")
		}
		out = append(out, makeTool(item[0], item[1], params))
	}
	return out
}
func (c Catalog) callScenario(ctx context.Context, name string, args json.RawMessage) (any, bool, error) {
	known := false
	var suffix string
	for _, p := range crew.All() {
		for _, t := range c.scenarioTools(p.ID) {
			if t.Name == name {
				known = true
				suffix = strings.TrimPrefix(name, p.ID+"_")
			}
		}
	}
	if !known {
		return nil, false, nil
	}
	v, ok := mission.FromContext(ctx)
	if !ok {
		return nil, true, mission.ErrNotJoined
	}
	fail := func(message string) (any, bool, error) { return nil, true, errors.New(message) }
	switch suffix {
	case "preview_action":
		var a mission.Action
		if err := decode(args, &a); err != nil {
			return nil, true, err
		}
		if a.Name == "" {
			return fail("name required")
		}
		return mission.Preview(v, a), true, nil
	case "quest_options":
		var a struct {
			Quest string `json:"quest"`
		}
		if err := decode(args, &a); err != nil {
			return nil, true, err
		}
		value, err := questOptions(v, a.Quest)
		return value, true, err
	case "drone_route":
		var a struct {
			Path []string `json:"path"`
		}
		if err := decode(args, &a); err != nil {
			return nil, true, err
		}
		if !v.HasFact("drone_sensor") {
			return fail("Сначала откройте показание датчика; опасный узел пока неизвестен")
		}
		valid, safe := mission.CheckRoute(a.Path)
		return map[string]any{"connected": valid, "safe": valid && safe, "forecast": mission.Preview(v, mission.Action{Name: "drone_dispatch", Path: a.Path})}, true, nil
	}
	if err := decode(args, &struct{}{}); err != nil {
		return nil, true, err
	}
	s := v.State
	switch suffix {
	case "mission_status":
		return v, true, nil
	case "rescue_plans":
		return map[string]any{"plans": mission.Plans(v)}, true, nil
	case "systems_diagnosis":
		reports := []string{}
		if !s.Cooling && s.Reactor {
			reports = append(reports, "Работающий реактор без охлаждения отнимает 12 H за обычный цикл")
		}
		if s.E <= 8 {
			reports = append(reports, "Малый запас энергии: сравните доступные операции до подтверждения")
		}
		if s.O <= 16 {
			reports = append(reports, "Критически малый запас кислорода")
		}
		return map[string]any{"state": s, "warnings": reports, "facts": v.Facts}, true, nil
	case "check_evidence":
		missing := []string{}
		for _, id := range []string{"damaged_filter", "impact_record", "archive_chronology"} {
			if !v.HasFact(id) {
				missing = append(missing, id)
			}
		}
		return map[string]any{"ready": len(missing) == 0, "missing_visible_evidence": missing, "confirmed": s.Investigated, "next_check": "Ремонт охлаждения, восстановление архива и обмен открытыми фактами; подтвердить версию отдельной командой игрока"}, true, nil
	case "oxygen_forecast":
		extra := 0
		if s.Quests["leak"] == "active" {
			extra = 2
		}
		return map[string]any{"current_oxygen": s.O, "without_garden_per_cycle": 8 + extra, "with_garden_per_cycle": 3 + extra, "garden_active": s.Garden, "note": "Все числа — игровые единицы; точный прогноз действия даёт preview_action"}, true, nil
	case "analyze_samples":
		if !v.HasFact("no_infection") {
			return fail("Результат биопроб не открыт этой роли")
		}
		return map[string]any{"infection": false, "observed": "Падение освещённости", "does_not_prove": "Проба не устанавливает исходную причину перегрева"}, true, nil
	case "garden_options":
		value, err := questOptions(v, "garden")
		return value, true, err
	case "memory_check":
		if !s.Archive || !v.HasFact("archive_chronology") || !v.HasFact("no_infection") {
			return fail("Нужны восстановленный архив и доступные биологические пробы; обменяйтесь фактами")
		}
		return map[string]any{"supported": false, "explanation": "Снижение температуры после отключения нагрузки не доказывает, что растения вызвали исходный перегрев. Проба исключает инфекцию; первопричина требует технических улик.", "resources_changed": false}, true, nil
	case "archive_timeline":
		if !v.HasFact("archive_chronology") {
			return fail("Восстановите архив или получите открытый факт от оператора")
		}
		out := []mission.Fact{}
		for _, f := range v.Facts {
			if f.ID == "archive_chronology" || f.ID == "impact_record" {
				out = append(out, f)
			}
		}
		return map[string]any{"records": out}, true, nil
	case "compare_navigation":
		if !v.HasFact("navigation_records") {
			return fail("Сначала осмотрите обе записи; их содержание ещё не открыто")
		}
		return map[string]any{"preferred": "signal_a", "criteria": []string{"актуальная отметка времени", "совпадение с независимой звёздной картой"}, "signal_b": "Координаты и время до аварии", "changes_course": false}, true, nil
	case "door_check":
		if s.Quests["door"] == "" {
			return fail("Квест двери не включён")
		}
		next := "Инженер снимает питание насоса"
		if s.PumpOff {
			next = "Капитан открывает дверь"
		}
		if s.DoorOpen {
			next = "Инженер предлагает ремонт, капитан подтверждает"
		}
		return map[string]any{"pump_off": s.PumpOff, "door_open": s.DoorOpen, "next_step": next, "extra_cycle": false}, true, nil
	case "power_audit":
		if !v.HasFact("power_audit") {
			return fail("Сначала откройте аудит нагрузки")
		}
		rows := []map[string]int{}
		for n := 1; n <= 8-s.Cycle; n++ {
			rows = append(rows, map[string]int{"ordinary_cycles": n, "rewire_net_gain": 2*n - 2, "charge_gain": 6})
		}
		return map[string]any{"comparison": rows, "warning": "Мгновенный финальный манёвр не является обычным циклом; награда выбирается один раз"}, true, nil
	}
	return nil, true, ErrUnknownTool
}
func questOptions(v mission.View, q string) (any, error) {
	status, selected := v.State.Quests[q]
	if !selected {
		return nil, errors.New("quest not selected")
	}
	if status != "active" && !(q == "door" || q == "beacon") {
		return nil, errors.New("quest not active or already completed")
	}
	choices := map[string][]mission.Action{
		"leak":    {{Name: "leak_patch"}, {Name: "leak_isolate"}},
		"garden":  {{Name: "garden_preserve"}, {Name: "garden_consume"}},
		"capsule": {{Name: "capsule_recover"}, {Name: "capsule_oxygen"}},
		"power":   {{Name: "power_rewire"}, {Name: "power_charge"}},
		"door":    {{Name: "repair_cooling", Choice: "normal"}, {Name: "repair_cooling", Choice: "force"}},
		"beacon":  {{Name: "prepare_course", Choice: "signal_a"}, {Name: "prepare_course", Choice: "signal_b"}, {Name: "prepare_course", Choice: "manual"}},
	}
	if q == "drone" {
		if !v.HasFact("drone_sensor") {
			return map[string]any{"status": status, "next_check": "Откройте показание датчика дрона", "changes_state": false}, nil
		}
		choices[q] = []mission.Action{{Name: "drone_dispatch", Path: []string{"А", "В", "Г"}}, {Name: "drone_dispatch", Path: []string{"А", "Б", "Г"}}}
	}
	if q == "memory" {
		return map[string]any{"status": status, "next_check": "Сопоставьте восстановленную хронологию и пробу биолога; verify_memory — отдельная команда игрока"}, nil
	}
	out := []mission.Forecast{}
	for _, a := range choices[q] {
		out = append(out, mission.Preview(v, a))
	}
	return map[string]any{"quest": q, "options": out, "decline": "Только капитан может отдельно закрыть квест без операции (кроме утечки, двери и маяка)", "changes_state": false}, nil
}
