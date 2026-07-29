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
		ApplyDestroyScopeExtraVars(&plan, false, "", nil, nil, false, false, false)
		if _, ok := storageVar(plan); ok {
			t.Fatalf("unscoped destroy must not emit the storage-scope gate; got %v", plan.ExtraVarPairs)
		}
	})

	t.Run("container-only emits empty allowlist (tear down none)", func(t *testing.T) {
		plan := WorkflowPlan{StorageWorkNames: []string{}}
		ApplyDestroyScopeExtraVars(&plan, false, "ocp-a", nil, nil, false, false, false)
		val, ok := storageVar(plan)
		if !ok || val != "" {
			t.Fatalf("container-only selection must emit an empty storage allowlist; got val=%q ok=%v vars=%v", val, ok, plan.ExtraVarPairs)
		}
	})

	t.Run("storage-narrowed emits the named roots", func(t *testing.T) {
		plan := WorkflowPlan{StorageWorkNames: []string{"ceph-a", "ceph-b"}}
		ApplyDestroyScopeExtraVars(&plan, false, "ceph-a,ceph-b", nil, nil, false, false, false)
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
		ApplyDestroyScopeExtraVars(&plan, false, "", nil, nil, false, false, false)
		if has(plan) {
			t.Fatalf("destroy without --authorize unreachable-nodes must not emit the gate; got %v", plan.ExtraVarPairs)
		}
	})

	t.Run("emitted on opt-in", func(t *testing.T) {
		plan := WorkflowPlan{}
		ApplyDestroyScopeExtraVars(&plan, false, "", nil, nil, false, false, true)
		if !has(plan) {
			t.Fatalf("destroy with --authorize unreachable-nodes must emit %s=true; got %v", DestroySkipUnreachableExtraVar, plan.ExtraVarPairs)
		}
	})
}

func TestApplyDestroyScopeExtraVarsUnownedSplit(t *testing.T) {
	has := func(plan WorkflowPlan, name string) bool {
		for _, pair := range plan.ExtraVarPairs {
			if pair == name+"=true" {
				return true
			}
		}
		return false
	}

	t.Run("neither by default", func(t *testing.T) {
		plan := WorkflowPlan{}
		ApplyDestroyScopeExtraVars(&plan, true, "", nil, nil, false, false, false)
		if has(plan, DestroyAuthorizeUnownedVMsExtraVar) || has(plan, DestroyAuthorizeUnownedNetsExtraVar) {
			t.Fatalf("destroy without an unowned authorization must emit neither gate; got %v", plan.ExtraVarPairs)
		}
	})

	t.Run("vms only", func(t *testing.T) {
		plan := WorkflowPlan{}
		ApplyDestroyScopeExtraVars(&plan, true, "", nil, nil, true, false, false)
		if !has(plan, DestroyAuthorizeUnownedVMsExtraVar) {
			t.Fatalf("--authorize unowned-vms must emit %s=true; got %v", DestroyAuthorizeUnownedVMsExtraVar, plan.ExtraVarPairs)
		}
		if has(plan, DestroyAuthorizeUnownedNetsExtraVar) {
			t.Fatalf("--authorize unowned-vms must not authorize unowned networks; got %v", plan.ExtraVarPairs)
		}
	})

	t.Run("networks only", func(t *testing.T) {
		plan := WorkflowPlan{}
		ApplyDestroyScopeExtraVars(&plan, true, "", nil, nil, false, true, false)
		if !has(plan, DestroyAuthorizeUnownedNetsExtraVar) {
			t.Fatalf("--authorize unowned-networks must emit %s=true; got %v", DestroyAuthorizeUnownedNetsExtraVar, plan.ExtraVarPairs)
		}
		if has(plan, DestroyAuthorizeUnownedVMsExtraVar) {
			t.Fatalf("--authorize unowned-networks must not authorize unowned VMs; got %v", plan.ExtraVarPairs)
		}
	})
}
