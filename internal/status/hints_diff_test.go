package status

import (
	"reflect"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func TestNextStepHintsSurfacesDiffOnlyWhenApplied(t *testing.T) {
	state := v1alpha1.State{}

	before := NextStepHints(true, state, "", "", "matrix", nil, false, false)
	if containsCommand(before, []string{"bootwright", "diff", "--context", "matrix"}) {
		t.Fatalf("diff must stay off the spine before the first apply: %v", before)
	}

	after := NextStepHints(true, state, "", "", "matrix", nil, false, true)
	sc := indexOfCommand(after, []string{"bootwright", "diff", "--context", "matrix"})
	if sc < 0 {
		t.Fatalf("diff must appear on the spine once applied: %v", after)
	}
	plan := indexOfCommand(after, []string{"bootwright", "plan", "--context", "matrix"})
	if plan < 0 || sc > plan {
		t.Fatalf("diff must come before plan (sc=%d plan=%d): %v", sc, plan, after)
	}
}

func containsCommand(hints []NextStepHint, want []string) bool {
	return indexOfCommand(hints, want) >= 0
}

func indexOfCommand(hints []NextStepHint, want []string) int {
	for i, h := range hints {
		if reflect.DeepEqual(h.Args, want) {
			return i
		}
	}
	return -1
}
