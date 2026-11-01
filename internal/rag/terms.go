package rag

import "strings"

// Small Russian suffix normalization, not a full lemmatizer or semantic search.
func normalizedTerm(word string) string {
	r := []rune(word)
	if len(r) < 5 {
		return word
	}
	for _, suffix := range []string{"иями", "ями", "ами", "ого", "ему", "ому", "ыми", "ими", "иях", "ах", "ях", "иям", "ием", "ией", "ить", "ать", "ять", "ется", "ются", "лся", "лась", "лось", "ия", "ие", "ии", "ию", "ый", "ий", "ой", "ая", "яя", "ое", "ее", "ые", "ую", "юю", "ов", "ев", "ам", "ом", "ем", "а", "я", "ы", "и", "ь", "у", "ю", "е"} {
		if strings.HasSuffix(word, suffix) && len(r)-len([]rune(suffix)) >= 3 {
			return strings.TrimSuffix(word, suffix)
		}
	}
	return word
}

var stopwords = map[string]bool{"и": true, "а": true, "но": true, "в": true, "во": true, "на": true, "с": true, "со": true, "к": true, "у": true, "о": true, "об": true, "для": true, "из": true, "это": true, "как": true, "что": true, "ли": true, "мне": true, "мы": true, "вы": true, "ты": true, "он": true, "она": true, "оно": true, "они": true, "не": true, "нет": true, "есть": true, "по": true, "же": true, "бы": true, "можно": true, "нужно": true, "расскажи": true, "пожалуйста": true}
