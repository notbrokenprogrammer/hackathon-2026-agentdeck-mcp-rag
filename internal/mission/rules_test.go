package mission

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
)

func solo(s State) View {
	return View{State: s, Players: 1, Roles: []string{Captain, Engineer, Biologist, Navigator}, Facts: []Fact{{ID: "valves_inspected"}, {ID: "drone_sensor"}, {ID: "navigation_records"}, {ID: "power_audit"}}}
}
func step(t *testing.T, s State, a Action) State {
	t.Helper()
	f := Preview(solo(s), a)
	if !f.Allowed {
		t.Fatal(a, f.Reasons)
	}
	return f.After
}
func TestReferencePlaythroughFromAttachedDocument(t *testing.T) {
	s, _ := Initial(nil)
	actions := []string{"start_reactor", "repair_cooling", "start_garden", "restore_archive", "prepare_course", "wait", "combined_plan"}
	want := [][3]int{{56, 31, 58}, {48, 29, 58}, {45, 18, 58}, {42, 17, 58}, {39, 14, 58}, {36, 15, 58}, {24, 5, 53}}
	for i, a := range actions {
		s = step(t, s, Action{Name: a})
		if [3]int{s.O, s.E, s.H} != want[i] || s.Cycle != i+1 {
			t.Fatalf("cycle %d: %+v", i+1, s)
		}
	}
	if s.Status != "won" || s.Battery {
		t.Fatal(s)
	}
}
func TestFinalThresholdsAndNoNormalDrain(t *testing.T) {
	s, _ := Initial(nil)
	s.Reactor = true
	s.Cooling = true
	for _, tc := range []struct {
		a              string
		e, o, h, cycle int
		allowed        bool
		status         string
	}{{"combined_plan", 10, 64, 70, 0, false, ""}, {"combined_plan", 11, 12, 70, 0, false, ""}, {"combined_plan", 11, 13, 30, 7, true, "won"}, {"emergency_burn", 22, 64, 70, 0, false, ""}, {"emergency_burn", 23, 1, 35, 7, true, "won"}, {"emergency_burn", 23, 1, 34, 6, true, "active"}, {"evacuate", 5, 7, 1, 7, true, "evacuated"}} {
		x := s
		x.E = tc.e
		x.O = tc.o
		x.H = tc.h
		x.Cycle = tc.cycle
		f := Preview(solo(x), Action{Name: tc.a})
		if f.Allowed != tc.allowed || tc.allowed && f.After.Status != tc.status {
			t.Fatal(tc, f)
		}
		if tc.allowed && f.After.O != tc.o-f.OxygenCost {
			t.Fatal("unexpected normal oxygen drain", tc, f)
		}
	}
	s.Cooling = false
	s.H = 45
	f := Preview(solo(s), Action{Name: "emergency_burn"})
	if !f.Allowed || f.After.Status != "lost" || f.After.Reactor {
		t.Fatal(f)
	}
	s.Cooling = true
	s.Garden = true
	s.Course = true
	s.E = 18
	s.O = 25
	s.H = 40
	s.Cycle = 6
	f = Preview(solo(s), Action{Name: "ecosystem_plan"})
	if !f.Allowed || f.After.Cycle != 8 || f.After.E != 4 || f.After.O != 19 || f.After.Status != "won" {
		t.Fatal(f)
	}
	s.Cycle = 7
	if Preview(solo(s), Action{Name: "ecosystem_plan"}).Allowed {
		t.Fatal("two-cycle plan started on last turn")
	}
}
func TestQuestPricesModifiersAndOneTimeRewards(t *testing.T) {
	s, _ := Initial([]string{"leak", "drone"})
	s = step(t, s, Action{Name: "start_reactor"})
	if s.O != 56 || s.Quests["leak"] != "active" {
		t.Fatal(s)
	}
	x := step(t, s, Action{Name: "wait"})
	if x.O != 46 {
		t.Fatal("leak must first charge at cycle 2", x)
	}
	s = step(t, s, Action{Name: "leak_patch"})
	if s.O != 48 || s.Quests["leak"] != "patched" {
		t.Fatal(s)
	}
	if Preview(solo(s), Action{Name: "leak_patch"}).Allowed {
		t.Fatal("quest reward repeated")
	}
	s = step(t, s, Action{Name: "repair_cooling"})
	s.E = 50
	s.H = 98
	s = step(t, s, Action{Name: "drone_dispatch", Path: []string{"А", "В", "Г"}})
	if s.H != 100 || s.Quests["drone"] != "returned" {
		t.Fatal(s)
	}
	for _, q := range []string{"garden", "capsule", "power"} {
		s, _ := Initial([]string{q})
		s.Reactor = true
		s.Cooling = true
		s.Garden = true
		s.E = 50
		s.O = 50
		s.Cycle = 4
		activate(&s)
		a := map[string]string{"garden": "garden_preserve", "capsule": "capsule_recover", "power": "power_rewire"}[q]
		f := Preview(solo(s), Action{Name: a})
		if !f.Allowed {
			t.Fatal(q, f)
		}
		switch q {
		case "garden":
			if f.After.O != 52 || f.After.E != 47 {
				t.Fatal(f)
			}
		case "capsule":
			if f.After.O != 59 || f.After.E != 45 || f.After.H != 66 {
				t.Fatal(f)
			}
		case "power":
			if f.After.E != 51 || f.After.BaseDrain != 2 {
				t.Fatal(f)
			}
		}
		if Preview(solo(f.After), Action{Name: a}).Allowed {
			t.Fatal("repeat", q)
		}
	}
}
func TestDoorNavigationAndSensorGates(t *testing.T) {
	s, _ := Initial([]string{"door", "beacon"})
	if Preview(solo(s), Action{Name: "repair_cooling"}).Allowed {
		t.Fatal("door bypass")
	}
	s.PumpOff = true
	s.DoorOpen = true
	if !Preview(solo(s), Action{Name: "repair_cooling"}).Allowed {
		t.Fatal("cooperative repair blocked")
	}
	x := step(t, s, Action{Name: "prepare_course", Choice: "signal_b"})
	if x.Course {
		t.Fatal("stale signal accepted")
	}
	x = step(t, x, Action{Name: "prepare_course", Choice: "signal_a"})
	if !x.Course {
		t.Fatal("cannot recover from wrong navigation")
	}
	for _, path := range [][]string{{"А", "Г"}, {"А", "В", "Б", "Г"}, {"Г", "В", "А"}} {
		if valid, _ := CheckRoute(path); valid {
			t.Fatal(path)
		}
	}
}
func TestPersistentCommandsRolesAndIdempotency(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "mission.json")
	svc, _ := New(path)
	cap, err := svc.Create(ctx, "cap", 4, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cap.HasFact("temperature_spike") || cap.HasFact("no_infection") || cap.HasFact("cause_confirmed") {
		t.Fatal("private facts leaked", cap)
	}
	eng, err := svc.Join(ctx, "eng", Engineer)
	if err != nil || !eng.HasFact("temperature_spike") {
		t.Fatal(eng, err)
	}
	if _, err = svc.Apply(ctx, "eng", Command{ID: "bad", Version: eng.State.Version, Type: "confirm", Action: Action{Name: "start_reactor"}}); err == nil {
		t.Fatal("specialist confirmed")
	}
	cap, _ = svc.View("cap")
	if _, err = svc.Apply(ctx, "cap", Command{ID: "bad2", Version: cap.State.Version, Type: "confirm", Action: Action{Name: "start_reactor"}}); err == nil {
		t.Fatal("captain bypassed specialist proposal")
	}
	r, err := svc.Apply(ctx, "eng", Command{ID: "proposal", Version: eng.State.Version, Type: "propose", Action: Action{Name: "start_reactor"}})
	if err != nil {
		t.Fatal(err)
	}
	c := Command{ID: "execute", Version: r.View.State.Version, Type: "confirm"}
	r, err = svc.Apply(ctx, "cap", c)
	if err != nil || r.View.State.Cycle != 1 {
		t.Fatal(r, err)
	}
	retry, err := svc.Apply(ctx, "cap", c)
	if err != nil || !retry.Duplicate || retry.View.State.Cycle != 1 {
		t.Fatal(retry, err)
	}
	c.ID = "stale"
	if _, err = svc.Apply(ctx, "cap", c); err == nil {
		t.Fatal("stale write accepted")
	}
	svc, err = New(path)
	if err != nil {
		t.Fatal(err)
	}
	cap, _ = svc.View("cap")
	if cap.State.O != 56 || cap.State.E != 31 || cap.State.H != 58 {
		t.Fatal(cap)
	}
	if _, err = svc.View("intruder"); err == nil {
		t.Fatal("unjoined state read")
	}
}
func TestSoloInvestigationAndQuestFreeCommands(t *testing.T) {
	ctx := context.Background()
	svc, _ := New("")
	v, _ := svc.Create(ctx, "p", 1, []string{"door", "memory"})
	n := 0
	run := func(typ string, a Action, value string, evidence ...string) error {
		n++
		r, err := svc.Apply(ctx, "p", Command{ID: fmt.Sprint(n), Version: v.State.Version, Type: typ, Action: a, Value: value, Evidence: evidence})
		if err == nil {
			v = r.View
		}
		return err
	}
	if err := run("open_door", Action{}, ""); err == nil {
		t.Fatal("opened live door")
	}
	for _, c := range []struct {
		typ string
		a   Action
	}{{"power_off", Action{}}, {"open_door", Action{}}, {"confirm", Action{Name: "repair_cooling"}}, {"confirm", Action{Name: "restore_archive"}}} {
		if err := run(c.typ, c.a, ""); err != nil {
			t.Fatal(err)
		}
	}
	if v.State.Cycle != 2 {
		t.Fatal("free coordination consumed cycles", v)
	}
	if err := run("verify_memory", Action{}, ""); err != nil {
		t.Fatal(err)
	}
	if err := run("investigate", Action{}, "impact_cooling", "damaged_filter", "impact_record", "archive_chronology"); err != nil || !v.HasFact("cause_confirmed") {
		t.Fatal(v, err)
	}
}
