package mission

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"feeders/internal/crew"
	"feeders/pkg/protocol"
)

type Member struct {
	PlayerID string   `json:"player_id"`
	Roles    []string `json:"roles"`
}
type proposal struct {
	Action Action `json:"action"`
	Player string `json:"player"`
}
type receipt struct {
	ID      string `json:"id"`
	Player  string `json:"player"`
	Body    string `json:"body"`
	Version uint64 `json:"version"`
}
type record struct {
	State    State           `json:"state"`
	Players  int             `json:"players"`
	Members  []Member        `json:"members"`
	Observed map[string]bool `json:"observed"`
	Shared   map[string]bool `json:"shared"`
	Proposal *proposal       `json:"proposal,omitempty"`
	Receipts []receipt       `json:"receipts"`
}
type Service struct {
	mu   sync.Mutex
	path string
	data *record
}
type Command struct {
	ID       string   `json:"command_id"`
	Version  uint64   `json:"version"`
	Type     string   `json:"type"` // propose, confirm, observe, share_fact, investigate, decline, power_off, open_door
	Action   Action   `json:"action,omitempty"`
	Value    string   `json:"value,omitempty"`
	Evidence []string `json:"evidence,omitempty"`
	Target   string   `json:"target_player,omitempty"`
}
type Result struct {
	View           View   `json:"view"`
	Duplicate      bool   `json:"duplicate"`
	AppliedVersion uint64 `json:"applied_version"`
}

// One host owns this file. Atomic file replacement keeps the last complete turn.
// Models receive read-only views; only the separate human command path calls Apply.
func New(path string) (*Service, error) {
	s := &Service{path: path}
	if path == "" {
		return s, nil
	}
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	r, err := decodeRecord(b)
	if err != nil {
		return nil, err
	}
	s.data = r
	return s, nil
}
func decodeRecord(b []byte) (*record, error) {
	if len(b) > 2<<20 {
		return nil, errors.New("mission file too large")
	}
	var r record
	if err := protocol.DecodePayload(b, &r); err != nil {
		return nil, err
	}
	s := r.State
	if r.Players < 1 || r.Players > 4 || len(r.Members) == 0 || len(r.Members) > r.Players || s.Cycle < 0 || s.Cycle > 8 || s.Version == 0 || s.O < 0 || s.O > 100 || s.E < 0 || s.E > 100 || s.H < 0 || s.H > 100 || (s.BaseDrain != 2 && s.BaseDrain != 4) || !contains([]string{"active", "won", "lost", "evacuated"}, s.Status) || r.Observed == nil || r.Shared == nil || s.Quests == nil || len(r.Receipts) > 512 {
		return nil, errors.New("invalid mission snapshot")
	}
	roles := map[string]bool{}
	ids := map[string]bool{}
	for _, m := range r.Members {
		if m.PlayerID == "" || ids[m.PlayerID] {
			return nil, errors.New("invalid member")
		}
		ids[m.PlayerID] = true
		for _, role := range m.Roles {
			if _, ok := crew.Find(role); !ok || roles[role] {
				return nil, errors.New("invalid role assignments")
			}
			roles[role] = true
		}
	}
	return &r, nil
}
func copyRecord(r *record) *record {
	b, _ := json.Marshal(r)
	var out record
	_ = json.Unmarshal(b, &out)
	return &out
}
func (s *Service) save(next *record) error {
	if s.path != "" {
		if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
			return err
		}
		f, err := os.CreateTemp(filepath.Dir(s.path), ".mission-*")
		if err != nil {
			return err
		}
		defer os.Remove(f.Name())
		b, err := json.Marshal(next)
		if err == nil {
			_, err = f.Write(b)
		}
		if err == nil {
			err = f.Sync()
		}
		closeErr := f.Close()
		if err == nil {
			err = closeErr
		}
		if err != nil {
			return err
		}
		if err = os.Rename(f.Name(), s.path); err != nil {
			return err
		}
	}
	s.data = next
	return nil
}
func seatRoles(players, seat int) []string {
	if players == 1 {
		return []string{Captain, Engineer, Biologist, Navigator}
	}
	if players == 2 {
		if seat == 0 {
			return []string{Captain, Engineer}
		}
		return []string{Biologist, Navigator}
	}
	if players == 3 {
		return [][]string{{Captain, Navigator}, {Engineer}, {Biologist}}[seat]
	}
	return [][]string{{Captain}, {Engineer}, {Biologist}, {Navigator}}[seat]
}
func (s *Service) Create(ctx context.Context, player string, players int, quests []string) (View, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return View{}, err
	}
	if s.data != nil {
		return View{}, errors.New("mission already exists; reset is an explicit offline administrative operation")
	}
	if strings.TrimSpace(player) == "" || len(player) > 128 || players < 1 || players > 4 {
		return View{}, errors.New("player and players=1..4 required")
	}
	state, err := Initial(quests)
	if err != nil {
		return View{}, err
	}
	next := &record{State: state, Players: players, Members: []Member{{player, seatRoles(players, 0)}}, Observed: map[string]bool{}, Shared: map[string]bool{}, Receipts: []receipt{}}
	if err = s.save(next); err != nil {
		return View{}, err
	}
	return view(next, player)
}
func (s *Service) Join(ctx context.Context, player string, role string) (View, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return View{}, err
	}
	if s.data == nil {
		return View{}, ErrNotJoined
	}
	for _, m := range s.data.Members {
		if m.PlayerID == player {
			return view(s.data, player)
		}
	}
	if strings.TrimSpace(player) == "" || len(player) > 128 {
		return View{}, errors.New("invalid player")
	}
	next := copyRecord(s.data)
	if next.State.Status != "active" || len(next.Members) >= next.Players {
		return View{}, errors.New("no available mission seat")
	}
	for seat := 1; seat < next.Players; seat++ {
		roles := seatRoles(next.Players, seat)
		occupied := false
		for _, m := range next.Members {
			for _, r := range roles {
				occupied = occupied || contains(m.Roles, r)
			}
		}
		if occupied || role != "" && !contains(roles, role) {
			continue
		}
		next.Members = append(next.Members, Member{player, roles})
		next.State.Version++
		if err := s.save(next); err != nil {
			return View{}, err
		}
		return view(next, player)
	}
	return View{}, errors.New("requested role unavailable")
}
func view(r *record, player string) (View, error) {
	for _, m := range r.Members {
		if m.PlayerID == player {
			v := View{State: clone(r.State), Roles: append([]string(nil), m.Roles...), Facts: factsFor(r.State, r.Observed, r.Shared, m.Roles), Players: r.Players}
			if r.Proposal != nil {
				a := r.Proposal.Action
				a.Path = append([]string(nil), a.Path...)
				v.PendingAction = &a
				v.ProposedBy = r.Proposal.Player
			}
			return v, nil
		}
	}
	return View{}, ErrNotJoined
}
func (s *Service) View(player string) (View, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data == nil {
		return View{}, ErrNotJoined
	}
	return view(s.data, player)
}
func (s *Service) PlayerIDs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []string{}
	if s.data != nil {
		for _, m := range s.data.Members {
			out = append(out, m.PlayerID)
		}
	}
	return out
}

// ReadView binds a trusted local MCP process to the same saved mission and player.
// The actor ID is a process flag, never a model-controlled tool argument.
func ReadView(path, player string) (View, error) {
	s, err := New(path)
	if err != nil {
		return View{}, err
	}
	return s.View(player)
}

func (s *Service) Apply(ctx context.Context, player string, c Command) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if s.data == nil {
		return Result{}, ErrNotJoined
	}
	v, err := view(s.data, player)
	if err != nil {
		return Result{}, err
	}
	if strings.TrimSpace(c.ID) == "" || len(c.ID) > 128 || len(c.Evidence) > 12 || len(c.Value) > 128 || len(c.Target) > 128 || len(c.Action.Path) > 8 || len(c.Action.Name) > 64 || len(c.Action.Choice) > 128 || len(c.Type) > 32 {
		return Result{}, errors.New("invalid command")
	}
	for _, id := range c.Evidence {
		if len(id) > 128 {
			return Result{}, errors.New("evidence id too long")
		}
	}
	for _, node := range c.Action.Path {
		if !contains([]string{"А", "Б", "В", "Г"}, node) {
			return Result{}, errors.New("invalid route node")
		}
	}
	c.Action.Path = append([]string(nil), c.Action.Path...)
	body, _ := json.Marshal(struct {
		Type     string
		Action   Action
		Value    string
		Evidence []string
		Target   string
	}{c.Type, c.Action, c.Value, c.Evidence, c.Target})
	for _, old := range s.data.Receipts {
		if old.ID == c.ID && old.Player == player {
			if old.Body != string(body) {
				return Result{}, errors.New("command_id reused with different payload")
			}
			return Result{v, true, old.Version}, nil
		}
	}
	if c.Version != v.State.Version {
		return Result{}, fmt.Errorf("stale version: current %d", v.State.Version)
	}
	if v.State.Status != "active" {
		return Result{}, errors.New("mission is finished")
	}
	next := copyRecord(s.data)
	switch c.Type {
	case "propose":
		f := Preview(v, c.Action)
		if !f.Allowed {
			return Result{}, errors.New(strings.Join(f.Reasons, "; "))
		}
		if !mayOperate(v, f.Role) {
			return Result{}, errors.New("specialist role required")
		}
		next.Proposal = &proposal{c.Action, player}
	case "confirm":
		if !v.Owns(Captain) {
			return Result{}, errors.New("captain confirmation required")
		}
		a := c.Action
		if a.Name == "" {
			if next.Proposal == nil {
				return Result{}, errors.New("no proposed operation")
			}
			a = next.Proposal.Action
		} else if !mayOperate(v, RequiredRole(v.State, a.Name)) {
			return Result{}, errors.New("specialist must propose this operation first")
		}
		// A confirmed specialist proposal may use observations visible to that specialist.
		all := v
		all.Facts = factsFor(next.State, next.Observed, next.Shared, []string{Captain, Engineer, Biologist, Navigator})
		f := Preview(all, a)
		if !f.Allowed {
			return Result{}, errors.New(strings.Join(f.Reasons, "; "))
		}
		next.State = f.After
		next.Proposal = nil
	case "observe":
		if err := observe(next, v, c.Value); err != nil {
			return Result{}, err
		}
	case "share_fact":
		if !v.HasFact(c.Value) {
			return Result{}, errors.New("fact is not visible to you")
		}
		next.Shared[c.Value] = true
	case "transfer_role":
		if !v.Owns(Captain) {
			return Result{}, errors.New("captain required")
		}
		if _, ok := crew.Find(c.Value); !ok {
			return Result{}, errors.New("unknown role")
		}
		target := c.Target
		if target == "" {
			target = player
		}
		exists := false
		for _, m := range next.Members {
			exists = exists || m.PlayerID == target
		}
		if !exists {
			return Result{}, errors.New("target must already be a mission member")
		}
		for i := range next.Members {
			roles := []string{}
			for _, role := range next.Members[i].Roles {
				if role != c.Value {
					roles = append(roles, role)
				}
			}
			if next.Members[i].PlayerID == target {
				roles = append(roles, c.Value)
			}
			next.Members[i].Roles = roles
		}
		next.Proposal = nil
	case "investigate":
		if !v.Owns(Captain) && !v.Owns(Navigator) {
			return Result{}, errors.New("captain or navigator required")
		}
		if c.Value != "impact_cooling" {
			return Result{}, errors.New("hypothesis not supported; inspect cooling and chronology")
		}
		for _, id := range []string{"damaged_filter", "impact_record", "archive_chronology"} {
			if !v.HasFact(id) || !contains(c.Evidence, id) {
				return Result{}, errors.New("missing visible evidence: " + id)
			}
		}
		next.State.Investigated = true
	case "decline":
		if !v.Owns(Captain) {
			return Result{}, errors.New("captain required")
		}
		if !contains([]string{"drone", "garden", "capsule", "power"}, c.Value) || next.State.Quests[c.Value] != "active" {
			return Result{}, errors.New("quest cannot be declined now")
		}
		next.State.Quests[c.Value] = "declined"
	case "power_off":
		if !v.Owns(Engineer) || next.State.Cooling || next.State.Quests["door"] == "" {
			return Result{}, errors.New("door quest and engineer required")
		}
		next.State.PumpOff = true
		next.State.Quests["door"] = "active"
	case "open_door":
		if !v.Owns(Captain) || next.State.Cooling || next.State.Quests["door"] == "" || !next.State.PumpOff {
			return Result{}, errors.New("engineer must remove pump power before captain opens door")
		}
		next.State.DoorOpen = true
	case "verify_memory":
		if next.State.Quests["memory"] != "active" || !v.HasFact("archive_chronology") || !v.HasFact("no_infection") {
			return Result{}, errors.New("memory quest requires chronology and biological sample shared by crew")
		}
		next.State.Quests["memory"] = "verified"
	default:
		return Result{}, errors.New("unknown command type")
	}
	next.State.Version++
	next.Receipts = append(next.Receipts, receipt{c.ID, player, string(body), next.State.Version})
	if len(next.Receipts) > 512 {
		next.Receipts = next.Receipts[len(next.Receipts)-512:]
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if err := s.save(next); err != nil {
		return Result{}, err
	}
	v, _ = view(next, player)
	return Result{v, false, next.State.Version}, nil
}
func observe(r *record, v View, name string) error {
	role, quest := Engineer, ""
	switch name {
	case "valves":
		quest = "leak"
		if v.Owns(Biologist) {
			role = Biologist
		}
	case "drone_sensor":
		quest = "drone"
		if v.Owns(Navigator) {
			role = Navigator
		}
	case "navigation":
		role = Navigator
		quest = "beacon"
	case "power_audit":
		quest = "power"
		if v.Owns(Biologist) {
			role = Biologist
		}
	default:
		return errors.New("unknown observation")
	}
	if !v.Owns(role) || (r.State.Quests[quest] != "active" && !(quest == "beacon" && r.State.Quests[quest] == "pending")) {
		return errors.New("observation unavailable for this role or quest")
	}
	r.Observed[name] = true
	if quest == "beacon" {
		r.State.Quests[quest] = "active"
	}
	return nil
}
