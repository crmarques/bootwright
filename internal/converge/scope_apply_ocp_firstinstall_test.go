package converge

import (
	"strings"
	"testing"
)

func TestApplyOCPFirstInstallClustersExtraVar(t *testing.T) {
	var plan WorkflowPlan
	ApplyOCPFirstInstallClustersExtraVar(&plan, nil)
	if len(plan.ExtraVarPairs) != 0 {
		t.Fatalf("empty list must append nothing, got %v", plan.ExtraVarPairs)
	}
	ApplyOCPFirstInstallClustersExtraVar(&plan, []string{"prod-east", "prod-west"})
	if got, want := strings.Join(plan.ExtraVarPairs, ";"), "bootwright_ocp_first_install_clusters=prod-east,prod-west"; got != want {
		t.Fatalf("extra-var = %q, want %q", got, want)
	}
}
