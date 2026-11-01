package mission

import (
	"context"
	"fmt"
	"sync"
	"testing"
)

func TestPartySizesAndRoleTransfer(t *testing.T) {
	ctx := context.Background()
	for players := 1; players <= 4; players++ {
		s, _ := New("")
		_, err := s.Create(ctx, "p0", players, nil)
		if err != nil {
			t.Fatal(err)
		}
		for n := 1; n < players; n++ {
			if _, err = s.Join(ctx, fmt.Sprint("p", n), ""); err != nil {
				t.Fatal(players, err)
			}
		}
		seen := map[string]bool{}
		for _, id := range s.PlayerIDs() {
			v, _ := s.View(id)
			for _, role := range v.Roles {
				if seen[role] {
					t.Fatal("duplicate role")
				}
				seen[role] = true
			}
		}
		if len(seen) != 4 {
			t.Fatal(players, seen)
		}
		if players == 2 {
			v, _ := s.View("p0")
			if v.Owns(Navigator) {
				t.Fatal("archive should belong to second player in duo")
			}
		}
	}
	s, _ := New("")
	_, _ = s.Create(ctx, "cap", 4, nil)
	_, _ = s.Join(ctx, "eng", Engineer)
	cap, _ := s.View("cap")
	if cap.HasFact("temperature_spike") {
		t.Fatal(cap)
	}
	r, err := s.Apply(ctx, "cap", Command{ID: "take", Version: cap.State.Version, Type: "transfer_role", Value: Engineer})
	if err != nil || !r.View.HasFact("temperature_spike") {
		t.Fatal(r, err)
	}
	eng, _ := s.View("eng")
	if eng.HasFact("temperature_spike") || eng.Owns(Engineer) {
		t.Fatal("revoked role still visible", eng)
	}
}
func TestConcurrentCommandsSerializeWithVersionCheck(t *testing.T) {
	ctx := context.Background()
	s, _ := New("")
	v, _ := s.Create(ctx, "p", 1, nil)
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_, err := s.Apply(ctx, "p", Command{ID: fmt.Sprint(n), Version: v.State.Version, Type: "confirm", Action: Action{Name: "start_reactor"}})
			results <- err
		}(i)
	}
	wg.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		}
	}
	after, _ := s.View("p")
	if success != 1 || after.State.Cycle != 1 || after.State.E != 31 {
		t.Fatal(success, after)
	}
}
