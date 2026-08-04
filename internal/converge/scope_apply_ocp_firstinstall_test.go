package converge

import (
	"strings"
	"testing"
)

func TestApplyOCPRebuildAuthorizedClustersExtraVar(t *testing.T) {
	var plan WorkflowPlan
	ApplyOCPRebuildAuthorizedClustersExtraVar(&plan, nil)
	if len(plan.ExtraVarPairs) != 0 {
		t.Fatalf("empty list must append nothing, got %v", plan.ExtraVarPairs)
	}
	ApplyOCPRebuildAuthorizedClustersExtraVar(&plan, []string{"prod-east", "prod-west"})
	if got, want := strings.Join(plan.ExtraVarPairs, ";"), "bootwright_ocp_rebuild_authorized_clusters=prod-east,prod-west"; got != want {
		t.Fatalf("extra-var = %q, want %q", got, want)
	}
}
