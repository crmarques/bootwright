package converge

import (
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

// TestSubPhaseStageNamesMatchProvisioningStages pins the converge --stage
// sub-phase vocabulary to api/v1alpha1.ProvisioningStages(), the single source
// of truth a ProvisioningPlaybook's spec.stage validates against. The leaf api
// package cannot import converge, so this converge-side test keeps the two lists
// from drifting.
func TestSubPhaseStageNamesMatchProvisioningStages(t *testing.T) {
	got := SubPhaseStageNames()
	want := v1alpha1.ProvisioningStages()
	if len(got) != len(want) {
		t.Fatalf("SubPhaseStageNames()=%v, ProvisioningStages()=%v: length mismatch", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("stage[%d]: SubPhaseStageNames()=%q, ProvisioningStages()=%q", i, got[i], want[i])
		}
	}
}
