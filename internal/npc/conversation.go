package npc

import (
	"context"
	"regexp"
	"strings"
	"unicode"

	"feeders/internal/mission"
	"feeders/internal/rag"
)

func socialQuestion(text string) bool {
	words := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	switch strings.Join(words, " ") {
	case "привет", "здравствуй", "здравствуйте", "добрый день", "добрый вечер", "привет как дела", "как дела", "спасибо", "благодарю", "до свидания", "пока", "ты здесь", "ты меня слышишь", "можно с тобой поговорить", "кто ты", "расскажи о себе", "что ты умеешь", "чем ты можешь помочь", "hello", "hi", "thanks":
		return true
	}
	return false
}

// Normal tutoring may paraphrase visible sources. Investigation answers retain
// their stricter extractive boundary: the model must not turn a player's guess
// into a newly confirmed clue or disclose a locked mission solution. Check the
// generated answer too, so an unrelated prompt cannot bypass this boundary.
var protectedMissionTerms = regexp.MustCompile(`(?i)(причин|авари|винов|саботаж|астр[аы]|инфекц|анализ.{0,15}проб|результат.{0,15}проб|фильтр|фрагмент|микрометеор|попадан|удар|датчик|хронолог|навигационн.{0,25}(сигнал|запис)|уз(ел|ле|ла)\s+[абвг]|опасн.{0,20}напряжени|нагревател|клапан|impact_record|cause_confirmed|drone_sensor|navigation_records)`)

func protectedMissionAnswer(ctx context.Context, question, answer string, evidence []rag.Chunk) bool {
	_, playing := mission.FromContext(ctx)
	return (playing || hasScenarioEvidence(evidence)) && protectedMissionTerms.MatchString(question+"\n"+answer)
}

func previousAccessKey(scope string) string {
	_, access, _ := strings.Cut(scope, ":")
	return access
}
