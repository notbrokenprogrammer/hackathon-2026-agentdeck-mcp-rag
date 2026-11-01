// Package mission implements the numerical rules of the attached survival
// scenario. Models only receive View; they never own or mutate State.
package mission

import (
	"context"
	"errors"
	"sort"
)

const Captain = "savinykh"
const Engineer = "dzhanibekov"
const Biologist = "ryazansky"
const Navigator = "krikalev"

var ErrNotJoined = errors.New("create or join a mission first")

var Quests = []string{"leak", "drone", "garden", "capsule", "beacon", "power", "memory", "door"}

type State struct {
	Version      uint64            `json:"version"`
	Cycle        int               `json:"cycle"` // completed cycles, 0..8
	O            int               `json:"oxygen"`
	E            int               `json:"energy"`
	H            int               `json:"hull"`
	Reactor      bool              `json:"reactor"`
	Cooling      bool              `json:"cooling"`
	Garden       bool              `json:"garden"`
	Battery      bool              `json:"battery"`
	Course       bool              `json:"course"`
	Transmitter  bool              `json:"transmitter"`
	Module       bool              `json:"module"`
	Archive      bool              `json:"archive"`
	Investigated bool              `json:"investigated"`
	BaseDrain    int               `json:"base_energy_drain"`
	PumpOff      bool              `json:"pump_off"`
	DoorOpen     bool              `json:"door_open"`
	Status       string            `json:"status"` // active, won, evacuated, lost
	Ending       string            `json:"ending,omitempty"`
	Quests       map[string]string `json:"quests"` // pending, active, or chosen outcome
}

func Initial(quests []string) (State, error) {
	s := State{Version: 1, O: 64, E: 35, H: 70, Battery: true, Transmitter: true, Module: true, BaseDrain: 4, Status: "active", Quests: map[string]string{}}
	if len(quests) > 2 {
		return s, errors.New("select at most two optional quests")
	}
	for _, q := range quests {
		if !contains(Quests, q) || s.Quests[q] != "" {
			return s, errors.New("unknown or duplicate quest")
		}
		s.Quests[q] = "pending"
	}
	activate(&s)
	return s, nil
}
func clone(s State) State {
	out := s
	out.Quests = map[string]string{}
	for k, v := range s.Quests {
		out.Quests[k] = v
	}
	return out
}
func contains(v []string, s string) bool {
	for _, x := range v {
		if x == s {
			return true
		}
	}
	return false
}
func sortedKeys(m map[string]bool) []string {
	out := []string{}
	for k, v := range m {
		if v {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

type Fact struct {
	ID     string `json:"id"`
	Text   string `json:"text"`
	Source string `json:"source"`
}
type View struct {
	Learning      any      `json:"learning,omitempty"`
	State         State    `json:"state"`
	Roles         []string `json:"roles"`
	Facts         []Fact   `json:"facts"`
	Players       int      `json:"players"`
	PendingAction *Action  `json:"pending_action,omitempty"`
	ProposedBy    string   `json:"proposed_by,omitempty"`
}

func (v View) FactIDs() []string {
	out := []string{}
	for _, f := range v.Facts {
		out = append(out, f.ID)
	}
	return out
}
func (v View) HasFact(id string) bool { return contains(v.FactIDs(), id) }
func (v View) Owns(role string) bool  { return contains(v.Roles, role) }

type viewKey struct{}

func WithView(ctx context.Context, v View) context.Context {
	return context.WithValue(ctx, viewKey{}, v)
}
func FromContext(ctx context.Context) (View, bool) {
	v, ok := ctx.Value(viewKey{}).(View)
	return v, ok
}

type factRule struct {
	Fact
	roles     []string
	available func(State, map[string]bool) bool
}

var factRules = []factRule{
	{Fact{"temperature_spike", "Температура выросла до падения мощности реактора.", "Сценарий, разделы 1 и 4"}, []string{Engineer}, func(State, map[string]bool) bool { return true }},
	{Fact{"no_infection", "Признаков инфекции в пробах нет; увядание связано с отсутствием света.", "Сценарий, разделы 1 и 4"}, []string{Biologist}, func(State, map[string]bool) bool { return true }},
	{Fact{"shutdown_command", "В журнале есть команда отключения оранжереи с повреждённой отметкой времени.", "Сценарий, раздел 1"}, []string{Captain, Navigator}, func(State, map[string]bool) bool { return true }},
	{Fact{"damaged_filter", "После ремонта в фильтре охлаждения найден посторонний металлический фрагмент.", "Сценарий, раздел 4"}, []string{Engineer}, func(s State, _ map[string]bool) bool { return s.Cooling }},
	{Fact{"archive_chronology", "Перегрев предшествовал защитному отключению нагрузки АСТРОЙ.", "Сценарий, раздел 4"}, []string{Navigator}, func(s State, _ map[string]bool) bool { return s.Archive }},
	{Fact{"impact_record", "Датчик корпуса зарегистрировал попадание перед температурным скачком.", "Сценарий, раздел 4"}, []string{Navigator}, func(s State, _ map[string]bool) bool { return s.Archive }},
	{Fact{"cause_confirmed", "Удар повредил теплообмен; фрагмент засорил фильтр, поток снизился, возник перегрев. Отключение нагрузки было защитой.", "Сценарий, раздел 4"}, nil, func(s State, _ map[string]bool) bool { return s.Investigated }},
	{Fact{"valves_inspected", "Давление падает только при открытом клапане грузового отсека.", "Сценарий, квест 1"}, []string{Engineer, Biologist}, func(_ State, o map[string]bool) bool { return o["valves"] }},
	{Fact{"drone_sensor", "В узле Б опасное напряжение; связи А–Б, А–В, Б–Г, В–Г.", "Сценарий, квест 2"}, []string{Engineer, Navigator}, func(_ State, o map[string]bool) bool { return o["drone_sensor"] }},
	{Fact{"navigation_records", "Сигнал А актуален и согласуется со звёздной картой. У Б старая отметка времени и координаты до аварии.", "Сценарий, квест 5"}, []string{Captain, Navigator}, func(_ State, o map[string]bool) bool { return o["navigation"] }},
	{Fact{"power_audit", "В пустом грузовом отсеке работает забытый нагреватель. Его расход входит в стандартные 4 E.", "Сценарий, квест 6"}, []string{Engineer, Biologist}, func(_ State, o map[string]bool) bool { return o["power_audit"] }},
}

func factsFor(s State, observed, shared map[string]bool, roles []string) []Fact {
	out := []Fact{}
	for _, rule := range factRules {
		if !rule.available(s, observed) {
			continue
		}
		visible := len(rule.roles) == 0 || shared[rule.ID]
		for _, role := range roles {
			visible = visible || contains(rule.roles, role)
		}
		if visible {
			out = append(out, rule.Fact)
		}
	}
	for q, status := range s.Quests {
		if status == "active" {
			out = append(out, Fact{q + "_active", "Активен дополнительный квест: " + q, "Сценарий, раздел 12"})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
func (v View) AccessKey() string {
	key := ""
	for _, r := range v.Roles {
		key += r + ":"
	}
	for _, f := range v.Facts {
		key += f.ID + ":"
	}
	return key
}
