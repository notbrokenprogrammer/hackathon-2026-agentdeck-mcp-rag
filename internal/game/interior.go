package game

import (
	"encoding/json"
	"feeders/internal/model"
	"feeders/web"
	"math"
	"time"
)

type deckRect struct {
	ID, Name, Kind string
	X, Z, W, D     float64
}
type deckGate struct {
	ID, Name       string
	X, Z, Rotation float64
	Areas          []string
	Door           bool
}
type deckLayout struct {
	Height, Wall, Radius, DoorWidth, DoorDuration float64
	Areas                                         []deckRect
	Gates                                         []deckGate
	Obstacles                                     []deckRect
}

var deck = func() deckLayout {
	var d deckLayout
	if err := json.Unmarshal(web.InteriorLayout, &d); err != nil {
		panic(err)
	}
	return d
}()
var deckWalls = func() []deckRect {
	var walls []deckRect
	for _, a := range deck.Areas {
		for axis := 0; axis < 2; axis++ {
			for _, sign := range []float64{-1, 1} {
				center, length, fixed := a.Z, a.D, a.X+sign*a.W/2
				if axis == 1 {
					center, length, fixed = a.X, a.W, a.Z+sign*a.D/2
				}
				cuts := []float64{}
				for _, g := range deck.Gates {
					belongs := false
					for _, id := range g.Areas {
						if id == a.ID {
							belongs = true
						}
					}
					value, tangent := g.X, g.Z
					if axis == 1 {
						value, tangent = g.Z, g.X
					}
					if belongs && math.Abs(value-fixed) < .01 {
						cuts = append(cuts, tangent-deck.DoorWidth/2)
					}
				}
				// Small insertion sort keeps the shared layout dependency free.
				for i := 1; i < len(cuts); i++ {
					for j := i; j > 0 && cuts[j] < cuts[j-1]; j-- {
						cuts[j], cuts[j-1] = cuts[j-1], cuts[j]
					}
				}
				cuts = append(cuts, center+length/2)
				start := center - length/2
				for _, end := range cuts {
					if end > start+.01 {
						b := deckRect{X: fixed, Z: (start + end) / 2, W: deck.Wall, D: end - start}
						if axis == 1 {
							b.X, b.Z, b.W, b.D = (start+end)/2, fixed, end-start, deck.Wall
						}
						walls = append(walls, b)
					}
					start = end + deck.DoorWidth
				}
			}
		}
	}
	return walls
}()

func deckContains(a deckRect, x, z float64) bool {
	return math.Abs(x-a.X) <= a.W/2 && math.Abs(z-a.Z) <= a.D/2
}
func deckHit(b deckRect, x, z, r float64) bool {
	dx := math.Max(math.Abs(x-b.X)-b.W/2, 0)
	dz := math.Max(math.Abs(z-b.Z)-b.D/2, 0)
	return dx*dx+dz*dz < r*r
}
func deckWalkable(x, z float64) bool {
	floor := false
	for _, a := range deck.Areas {
		if deckContains(a, x, z) {
			floor = true
			break
		}
	}
	if !floor {
		return false
	}
	for _, b := range deckWalls {
		if deckHit(b, x, z, deck.Radius) {
			return false
		}
	}
	for _, b := range deck.Obstacles {
		if deckHit(b, x, z, deck.Radius) {
			return false
		}
	}
	return true
}
func architecturalDoors() []UniverseObject {
	out := []UniverseObject{}
	for _, g := range deck.Gates {
		if g.Door {
			out = append(out, UniverseObject{ID: g.ID, Kind: "door", Name: g.Name, X: g.X, Z: g.Z, Rotation: g.Rotation})
		}
	}
	return out
}
func isArchitecturalDoor(id string) bool {
	for _, g := range deck.Gates {
		if g.ID == id && g.Door {
			return true
		}
	}
	return false
}
func doorFraction(o UniverseObject, now int64) float64 {
	if o.MotionAt == 0 {
		if o.Open {
			return 1
		}
		return 0
	}
	t := math.Max(0, math.Min(1, float64(now-o.MotionAt)/deck.DoorDuration))
	target := 0.0
	if o.Open {
		target = 1
	}
	return o.MotionFrom + (target-o.MotionFrom)*t
}
func setDoorTarget(o *UniverseObject, open bool, now int64) {
	if o.Open == open {
		return
	}
	o.MotionFrom = doorFraction(*o, now)
	o.MotionAt = now
	o.Open = open
}
func doorBlocks(o UniverseObject, x, z float64, now int64) bool {
	dx, dz := x-o.X, z-o.Z
	c, s := math.Cos(o.Rotation), math.Sin(o.Rotation)
	normal, tangent := dx*c-dz*s, dx*s+dz*c
	return math.Abs(normal) < .15+deck.Radius && math.Abs(tangent) < 1.3+deck.Radius && math.Abs(tangent)+deck.Radius > doorFraction(o, now)*1.34
}
func doorOccupied(o UniverseObject, x, z float64) bool {
	dx, dz := x-o.X, z-o.Z
	c, s := math.Cos(o.Rotation), math.Sin(o.Rotation)
	return math.Abs(dx*c-dz*s) < .8 && math.Abs(dx*s+dz*c) < 1.6
}
func (m *Module) protectDoorOccupants(u *Universe, r *Room, positions map[string][2]float64) {
	for i := range u.Objects {
		o := &u.Objects[i]
		if o.Kind != "door" || o.Open || doorFraction(*o, time.Now().UnixMilli()) <= 0 {
			continue
		}
		for id, p := range positions {
			if r.Members[id] != nil && doorOccupied(*o, p[0], p[1]) {
				setDoorTarget(o, true, time.Now().UnixMilli())
				break
			}
		}
	}
}

func (m *Module) doorPlayerPositions() map[string][2]float64 {
	positions := map[string][2]float64{}
	if loop, ok := m.world.(*ServerLoop); ok {
		for _, p := range loop.GetAllPlayersStates() {
			positions[p.PlayerID] = [2]float64{p.Position.X, p.Position.Z}
		}
	}
	return positions
}

// A disconnected player may have been saved inside a passage that the remaining
// crew later closed. Restore just outside the leaf, keeping the door's state.
func (m *Module) repairDoorSpawn(state model.PlayerState) model.PlayerState {
	loop, ok := m.world.(*ServerLoop)
	if !ok {
		return state
	}
	m.roomMu.Lock()
	r := m.rooms[m.membership[state.PlayerID]]
	if r == nil || r.Universe == nil {
		m.roomMu.Unlock()
		return state
	}
	objects := append([]UniverseObject(nil), universeOf(r).Objects...)
	m.roomMu.Unlock()
	now := time.Now().UnixMilli()
	safe := func(x, z float64) bool {
		if !deckWalkable(x, z) {
			return false
		}
		for _, o := range objects {
			if o.Kind == "door" && doorBlocks(o, x, z, now) {
				return false
			}
		}
		return true
	}
	if safe(state.Position.X, state.Position.Z) {
		return state
	}
	for _, o := range objects {
		if o.Kind != "door" || !doorBlocks(o, state.Position.X, state.Position.Z, now) {
			continue
		}
		for _, side := range []float64{-1, 1} {
			x, z := state.Position.X+side*math.Cos(o.Rotation)*.9, state.Position.Z-side*math.Sin(o.Rotation)*.9
			if safe(x, z) {
				state.Position = model.Position{X: x, Y: 1.7, Z: z}
				loop.mu.Lock()
				if p := loop.players[state.PlayerID]; p != nil {
					p.Position = state.Position
					p.Dirty = true
				}
				loop.mu.Unlock()
				return state
			}
		}
	}
	return state
}
