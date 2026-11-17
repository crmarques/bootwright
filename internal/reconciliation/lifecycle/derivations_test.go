package lifecycle

import (
	"errors"
	"testing"
)

func TestAPlanInputDerivesEachKeyOnceAndKeepsNoFailure(t *testing.T) {
	runs := 0
	derive := func() (int, error) { runs++; return runs, nil }
	input := PlanInput{Derived: NewDerivations()}
	for range 3 {
		if value, err := Derived(input, "requests", derive); err != nil || value != 1 {
			t.Fatalf("the shared derivation = %d (%v), want the first run's 1", value, err)
		}
	}
	if value, _ := Derived(input, "other", derive); value != 2 || runs != 2 {
		t.Fatalf("another key derived %d after %d runs", value, runs)
	}
	if value, _ := Derived(PlanInput{}, "requests", derive); value != 3 {
		t.Fatalf("an input holding no derivations reused %d", value)
	}
	failed := errors.New("unrenderable")
	attempts := 0
	failing := func() (int, error) {
		attempts++
		if attempts == 1 {
			return 0, failed
		}
		return attempts, nil
	}
	if _, err := Derived(input, "failing", failing); !errors.Is(err, failed) {
		t.Fatalf("the failed derivation reported %v", err)
	}
	if value, err := Derived(input, "failing", failing); err != nil || value != 2 {
		t.Fatalf("a failed derivation was kept: %d (%v)", value, err)
	}
	if fresh, _ := Derived(PlanInput{Derived: NewDerivations()}, "requests", derive); fresh != 4 {
		t.Fatalf("another plan's input reused %d", fresh)
	}
}
