package game

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"feeders/internal/auth"
	"feeders/internal/model"
	"feeders/internal/realtime"
	"feeders/internal/server"
	"feeders/pkg/protocol"
	"fmt"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
)

// Keep nearby rooms resident before the player crosses a doorway. The former
// 8% cache could not hold even the low-detail cryo + bridge + reactor assets.
const worldBudgetRatio = .25
const worldLimitRatio = .30
const worldPrefetchRadius = 3

type WorldResource struct {
	ID   string `json:"id"`
	Size int    `json:"size"`
	Hash string `json:"hash"`
	Kind string `json:"kind"`
}
type WorldLOD struct {
	Object string   `json:"object"`
	Deps   []string `json:"deps"`
}
type WorldChunk struct {
	NPC   string   `json:"npc,omitempty"`
	Areas []string `json:"areas,omitempty"`
	Role  string   `json:"role,omitempty"`
	CX    int      `json:"cx"`
	CZ    int      `json:"cz"`
	Low   WorldLOD `json:"low"`
	High  WorldLOD `json:"high"`
}
type WorldRoom struct {
	ID   string  `json:"id"`
	X    float64 `json:"x"`
	Z    float64 `json:"z"`
	W    float64 `json:"w"`
	D    float64 `json:"d"`
	Name string  `json:"name"`
}
type StreamWorld struct {
	Version    string          `json:"version"`
	ChunkSize  int             `json:"chunk_size"`
	Bytes      int             `json:"world_bytes"`
	Resources  []WorldResource `json:"resources"`
	Chunks     []WorldChunk    `json:"chunks"`
	Background WorldLOD        `json:"background"`
	Rooms      []WorldRoom     `json:"rooms"`
	dir        string
	byID       map[string]WorldResource
	auth       *auth.Handler
}
type WorldPlan struct {
	Version    string          `json:"version"`
	WorldBytes int             `json:"world_bytes"`
	Budget     int             `json:"budget"`
	Resources  []WorldResource `json:"resources"`
	Chunks     []WorldLOD      `json:"chunks"`
	Background WorldLOD        `json:"background"`
	Position   model.Position  `json:"position"`
}

func (m *Module) WithWorld(dir string, a *auth.Handler) error {
	b, e := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if e != nil {
		return e
	}
	w := &StreamWorld{dir: dir, auth: a, byID: map[string]WorldResource{}}
	if e = json.Unmarshal(b, w); e != nil {
		return e
	}
	total := 0
	for _, r := range w.Resources {
		if !regexp.MustCompile(`^[a-z]+_[a-f0-9]{24}$`).MatchString(r.ID) {
			return fmt.Errorf("invalid resource ID")
		}
		b, e = os.ReadFile(filepath.Join(dir, r.ID+".json"))
		if e != nil {
			return e
		}
		sum := sha256.Sum256(b)
		if len(b) != r.Size || hex.EncodeToString(sum[:]) != r.Hash {
			return fmt.Errorf("world resource failed validation: %s", r.ID)
		}
		if _, exists := w.byID[r.ID]; exists {
			return fmt.Errorf("duplicate world resource")
		}
		w.byID[r.ID] = r
		total += r.Size
	}
	if total != w.Bytes || w.ChunkSize <= 0 {
		return fmt.Errorf("world manifest size mismatch")
	}
	w.Rooms = nil
	for _, a := range deck.Areas {
		if a.Kind == "room" {
			w.Rooms = append(w.Rooms, WorldRoom{ID: a.ID, X: a.X, Z: a.Z, W: a.W, D: a.D, Name: a.Name})
		}
	}
	m.streamWorld = w
	return nil
}
func (m *Module) RegisterRoutes(mux *http.ServeMux) {
	if m.streamWorld == nil {
		return
	}
	m.registerExperimentHTTP(mux)
	w := m.streamWorld
	resolve := func(writer http.ResponseWriter, r *http.Request) (string, bool) {
		u, ok := w.auth.Resolve(r)
		if !ok {
			server.JSON(writer, 401, map[string]string{"message": "Войдите в аккаунт"})
			return "", false
		}
		return u.ID, true
	}
	mux.HandleFunc("GET /api/world/meta", func(out http.ResponseWriter, r *http.Request) {
		if _, ok := resolve(out, r); !ok {
			return
		}
		server.JSON(out, 200, map[string]any{"version": w.Version, "world_bytes": w.Bytes, "budget": int(float64(w.Bytes) * worldBudgetRatio), "limit": int(float64(w.Bytes) * worldLimitRatio), "prefetch_radius": worldPrefetchRadius, "chunk_size": w.ChunkSize, "rooms": w.Rooms})
	})
	mux.HandleFunc("GET /api/world/interest", func(out http.ResponseWriter, r *http.Request) {
		id, ok := resolve(out, r)
		if !ok {
			return
		}
		server.JSON(out, 200, m.worldPlanForView(id, r.URL.Query().Get("view")))
	})
	mux.HandleFunc("GET /api/world/resource/{id}", func(out http.ResponseWriter, r *http.Request) {
		id, ok := resolve(out, r)
		if !ok {
			return
		}
		requested := r.PathValue("id")
		plan := m.worldPlanForView(id, r.URL.Query().Get("view"))
		allowed := false
		for _, v := range plan.Resources {
			if v.ID == requested {
				allowed = true
				break
			}
		}
		if !allowed {
			server.JSON(out, 403, map[string]string{"message": "Ресурс вне разрешённой области"})
			return
		}
		out.Header().Set("Content-Type", "application/json")
		out.Header().Set("Cache-Control", "no-store")
		http.ServeFile(out, r, filepath.Join(w.dir, requested+".json"))
	})
}
func (m *Module) worldPosition(id string) model.Position {
	if w, ok := m.world.(interface{ GetAllPlayersStates() []model.PlayerState }); ok {
		for _, p := range w.GetAllPlayersStates() {
			if p.PlayerID == id {
				return p.Position
			}
		}
	}
	return model.Position{X: -15, Y: 1.7, Z: 0}
}
func (m *Module) worldPlan(id string) WorldPlan {
	p := m.worldPosition(id)
	return m.planAt(p)
}

// Overview can inspect a named compartment without teleporting the player.
// Both the plan and resource authorization resolve the same bounded view.
func (m *Module) worldPlanForView(id, view string) WorldPlan {
	if view != "" {
		for _, area := range deck.Areas {
			if area.ID == view {
				return m.planAt(model.Position{X: area.X, Y: 1.7, Z: area.Z})
			}
		}
	}
	return m.worldPlan(id)
}
func (m *Module) planAt(p model.Position) WorldPlan {
	w := m.streamWorld
	plan := WorldPlan{Version: w.Version, WorldBytes: w.Bytes, Budget: int(float64(w.Bytes) * worldBudgetRatio), Background: w.Background, Position: p, Resources: []WorldResource{}, Chunks: []WorldLOD{}}
	used := map[string]bool{}
	bytes := 0
	add := func(lod WorldLOD) bool {
		extra := 0
		for _, id := range lod.Deps {
			if !used[id] {
				extra += w.byID[id].Size
			}
		}
		if bytes+extra > plan.Budget {
			return false
		}
		for _, id := range lod.Deps {
			if !used[id] {
				used[id] = true
				plan.Resources = append(plan.Resources, w.byID[id])
			}
		}
		bytes += extra
		return true
	}
	add(w.Background)
	cx, cz := int(math.Floor(p.X/float64(w.ChunkSize))), int(math.Floor(p.Z/float64(w.ChunkSize)))
	// Close the current room and both ends of its adjoining passages. The
	// architectural shell is admitted before optional equipment/texture detail.
	areas := map[string]bool{}
	occupied := map[string]bool{}
	direct := map[string]bool{}
	for _, area := range deck.Areas {
		if deckContains(area, p.X, p.Z) {
			areas[area.ID] = true
			occupied[area.ID] = true
		}
	}
	for hop := 0; hop < 2; hop++ {
		next := map[string]bool{}
		for id := range areas {
			next[id] = true
		}
		for _, gate := range deck.Gates {
			touches := false
			for _, id := range gate.Areas {
				if areas[id] {
					touches = true
				}
			}
			if touches {
				for _, id := range gate.Areas {
					next[id] = true
				}
			}
		}
		areas = next
		if hop == 0 {
			direct = next
		}
	}
	near := []WorldChunk{}
	inside := func(c WorldChunk) bool {
		for _, id := range c.Areas {
			if occupied[id] {
				return true
			}
		}
		return false
	}
	adjoining := func(c WorldChunk) bool {
		for _, id := range c.Areas {
			if direct[id] {
				return true
			}
		}
		return false
	}
	for _, c := range w.Chunks {
		shell := false
		if c.Role == "shell" || c.Role == "ceiling" {
			for _, id := range c.Areas {
				if areas[id] {
					shell = true
				}
			}
		}
		connected := false
		for _, id := range c.Areas {
			connected = connected || areas[id]
		}
		if connected || shell || abs(c.CX-cx) <= worldPrefetchRadius && abs(c.CZ-cz) <= worldPrefetchRadius {
			near = append(near, c)
		}
	}
	sort.Slice(near, func(i, j int) bool {
		priority := func(c WorldChunk) int {
			local := inside(c)
			switch {
			case local && (c.Role == "shell" || c.Role == "ceiling"):
				return 0
			case local && c.NPC != "":
				return 1
			case local && c.Role == "equipment":
				return 2
			case adjoining(c) && (c.Role == "shell" || c.Role == "ceiling"):
				// Floors, walls and ceilings across a door must already be
				// resident before crossing it. Decorative current-room panels
				// must not exhaust the budget before this first neighbouring hop.
				return 3
			case adjoining(c) && (c.Role == "equipment" || c.NPC != ""):
				return 4
			case c.Role == "shell" || c.Role == "ceiling":
				return 5
			case local:
				return 6
			case c.Role == "equipment" || c.NPC != "":
				return 7
			default:
				return 8
			}
		}
		pi, pj := priority(near[i]), priority(near[j])
		if pi != pj {
			return pi < pj
		}
		si, sj := near[i].Role == "shell" || near[i].Role == "ceiling", near[j].Role == "shell" || near[j].Role == "ceiling"
		if si != sj {
			return si
		}
		if (near[i].NPC != "") != (near[j].NPC != "") {
			return near[i].NPC != ""
		}
		di, dj := abs(near[i].CX-cx)+abs(near[i].CZ-cz), abs(near[j].CX-cx)+abs(near[j].CZ-cz)
		if di == dj {
			if near[i].NPC != "" && near[j].NPC == "" {
				return true
			}
			if near[j].NPC != "" && near[i].NPC == "" {
				return false
			}
			if near[i].CX == near[j].CX {
				return near[i].CZ < near[j].CZ
			}
			return near[i].CX < near[j].CX
		}
		return di < dj
	})
	// Admit low-detail neighbours first. Upgrade by replacing the dependency set,
	// so obsolete low-detail textures never occupy the high-detail budget.
	admitted := []WorldChunk{}
	for _, c := range near {
		if add(c.Low) {
			plan.Chunks = append(plan.Chunks, c.Low)
			admitted = append(admitted, c)
		}
	}
	for index, c := range admitted {
		// Keep the mandatory enclosure's resource IDs stable across cells. A
		// low/high shell swap would detach a preloaded wall before its replacement
		// arrives. Detail/equipment/NPC upgrades remain independent of this shell.
		if c.Role == "shell" || c.Role == "ceiling" {
			continue
		}
		if c.CX != cx || c.CZ != cz || c.High.Object == c.Low.Object {
			continue
		}
		candidate := map[string]bool{}
		for _, id := range w.Background.Deps {
			candidate[id] = true
		}
		for n, lod := range plan.Chunks {
			if n == index {
				lod = c.High
			}
			for _, id := range lod.Deps {
				candidate[id] = true
			}
		}
		size := 0
		for id := range candidate {
			size += w.byID[id].Size
		}
		if size <= plan.Budget {
			plan.Chunks[index] = c.High
			used = candidate
		}
	}
	plan.Resources = nil
	for _, r := range w.Resources {
		if used[r.ID] {
			plan.Resources = append(plan.Resources, r)
		}
	}
	return plan
}
func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
func (m *Module) teleport(ctx context.Context, s realtime.Session, e protocol.Envelope) {
	var p struct {
		Target string `json:"target"`
	}
	if protocol.DecodePayload(e.Payload, &p) != nil || !m.isReady(s.User().ID) || m.RoomOf(s.User().ID) == "" {
		sendError(s, e, "invalid_target", "Выберите допустимую точку корабля")
		return
	}
	targets := map[string]model.Position{"cryo": {X: -15, Y: 1.7, Z: 0}, "bridge": {X: 0, Y: 1.7, Z: 4.5}, "reactor": {X: 12, Y: 1.7, Z: 2}, "bio": {X: -19, Y: 1.7, Z: -14}, "savinykh": {X: 4.2, Y: 1.7, Z: -.5}, "dzhanibekov": {X: 12, Y: 1.7, Z: 3}, "ryazansky": {X: -19, Y: 1.7, Z: -14}, "krikalev": {X: -5, Y: 1.7, Z: -.5}}
	pos, ok := targets[p.Target]
	if !ok {
		sendError(s, e, "invalid_target", "Неизвестная точка корабля")
		return
	}
	loop, ok := m.world.(*ServerLoop)
	if !ok {
		sendError(s, e, "unavailable", "Перемещение недоступно")
		return
	}
	loop.mu.Lock()
	live := loop.players[s.User().ID]
	if live == nil {
		loop.mu.Unlock()
		return
	}
	live.Position = pos
	live.Dirty = true
	state := model.PlayerState{PlayerID: live.User.ID, Position: pos, Yaw: live.Yaw, LastSeq: live.LastSeq}
	loop.mu.Unlock()
	if err := m.players.SavePlayer(ctx, state); err != nil {
		m.failure(s, e, err)
		return
	}
	s.Send("player.position", e.RequestID, state)
}

func (m *Module) Walkable(x, z float64) bool {
	if m.streamWorld == nil {
		return true
	}
	return deckWalkable(x, z)
}
