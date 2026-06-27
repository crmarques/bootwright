package status

import (
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

// P21: state-check is the read-only drift verb for the steady-state loop. It belongs
// on the status next-step spine only once the context has a recorded apply (applied),
// and must be positioned before plan/apply so it is reached before convergence.
func TestNextStepHintsSurfacesStateCheckOnlyWhenApplied(t *testing.T) {
	state := v1alpha1.State{}

	before := NextStepHints(true, state, "", "", nil, false, false)
	if contains(before, "bootwright state-check") {
		t.Fatalf("state-check must stay off the spine before the first apply: %v", before)
	}

	after := NextStepHints(true, state, "", "", nil, false, true)
	sc := indexOf(after, "bootwright state-check")
	if sc < 0 {
		t.Fatalf("state-check must appear on the spine once applied: %v", after)
	}
	plan := indexOf(after, "bootwright plan")
	if plan < 0 || sc > plan {
		t.Fatalf("state-check must come before plan (sc=%d plan=%d): %v", sc, plan, after)
	}
}

func contains(hints []string, want string) bool { return indexOf(hints, want) >= 0 }

func indexOf(hints []string, want string) int {
	for i, h := range hints {
		if h == want {
			return i
		}
	}
	return -1
}
