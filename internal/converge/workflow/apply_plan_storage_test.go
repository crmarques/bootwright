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
	assertTaskDeps(t, tasks, "storageinfra.ceph-libvirt", "provider.bastion", "infra-component.bastion")
	assertTaskDeps(t, tasks, "storage.ceph-libvirt", "provider.bastion", "infra-component.bastion", "storageinfra.ceph-libvirt")
}
