package converge

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/converge/workflow"
)

func TestApplyDestroyScopeExtraVarsStorageGate(t *testing.T) {
	storageVar := func(plan WorkflowPlan) (string, bool) {
		for _, pair := range plan.ExtraVarPairs {
			if name, val, ok := strings.Cut(pair, "="); ok && name == workflow.DestroyStorageScopeExtraVar {
				return val, true
			}
		}
		return "", false
	}

	t.Run("no narrowing emits nothing", func(t *testing.T) {
		plan := WorkflowPlan{StorageWorkNames: nil}
		ApplyDestroyScopeExtraVars(&plan, false, "", nil, nil, false, false)
		if _, ok := storageVar(plan); ok {
			t.Fatalf("unscoped destroy must not emit the storage-scope gate; got %v", plan.ExtraVarPairs)
		}
	})

	t.Run("container-only emits empty allowlist (tear down none)", func(t *testing.T) {
		plan := WorkflowPlan{StorageWorkNames: []string{}}
		ApplyDestroyScopeExtraVars(&plan, false, "ocp-a", nil, nil, false, false)
		val, ok := storageVar(plan)
		if !ok || val != "" {
			t.Fatalf("container-only selection must emit an empty storage allowlist; got val=%q ok=%v vars=%v", val, ok, plan.ExtraVarPairs)
		}
	})

	t.Run("storage-narrowed emits the named roots", func(t *testing.T) {
		plan := WorkflowPlan{StorageWorkNames: []string{"ceph-a", "ceph-b"}}
		ApplyDestroyScopeExtraVars(&plan, false, "ceph-a,ceph-b", nil, nil, false, false)
		val, ok := storageVar(plan)
		if !ok || val != "ceph-a,ceph-b" {
			t.Fatalf("storage-narrowed selection must emit the allowlist; got val=%q ok=%v vars=%v", val, ok, plan.ExtraVarPairs)
		}
	})
}

func TestApplyDestroyScopeExtraVarsSkipUnreachable(t *testing.T) {
	has := func(plan WorkflowPlan) bool {
		for _, pair := range plan.ExtraVarPairs {
			if pair == DestroySkipUnreachableExtraVar+"=true" {
				return true
			}
		}
		return false
	}

	t.Run("absent by default", func(t *testing.T) {
		plan := WorkflowPlan{}
		ApplyDestroyScopeExtraVars(&plan, false, "", nil, nil, false, false)
		if has(plan) {
			t.Fatalf("destroy without --skip-unreachable must not emit the gate; got %v", plan.ExtraVarPairs)
		}
	})

	t.Run("emitted on opt-in", func(t *testing.T) {
		plan := WorkflowPlan{}
		ApplyDestroyScopeExtraVars(&plan, false, "", nil, nil, false, true)
		if !has(plan) {
			t.Fatalf("destroy with --skip-unreachable must emit %s=true; got %v", DestroySkipUnreachableExtraVar, plan.ExtraVarPairs)
		}
	})
}
