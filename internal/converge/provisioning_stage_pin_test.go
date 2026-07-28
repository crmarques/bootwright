package converge

import (
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func TestSubPhaseStageNamesMatchProvisioningStages(t *testing.T) {
	got := SubPhaseStageNames()
	want := v1alpha1.CustomPlaybookAnchors()
	if len(got) != len(want) {
		t.Fatalf("SubPhaseStageNames()=%v, PlaybookAnchors()=%v: length mismatch", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("stage[%d]: SubPhaseStageNames()=%q, PlaybookAnchors()=%q", i, got[i], want[i])
		}
	}
}
