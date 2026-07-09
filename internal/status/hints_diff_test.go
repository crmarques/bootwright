package status

import (
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func TestNextStepHintsSurfacesDiffOnlyWhenApplied(t *testing.T) {
	state := v1alpha1.State{}

	before := NextStepHints(true, state, "", "", nil, false, false)
	if contains(before, "bootwright diff") {
		t.Fatalf("diff must stay off the spine before the first apply: %v", before)
	}

	after := NextStepHints(true, state, "", "", nil, false, true)
	sc := indexOf(after, "bootwright diff")
	if sc < 0 {
		t.Fatalf("diff must appear on the spine once applied: %v", after)
	}
	plan := indexOf(after, "bootwright plan")
	if plan < 0 || sc > plan {
		t.Fatalf("diff must come before plan (sc=%d plan=%d): %v", sc, plan, after)
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
