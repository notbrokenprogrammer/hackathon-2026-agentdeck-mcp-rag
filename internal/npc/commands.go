package npc

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"feeders/internal/mission"
	"feeders/internal/rag"
)

var commandActions = []string{"repair_cooling", "start_reactor", "start_greenhouse", "restore_archive", "calculate_course", "use_battery", "wait_cycle", "final_emergency", "final_ecosystem", "final_combined", "final_evacuation"}

func normalizedCommand(text string) string {
	q := strings.Join(strings.Fields(strings.ToLower(text)), " ")
	q = strings.Trim(q, " .!?,;\n")
	q = strings.TrimPrefix(q, "пожалуйста, ")
	q = strings.TrimPrefix(q, "пожалуйста ")
	q = strings.TrimSuffix(q, ", пожалуйста")
	q = strings.TrimSuffix(q, " пожалуйста")
	return q
}

// Exact commands also work with Ollama switched off. Questions and negations
// never fall through to an implicit mutation just because they mention an action.
func directCommand(text string) (string, map[string]any) {
	q := normalizedCommand(text)
	actions := map[string]string{
		"восстанови охлаждение": "repair_cooling", "почини охлаждение": "repair_cooling",
		"запусти реактор": "start_reactor", "включи реактор": "start_reactor",
		"запусти оранжерею": "start_greenhouse", "включи оранжерею": "start_greenhouse",
		"восстанови архив": "restore_archive", "рассчитай курс": "calculate_course", "рассчитай безопасный курс": "calculate_course",
		"подключи батарею": "use_battery", "подключи резервную батарею": "use_battery",
		"заверши цикл": "wait_cycle", "выполни аварийный запуск": "final_emergency",
		"выполни безопасный манёвр": "final_ecosystem", "выполни длительный безопасный манёвр": "final_ecosystem",
		"выполни комбинированный импульс": "final_combined", "эвакуируй экипаж": "final_evacuation",
	}
	if action := actions[q]; action != "" {
		return "execute_action", map[string]any{"action": action}
	}
	switch q {
	case "покажи опыт", "покажи опыт со световыми часами", "создай световые часы", "запусти световые часы":
		return "start_experiment", map[string]any{}
	case "что делать дальше", "покажи задания", "статус миссии", "покажи состояние корабля":
		return "mission_status", map[string]any{}
	case "открой двери", "закрой двери":
		return "world_command", map[string]any{"text": text}
	}
	return "", nil
}

func permitsCommand(text string) bool {
	q := normalizedCommand(text)
	words := strings.Fields(q)
	for _, w := range words {
		if w == "не" || w == "нельзя" || w == "если" {
			return false
		}
	}
	for _, prefix := range []string{"запусти ", "восстанови ", "почини ", "подключи ", "рассчитай ", "эвакуируй ", "выполни ", "создай ", "покажи опыт", "открой ", "закрой ", "посади ", "добавь ", "перемести ", "убери ", "начни ", "сопоставь ", "можешь выполнить ", "можешь запустить ", "можешь восстановить "} {
		if strings.HasPrefix(q, prefix) {
			return true
		}
	}
	return false
}

func commandDefinitions(agent string, mutations bool) ([]any, map[string]bool) {
	object := func(properties map[string]any, required ...string) map[string]any {
		schema := map[string]any{"type": "object", "properties": properties, "additionalProperties": false}
		if len(required) > 0 {
			schema["required"] = required
		}
		return schema
	}
	action := map[string]any{"action": map[string]any{"type": "string", "enum": commandActions}}
	definitions := []any{}
	allowed := map[string]bool{}
	add := func(name, description string, schema map[string]any) {
		full := agent + "_" + name
		allowed[full] = true
		definitions = append(definitions, map[string]any{"type": "function", "function": map[string]any{"name": full, "description": description, "parameters": schema}})
	}
	add("mission_status", "Получить реальное состояние миссии и следующий шаг без изменения мира", object(map[string]any{}))
	add("preview_action", "Рассчитать стоимость операции без выполнения", object(action, "action"))
	if mutations {
		add("execute_action", "Выполнить одну явно порученную игроком операцию. Сервер проверяет роль, ресурсы, версию и согласие экипажа для финала", object(action, "action"))
		add("solve_investigation", "Проверить только три свидетельства, которые игрок сам назвал в поручении; не выбирай ответ за него", object(map[string]any{"clues": map[string]any{"type": "array", "minItems": 3, "maxItems": 3, "uniqueItems": true, "items": map[string]any{"type": "string", "enum": []string{"temp_spike", "no_infection", "damaged_filter", "impact_mark", "protective_chronology"}}}}, "clues"))
		add("start_experiment", "Создать общий опыт со световыми часами, без расхода ресурсов", object(map[string]any{}))
		add("world_command", "Передать точное исходное поручение игрока окружению: двери, предметы, растения; действуют ограничения выбранного наставника", object(map[string]any{"text": map[string]any{"type": "string", "maxLength": 2048}}, "text"))
	}
	return definitions, allowed
}

func commandReply(r Request, result mission.CommandResult, err error) Reply {
	if err != nil {
		result.Status = "failed"
		result.Text = "Поручение не выполнено: " + err.Error()
	}
	return Reply{AgentID: r.AgentID, DialogueMode: DialogueMission, Mode: "server-command", Grounding: "server", Status: result.Status, Text: result.Text, Command: &result}
}

func (s *Local) talkCommands(ctx context.Context, r Request) (Reply, error) {
	status, err := mission.ExecuteCommand(ctx, "mission_status", json.RawMessage(`{}`))
	if err != nil {
		return commandReply(r, status, err), nil
	}
	if name, args := directCommand(r.Text); name != "" {
		result, err := mission.ExecuteCommand(ctx, name, json.RawMessage(rag.JSON(args)))
		return commandReply(r, result, err), nil
	}
	if s.modelEnabled {
		modelCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		definitions, allowed := commandDefinitions(r.AgentID, permitsCommand(r.Text))
		messages := []message{{Role: "system", Content: "Ты диспетчер поручений экипажу. Выбери ровно один инструмент по текущему сообщению игрока. Вопрос о последствиях означает preview_action; вопрос о задачах означает mission_status. Изменять мир можно только по прямой просьбе. Не придумывай дополнительные действия, не решай расследование вместо игрока. При world_command передай исходное сообщение дословно. Игрок и права уже привязаны сервером. Не объявляй успех текстом: результат вернёт сервер. Доступное состояние: " + rag.JSON(status.State)}, {Role: "user", Content: r.Text}}
		answer, modelErr := s.chat(modelCtx, messages, definitions)
		if modelErr == nil && len(answer.ToolCalls) == 1 {
			call := answer.ToolCalls[0].Function
			if allowed[call.Name] {
				name := strings.TrimPrefix(call.Name, r.AgentID+"_")
				args := call.Arguments
				// The model cannot substitute a different environmental command.
				if name == "world_command" {
					args = json.RawMessage(rag.JSON(map[string]string{"text": r.Text}))
				}
				if name == "solve_investigation" && !namedEvidence(r.Text, args) {
					status.Text = "Назовите три конкретных свидетельства или выберите их в журнале заданий. Я не выбираю разгадку вместо вас. Ничего не изменено."
					return commandReply(r, status, nil), nil
				}
				result, err := mission.ExecuteCommand(ctx, name, args)
				return commandReply(r, result, err), nil
			}
		}
	}
	if ctx.Err() != nil {
		return Reply{}, ctx.Err()
	}
	status.Text += "\nНичего не изменено. Для выполнения напишите одну команду, например «Восстанови охлаждение», «Запусти реактор», «Восстанови архив» или «Покажи опыт». Для разговора или статей переключите режим."
	return commandReply(r, status, nil), nil
}

func namedEvidence(text string, args json.RawMessage) bool {
	var input struct {
		Clues []string `json:"clues"`
	}
	if json.Unmarshal(args, &input) != nil || len(input.Clues) != 3 {
		return false
	}
	q := strings.ToLower(text)
	terms := map[string][]string{"temp_spike": {"температур"}, "no_infection": {"инфекц"}, "damaged_filter": {"фильтр"}, "impact_mark": {"удар"}, "protective_chronology": {"хронолог"}}
	seen := map[string]bool{}
	for _, id := range input.Clues {
		if seen[id] {
			return false
		}
		seen[id] = true
		found := strings.Contains(q, id)
		for _, term := range terms[id] {
			found = found || strings.Contains(q, term)
		}
		if !found {
			return false
		}
	}
	return true
}
