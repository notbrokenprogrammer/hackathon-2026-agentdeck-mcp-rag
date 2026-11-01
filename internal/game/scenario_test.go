package game

import (
	"testing"
)

func TestScenarioControlWalkthrough(t *testing.T) {
	eng := NewScenarioEngine()
	s := eng.GetState()

	if s.Oxygen != 64 || s.Energy != 35 || s.Hull != 70 {
		t.Fatalf("unexpected start resources: O=%d, E=%d, H=%d", s.Oxygen, s.Energy, s.Hull)
	}

	// Цикл 1: Запустить реактор за 8 E
	s, err := eng.ExecuteAction("start_reactor")
	if err != nil {
		t.Fatal(err)
	}
	if s.Oxygen != 56 || s.Energy != 31 || s.Hull != 58 || s.Cycle != 2 {
		t.Fatalf("cycle 1 mismatch: O=%d, E=%d, H=%d, cycle=%d", s.Oxygen, s.Energy, s.Hull, s.Cycle)
	}

	// Цикл 2: Восстановить охлаждение за 6 E
	s, err = eng.ExecuteAction("repair_cooling")
	if err != nil {
		t.Fatal(err)
	}
	if s.Oxygen != 48 || s.Energy != 29 || s.Hull != 58 || s.Cycle != 3 {
		t.Fatalf("cycle 2 mismatch: O=%d, E=%d, H=%d, cycle=%d", s.Oxygen, s.Energy, s.Hull, s.Cycle)
	}

	// Цикл 3: Запустить оранжерею за 12 E
	s, err = eng.ExecuteAction("start_greenhouse")
	if err != nil {
		t.Fatal(err)
	}
	if s.Oxygen != 45 || s.Energy != 18 || s.Hull != 58 || s.Cycle != 4 {
		t.Fatalf("cycle 3 mismatch: O=%d, E=%d, H=%d, cycle=%d", s.Oxygen, s.Energy, s.Hull, s.Cycle)
	}

	// Цикл 4: Восстановить архив за 2 E
	s, err = eng.ExecuteAction("restore_archive")
	if err != nil {
		t.Fatal(err)
	}
	if s.Oxygen != 42 || s.Energy != 17 || s.Hull != 58 || s.Cycle != 5 {
		t.Fatalf("cycle 4 mismatch: O=%d, E=%d, H=%d, cycle=%d", s.Oxygen, s.Energy, s.Hull, s.Cycle)
	}

	// Расследование: проверяем 3 открытые ключевые улики
	solved, _, err := eng.SolveInvestigation([]string{"damaged_filter", "impact_mark", "protective_chronology"})
	if err != nil || !solved {
		t.Fatalf("investigation expected to be solved: solved=%v, err=%v", solved, err)
	}

	// Цикл 5: Подготовить курс за 4 E
	s, err = eng.ExecuteAction("calculate_course")
	if err != nil {
		t.Fatal(err)
	}
	if s.Oxygen != 39 || s.Energy != 14 || s.Hull != 58 || s.Cycle != 6 {
		t.Fatalf("cycle 5 mismatch: O=%d, E=%d, H=%d, cycle=%d", s.Oxygen, s.Energy, s.Hull, s.Cycle)
	}

	// Цикл 6: Ожидание и обсуждение финального плана
	s, err = eng.ExecuteAction("wait_cycle")
	if err != nil {
		t.Fatal(err)
	}
	if s.Oxygen != 36 || s.Energy != 15 || s.Hull != 58 || s.Cycle != 7 {
		t.Fatalf("cycle 6 mismatch: O=%d, E=%d, H=%d, cycle=%d", s.Oxygen, s.Energy, s.Hull, s.Cycle)
	}

	// Цикл 7: Финальный комбинированный импульс
	s, err = eng.ExecuteAction("final_combined")
	if err != nil {
		t.Fatal(err)
	}
	if s.Oxygen != 24 || s.Energy != 5 || s.Hull != 53 || !s.Victory || s.VictoryType != "safe_orbit" {
		t.Fatalf("cycle 7 final mismatch: O=%d, E=%d, H=%d, victory=%v, type=%s", s.Oxygen, s.Energy, s.Hull, s.Victory, s.VictoryType)
	}
}

func TestInvestigationRequiresDiscoveredClues(t *testing.T) {
	e := NewScenarioEngine()
	clues := []string{"damaged_filter", "impact_mark", "protective_chronology"}
	if solved, _, err := e.SolveInvestigation(clues); err == nil || solved {
		t.Fatal("guessed IDs unlocked hidden investigation")
	}
	if _, err := e.ExecuteAction("repair_cooling"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.ExecuteAction("restore_archive"); err != nil {
		t.Fatal(err)
	}
	if solved, _, err := e.SolveInvestigation(clues); err != nil || !solved {
		t.Fatalf("discovered evidence rejected: %v", err)
	}
}
