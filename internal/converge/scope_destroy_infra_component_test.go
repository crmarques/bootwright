package converge

import (
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/converge/workflow"
)

func infraComponentScopeVar(plan WorkflowPlan) (string, bool) {
	for _, pair := range plan.ExtraVarPairs {
		if name, val, ok := strings.Cut(pair, "="); ok && name == workflow.InfraComponentDestroyScopeRecordsExtraVar {
			return val, true
		}
	}
	return "", false
}

func TestApplyDestroyScopeExtraVarsClusterScopeGatesInfraComponents(t *testing.T) {
	plan := WorkflowPlan{}
	ApplyDestroyScopeExtraVars(&plan, true, "ocp-a", []string{"ocp-a"}, nil, false, false, false, false)

	val, ok := infraComponentScopeVar(plan)
	if !ok || val != "" {
		t.Fatalf("a cluster-scoped destroy must emit an empty infra-component allowlist when the scope owns no service; got val=%q ok=%v vars=%v", val, ok, plan.ExtraVarPairs)
	}
}

func TestApplyDestroyScopeExtraVarsContextSweepLeavesInfraComponentsUngated(t *testing.T) {
	plan := WorkflowPlan{}
	ApplyDestroyScopeExtraVars(&plan, true, "", nil, nil, false, false, false, false)

	if val, ok := infraComponentScopeVar(plan); ok {
		t.Fatalf("a context sweep tears down every recorded component, so it must not emit the allowlist; got %q in %v", val, plan.ExtraVarPairs)
	}
}

func TestApplyDestroyStaleInputDisablesRecordOnlySweeps(t *testing.T) {
	plan := WorkflowPlan{}
	ApplyDestroyScopeExtraVars(&plan, true, "", nil, nil, false, false, false, false)
	ApplyDestroyEvidenceDegradedExtraVar(&plan, true)

	for _, pair := range plan.ExtraVarPairs {
		if pair == InfraDestroyContextSweepExtraVar+"=true" {
			t.Fatalf("stale-input destroy must not sweep context records: %v", plan.ExtraVarPairs)
		}
	}
	want := DestroySkipOrphanSweepExtraVar + "=true"
	if !slices.Contains(plan.ExtraVarPairs, want) {
		t.Fatalf("stale-input destroy must publish the fail-closed sweep suppression %q: %v", want, plan.ExtraVarPairs)
	}
}
