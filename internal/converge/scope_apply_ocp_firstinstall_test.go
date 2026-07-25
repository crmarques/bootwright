package converge

import (
	"strings"
	"testing"
)

func TestApplyOCPReinstallClustersExtraVar(t *testing.T) {
	var plan WorkflowPlan
	ApplyOCPReinstallClustersExtraVar(&plan, nil)
	if len(plan.ExtraVarPairs) != 0 {
		t.Fatalf("empty list must append nothing so the occupancy guard stays armed, got %v", plan.ExtraVarPairs)
	}
	ApplyOCPReinstallClustersExtraVar(&plan, []string{"prod-east", "prod-west"})
	if got, want := strings.Join(plan.ExtraVarPairs, ";"), "bootwright_ocp_reinstall_clusters=prod-east,prod-west"; got != want {
		t.Fatalf("extra-var = %q, want %q", got, want)
	}
}

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
