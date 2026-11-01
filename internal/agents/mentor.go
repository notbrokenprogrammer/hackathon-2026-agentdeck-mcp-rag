package agents

import (
	"bufio"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"feeders/internal/storage"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

//go:embed corpus/*.jsonl
var mentorCorpus embed.FS

type mentorChunk struct {
	ID         string   `json:"chunk_id"`
	DocumentID string   `json:"document_id"`
	Text       string   `json:"text"`
	Section    string   `json:"section"`
	URL        string   `json:"source_url"`
	Keywords   []string `json:"keywords_ru"`
	Relation   string   `json:"relation_to_person"`
}
type mentorDocument struct {
	ID      string `json:"document_id"`
	Title   string `json:"title"`
	TitleRU string `json:"title_ru"`
}
type mentorMemory struct {
	Seen      map[string]int `json:"seen"`
	LastQuery string         `json:"last_query"`
	LastChunk string         `json:"last_chunk"`
	Actions   []string       `json:"actions"`
	Turns     int            `json:"turns"`
}

// Mentor is a local retrieval agent: no model, network calls or cloud APIs.
// Memory is isolated per authenticated player and atomically saved after a turn.
type Mentor struct {
	mu        sync.Mutex
	chunks    []mentorChunk
	documents map[string]mentorDocument
	memory    map[string]mentorMemory
	path      string
	store     storage.StateStore
}

func NewMentor(dataDir string) (*Mentor, error) {
	m := &Mentor{documents: map[string]mentorDocument{}, memory: map[string]mentorMemory{}, path: filepath.Join(dataDir, "mentor-memory.json")}
	for _, name := range []string{"documents", "chunks"} {
		b, err := mentorCorpus.ReadFile("corpus/" + name + ".jsonl")
		if err != nil {
			return nil, err
		}
		s := bufio.NewScanner(strings.NewReader(string(b)))
		s.Buffer(make([]byte, 4096), 128*1024)
		for s.Scan() {
			if name == "documents" {
				var d mentorDocument
				if err := json.Unmarshal(s.Bytes(), &d); err != nil {
					return nil, err
				}
				m.documents[d.ID] = d
			} else {
				var c mentorChunk
				if err := json.Unmarshal(s.Bytes(), &c); err != nil {
					return nil, err
				}
				m.chunks = append(m.chunks, c)
			}
		}
		if err := s.Err(); err != nil {
			return nil, err
		}
	}
	if len(m.chunks) == 0 {
		return nil, errors.New("mentor corpus is empty")
	}
	if b, err := os.ReadFile(m.path); err == nil {
		if err = json.Unmarshal(b, &m.memory); err != nil {
			return nil, fmt.Errorf("mentor memory: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	if m.memory == nil {
		m.memory = map[string]mentorMemory{}
	}
	return m, nil
}

func terms(s string) []string {
	words := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	out := []string{}
	stop := " как что это про для мне уже ещё еще или где есть было меня тебя почему расскажи объясни пожалуйста подробнее проще дальше "
	for _, w := range words {
		r := []rune(w)
		if len(r) < 3 || strings.Contains(stop, " "+w+" ") {
			continue
		}
		if len(r) > 5 {
			r = r[:5]
		}
		out = append(out, string(r))
	}
	return out
}

// Observe retains the player's actual successful operations, separately from history.
func (m *Mentor) Observe(playerID, action string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	x := m.memory[playerID]
	x.Actions = append(x.Actions, action)
	if len(x.Actions) > 8 {
		x.Actions = x.Actions[len(x.Actions)-8:]
	}
	m.memory[playerID] = x
}

func (m *Mentor) Talk(ctx context.Context, r Request) (Reply, error) {
	if err := ctx.Err(); err != nil {
		return Reply{}, err
	}
	if r.PlayerID == "" || strings.TrimSpace(r.Text) == "" || utf8.RuneCountInString(r.Text) > 1000 {
		return Reply{}, errors.New("вопрос должен содержать от 1 до 1000 символов")
	}
	if r.AgentID != "savinykh" {
		return Reply{}, errors.New("доступен наставник savinykh")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	x := m.memory[r.PlayerID]
	// Copy the map before modifying it so a failed write cannot corrupt prior memory.
	seen := map[string]int{}
	for k, v := range x.Seen {
		seen[k] = v
	}
	x.Seen = seen
	q := strings.ToLower(r.Text)
	followup := strings.Contains(q, "проще") || strings.Contains(q, "подробнее") || strings.Contains(q, "продолж") || strings.Contains(q, "дальше")
	query := r.Text
	if followup && x.LastQuery != "" {
		query = x.LastQuery
	}
	if strings.Contains(q, "дальше") && len(x.Actions) > 0 {
		switch x.Actions[len(x.Actions)-1] {
		case "start_reactor", "repair_cooling":
			query = "охлаждение питание конденсат"
		case "start_greenhouse":
			query = "жизнеобеспечение кислород углекислый"
		case "restore_archive":
			query = "диагностика телеметрия"
		}
	}
	tokens := terms(query)
	type candidate struct {
		c     mentorChunk
		score float64
	}
	rank := []candidate{}
	for _, c := range m.chunks {
		keywords := strings.Join(c.Keywords, " ")
		hay := strings.ToLower(keywords + " " + c.Section + " " + c.Text)
		score := 0.0
		for _, term := range tokens {
			if strings.Contains(strings.ToLower(keywords), term) {
				score += 4
			}
			if strings.Contains(hay, term) {
				df := 0
				for _, other := range m.chunks {
					if strings.Contains(strings.ToLower(strings.Join(other.Keywords, " ")+" "+other.Text), term) {
						df++
					}
				}
				score += 1 + math.Log(float64(len(m.chunks)+1)/float64(df+1))
			}
		}
		if score > 0 {
			rank = append(rank, candidate{c, score / (1 + float64(x.Seen[c.ID])*0.7)})
		}
	}
	if len(rank) == 0 {
		if strings.Contains(q, "привет") || strings.Contains(q, "кто ты") || strings.Contains(q, "начать") {
			rank = append(rank, candidate{m.chunks[0], 1})
		} else {
			return Reply{AgentID: "savinykh", AgentName: "Наставник Савиных", Text: "В локальных материалах нет надёжного ответа на этот вопрос. Я могу помочь с опытом восстановления «Салюта-7», диагностикой, питанием, охлаждением и жизнеобеспечением. Уточните, какую систему изучаем."}, nil
		}
	}
	sort.SliceStable(rank, func(i, j int) bool { return rank[i].score > rank[j].score })
	c := rank[0].c
	if strings.Contains(q, "проще") && x.LastChunk != "" {
		for _, item := range m.chunks {
			if item.ID == x.LastChunk {
				c = item
				break
			}
		}
	}
	text := c.Text
	mode := "summary"
	if translated, ok := mentorTranslations[c.DocumentID]; ok {
		text = translated
		mode = "translated_summary"
	}
	if !strings.Contains(q, "подробнее") {
		parts := strings.SplitAfter(text, ". ")
		if len(parts) > 4 {
			text = strings.Join(parts[:4], "")
		}
	}
	if strings.Contains(q, "проще") {
		parts := strings.SplitAfter(text, ". ")
		if len(parts) > 2 {
			text = strings.Join(parts[:2], "") + ". Коротко: сначала измеряем состояние системы, затем проверяем результат изменения."
		}
	}
	prefix := "По проверенным материалам о реальном опыте Савиных:\n\n"
	if c.Relation == "role_background" {
		prefix = "Инженерная справка NASA. Это отдельный источник, не работа и не личное воспоминание Савиных.\n\n"
	}
	if x.Seen[c.ID] > 0 {
		prefix = "Эту тему мы уже затрагивали. Вернёмся к ключевым связям между системами:\n\n" + prefix
	} else if x.Turns > 0 {
		prefix = "Продолжим с новым фрагментом источников.\n\n" + prefix
	}
	if len(x.Actions) > 0 && strings.Contains(q, "дальше") {
		last := x.Actions[len(x.Actions)-1]
		next := "Сопоставьте показатели ресурсов на пульте и обсудите следующую операцию с экипажем."
		switch last {
		case "start_reactor":
			next = "Проверьте статус охлаждения и изменение целостности корпуса. Затем сравните расход кислорода с запасом энергии."
		case "repair_cooling":
			next = "Сравните запуск питания и оранжереи: какой дефицит быстрее станет критическим? Проверьте стоимость операции на пульте."
		case "start_greenhouse":
			next = "Сравните новый расход кислорода и энергии. Оцените, сколько циклов оставляет этот баланс."
		case "restore_archive":
			next = "Откройте журнал улик и сопоставьте независимые свидетельства. Последовательность событий ещё не доказывает причинную связь."
		}
		prefix = "Для вашей игровой партии, после операции «" + actionName(last) + "»: " + next + " Исторический пример не раскрывает причину игровой аварии.\n\n" + prefix
	}
	x.Seen[c.ID]++
	x.LastQuery = query
	x.LastChunk = c.ID
	x.Turns++
	if err := ctx.Err(); err != nil {
		return Reply{}, err
	}
	previous, existed := m.memory[r.PlayerID]
	m.memory[r.PlayerID] = x
	if err := m.save(); err != nil {
		if existed {
			m.memory[r.PlayerID] = previous
		} else {
			delete(m.memory, r.PlayerID)
		}
		return Reply{}, err
	}
	d := m.documents[c.DocumentID]
	title := d.Title
	if d.TitleRU != "" {
		title = d.TitleRU
	}
	return Reply{AgentID: "savinykh", AgentName: "Наставник Савиных", Text: prefix + text, Citation: &Citation{SourceTitle: title, Section: c.Section, Excerpt: text, Confidence: "source_based", SourceURL: c.URL, DocumentID: c.DocumentID, ContentMode: mode}}, nil
}

func (m *Mentor) save() error {
	if m.store != nil {
		return m.store.SaveState("mentor_memory_v1", m.memory)
	}
	if err := os.MkdirAll(filepath.Dir(m.path), 0700); err != nil {
		return err
	}
	b, err := json.Marshal(m.memory)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(m.path), "mentor-*.tmp")
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
	return os.Rename(name, m.path)
}

func actionName(a string) string {
	switch a {
	case "start_reactor":
		return "запуск реактора"
	case "repair_cooling":
		return "ремонт охлаждения"
	case "start_greenhouse":
		return "запуск оранжереи"
	case "restore_archive":
		return "восстановление архива"
	default:
		return a
	}
}

// Translated summaries are intentionally separate from the English corpus originals.
var mentorTranslations = map[string]string{
	"savinykh_doc_009": "Жизнеобеспечение объединяет контроль давления, вентиляцию, кислород, очистку воздуха и водооборот. Добавить кислород и удалить углекислый газ — разные задачи. Вода проходит фильтрацию и каталитическую обработку; датчики проверяют результат, а непригодная вода возвращается на повторную очистку. Это пример того, почему восстановление одного узла ещё не доказывает безопасность всей системы.",
	"savinykh_doc_010": "При проектировании жизнеобеспечения проверяют не только отдельные приборы, но и их совместимость. В замкнутой кабине загрязнения могут накапливаться, воздух и вода используются многократно, а возможности спасения ограничены. Поэтому нужны измерения среды, испытания и контроль работы после ремонта.",
	"savinykh_doc_011": "Испытания жизнеобеспечения должны охватывать разные этапы полёта. Сеть приборов, труб, ёмкостей и датчиков одновременно поддерживает температуру, давление, кислород и удаление углекислого газа. Исправность одного компонента не подтверждает исправность всего контура.",
	"savinykh_doc_012": "Для длительных полётов уменьшают зависимость от расходных запасов и снабжения с Земли. Это требует надёжного возврата воды и кислорода, ремонтопригодности и разумных затрат энергии. Материал NGLS описывает цели и разработку технологий; он не означает, что все возможности уже реализованы на каждой станции.",
	"savinykh_doc_013": "Температура зависит от полученного солнечного тепла, собственного тепловыделения приборов и отдачи тепла излучением. Работающее электрооборудование создаёт дополнительную тепловую нагрузку. Пассивные средства — изоляция, покрытия и теплопроводящие связи; активные включают нагреватели и устройства охлаждения. Для исправной работы нужно удерживать оборудование в допустимом диапазоне температур.",
	"savinykh_doc_014": "В 2010 году отказ насоса вывел из строя половину системы охлаждения МКС. Другой насос продолжал охлаждать электронику, пока экипаж готовил замену. Установка запасного узла требовала соединения электрических и жидкостных линий. Сохранить работу за счёт резерва и восстановить отказавший контур — два разных результата.",
	"savinykh_doc_015": "На Orion контроль атмосферы включает удаление углекислого газа, влаги и следовых примесей, а не только подачу кислорода. Регенерируемый поглотитель попеременно работает с воздухом кабины и восстанавливается с использованием вакуума. Контроль температуры, влажности и давления помогает обнаружить небезопасные условия. Это современная система Orion, а не оборудование «Салюта-7».",
	"savinykh_doc_016": "В 2023 году NASA сообщило о демонстрации возврата 98 процентов воды в определённой системе МКС. Дополнительный процессор извлекал влагу из рассола, оставшегося после обработки мочи. После сбора вода всё равно проходила очистку и проверку датчиками. Процент относится к конкретной конфигурации; переносить его на любой корабль нельзя.",
	"savinykh_doc_017": "Материалы, рабочие жидкости и реагенты могут выделять примеси в воздух замкнутой кабины. Защита начинается с выбора материалов и герметизации; активная очистка работает с оставшейся нагрузкой. Утечка способна повредить и человеку, и оборудованию жизнеобеспечения. Поэтому замена жидкости или детали требует проверки совместимости.",
	"savinykh_doc_018": "NASA описывает замену стареющих никель-водородных аккумуляторов МКС на литий-ионные. Работы включали многолетнюю разработку и четырнадцать выходов в открытый космос. Эта краткая заметка не содержит алгоритмов зарядки или порогов отказа. Она относится к обслуживанию МКС, а не к спасению «Салюта-7».",
	"savinykh_doc_019": "В дальнем полёте редкое снабжение делает повторное использование ресурсов особенно важным. Обитаемость включает воздух, воду, отходы, мониторинг среды, здоровье экипажа и пожарную безопасность. Жилой модуль сам по себе не обеспечивает эти функции. Публикация описывает проектные цели, а не универсальную инструкцию эксплуатации.",
	"savinykh_doc_020": "Архитектура Gateway разделяет задачи между модулями, но связывает их общими ресурсами. Энергия, хранение заряда, распределение мощности и тепловой режим поддерживают работу обитаемого отсека. Программное управление должно учитывать ресурсы и отказы вместе. Это описание планируемой архитектуры, а не исторический опыт Савиных.",
}

func (m *Mentor) WithStore(store storage.StateStore) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.store = store
	if err := store.LoadState("mentor_memory_v1", &m.memory); err != nil {
		if !errors.Is(err, storage.ErrNotFound) {
			return err
		}
		return m.save()
	}
	return nil
}
