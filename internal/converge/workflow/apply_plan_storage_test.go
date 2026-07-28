package workflow

import (
	"path/filepath"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	desiredstate "github.com/crmarques/bootwright/internal/state/desired"
)

func managedOSLibvirtCephState(t *testing.T) v1alpha1.State {
	t.Helper()
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join("..", "..", "..", "test", "e2e", "006-ceph-3nodes-libvirt-managed-os")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	return state
}

func TestStorageManagedOSPrepareDependsOnlyOnItsProviderHost(t *testing.T) {
	state := managedOSLibvirtCephState(t)
	tasks, err := PlanApplyTasksChecked(applyAllTarget(), state)
	if err != nil {
		t.Fatalf("PlanApplyTasksChecked: %v", err)
	}

	assertTaskDeps(t, tasks, "osprepare.ceph-libvirt.bastion", "provider.bastion")
	assertTaskPresent(t, tasks, "infra-component.bastion")
	assertTaskResourceKeys(t, tasks, "osprepare.ceph-libvirt.bastion", hostMutationResource("bastion"))
}

func TestStorageManagedOSInstallKeepsMachineServiceDependencies(t *testing.T) {
	state := managedOSLibvirtCephState(t)
	tasks, err := PlanApplyTasksChecked(applyAllTarget(), state)
	if err != nil {
		t.Fatalf("PlanApplyTasksChecked: %v", err)
	}

	assertTaskHasDeps(t, tasks, "osinstall.ceph-libvirt", "provider.bastion", "infra-component.bastion", "osprepare.ceph-libvirt.bastion")
	assertTaskHasDeps(t, tasks, "storageinfra.ceph-libvirt", "provider.bastion", "infra-component.bastion")
	assertTaskHasDeps(t, tasks, "storage.ceph-libvirt", "provider.bastion", "infra-component.bastion")
}

func TestProvisioningPlaybookBeforeDepsHardGatesStorageClusterPrereqs(t *testing.T) {
	state := managedOSLibvirtCephState(t)
	state.CustomPlaybooks = []v1alpha1.CustomPlaybook{
		provisioningPlaybook("delegated-registration", v1alpha1.CustomPlaybookAnchorDeps, anchorKeyGates,
			v1alpha1.CustomPlaybookTarget{Clusters: []string{"ceph-libvirt"}}),
	}
	tasks, err := PlanApplyTasksChecked(applyAllTarget(), state)
	if err != nil {
		t.Fatalf("PlanApplyTasksChecked: %v", err)
	}
	prereqs := taskByID(t, tasks, "storageinfra.ceph-libvirt")
	for _, dep := range prereqs.Entry.Dependencies {
		if dep == "playbook.delegated-registration" {
			return
		}
	}
	t.Fatalf("storageinfra.ceph-libvirt dependencies = %v, want to include playbook.delegated-registration: ADR 0015 and specs/state-model.md promise that a deps-gating CustomPlaybook with the default failureMode: fail hard-gates the deps-phase Ceph work, which is how rhsm.management: external delegates node registration; the gate exists only because the Ceph prerequisite phases are carried by a task whose kind maps to the deps phase, so folding them into storage.ceph-libvirt (a base-phase kind) empties the deps anchor set and drops the edge", prereqs.Entry.Dependencies)
}

func TestProvisioningPlaybookAfterDepsAnchorsOnStorageClusterPrereqs(t *testing.T) {
	state := managedOSLibvirtCephState(t)
	state.CustomPlaybooks = []v1alpha1.CustomPlaybook{
		provisioningPlaybook("post-deps", v1alpha1.CustomPlaybookAnchorDeps, anchorKeyFollows,
			v1alpha1.CustomPlaybookTarget{Clusters: []string{"ceph-libvirt"}}),
	}
	tasks, err := PlanApplyTasksChecked(applyAllTarget(), state)
	if err != nil {
		t.Fatalf("PlanApplyTasksChecked: %v", err)
	}
	hook := taskByID(t, tasks, "playbook.post-deps")
	assertDependsOn(t, hook, "storageinfra.ceph-libvirt")
}

func TestProvidedOSStorageClusterKeepsMachineServiceDependencies(t *testing.T) {
	state := managedOSLibvirtCephState(t)
	provided := true
	for i := range state.Machines {
		if state.Machines[i].Spec.OS.Provided == nil || *state.Machines[i].Spec.OS.Provided {
			continue
		}
		state.Machines[i].Spec.OS.Provided = &provided
	}
	tasks, err := PlanApplyTasksChecked(applyAllTarget(), state)
	if err != nil {
		t.Fatalf("PlanApplyTasksChecked: %v", err)
	}

	assertTaskMissing(t, tasks, "osinstall.ceph-libvirt")
	assertTaskMissing(t, tasks, "osprepare.ceph-libvirt.bastion")
	assertTaskDeps(t, tasks, "storageinfra.ceph-libvirt", "provider.bastion", "infra-component.bastion", "nodeaccess.ceph-libvirt")
	assertTaskDeps(t, tasks, "storage.ceph-libvirt", "provider.bastion", "infra-component.bastion", "storageinfra.ceph-libvirt")
}
