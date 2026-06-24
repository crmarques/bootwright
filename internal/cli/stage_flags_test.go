package cli

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/converge"
)

func TestStageFlagCompletionOffersCanonicalValues(t *testing.T) {
	// apply/plan/state-check complete the full vocabulary; destroy completes the
	// families only. Driving completion from the converge accessors keeps it in
	// lockstep with validation and help.
	applyOut, _, code := runCLI(t, "__complete", "apply", "--stage", "")
	if code != 0 {
		t.Fatalf("apply __complete exit = %d, out=%q", code, applyOut)
	}
	for _, want := range converge.ApplyStageNames() {
		if !strings.Contains(applyOut, want) {
			t.Fatalf("apply --stage completion missing %q:\n%s", want, applyOut)
		}
	}

	destroyOut, _, code := runCLI(t, "__complete", "destroy", "--stage", "")
	if code != 0 {
		t.Fatalf("destroy __complete exit = %d, out=%q", code, destroyOut)
	}
	for _, want := range converge.DestroyStageNames() {
		if !strings.Contains(destroyOut, want) {
			t.Fatalf("destroy --stage completion missing %q:\n%s", want, destroyOut)
		}
	}
	if strings.Contains(destroyOut, "fabric") {
		t.Fatalf("destroy --stage completion offered an apply-only sub-phase:\n%s", destroyOut)
	}
}
