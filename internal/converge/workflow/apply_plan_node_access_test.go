package workflow

import (
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/render"
)

func hardenCephNodeAccess(t *testing.T, state *v1alpha1.State, clusterName, user, keyRef string) {
	t.Helper()
	for i := range state.StorageClusters {
		if state.StorageClusters[i].Metadata.Name != clusterName {
			continue
		}
		state.StorageClusters[i].Spec.Ceph.Cephadm.ClusterSSH = v1alpha1.StorageCephadmSSHSpec{
			User:   user,
			KeyRef: v1alpha1.LocalObjectReference{Name: keyRef},
		}
		for _, node := range state.StorageClusters[i].Spec.Ceph.Topology.Nodes {
			for j := range state.Machines {
				if state.Machines[j].Metadata.Name == node.MachineRef.Name {
					state.Machines[j].Spec.Access.RootLogin = v1alpha1.MachineRootLoginRevoke
				}
			}
		}
		return
	}
	t.Fatalf("storage cluster %s not found", clusterName)
}

func TestPlanStorageNodeAccessBetweenOSInstallAndRegistration(t *testing.T) {
	state := cephSubscriptionExampleState(t)
	hardenCephNodeAccess(t, &state, "ceph-ibm", "cephadm", "ceph-cluster-key")
	tasks, err := PlanApplyTasksChecked(applyAllTarget(), state)
	if err != nil {
		t.Fatalf("PlanApplyTasksChecked: %v", err)
	}
	task := assertTaskPresent(t, tasks, "nodeaccess.ceph-ibm")
	if task.Entry.Kind != ApplyTaskKindStorageNodeAccess {
		t.Fatalf("node access task kind = %q, want %q", task.Entry.Kind, ApplyTaskKindStorageNodeAccess)
	}
	if task.Limit != render.StorageClusterGroupName("ceph-ibm") {
		t.Fatalf("node access limit = %q, want the storage cluster group", task.Limit)
	}
	assertTaskDeps(t, tasks, "nodeaccess.ceph-ibm", "provider.bastion", "infra-component.bastion", "osinstall.ceph-ibm")
	assertTaskDeps(t, tasks, "registration.ceph-ibm", "provider.bastion", "infra-component.bastion", "osinstall.ceph-ibm", "nodeaccess.ceph-ibm")
	assertTaskDeps(t, tasks, "storageinfra.ceph-ibm", "provider.bastion", "infra-component.bastion", "osinstall.ceph-ibm", "nodeaccess.ceph-ibm", "registration.ceph-ibm")
}

func TestPlanStorageNodeAccessAbsentWhenRootLoginKept(t *testing.T) {
	state := cephSubscriptionExampleState(t)
	tasks, err := PlanApplyTasksChecked(applyAllTarget(), state)
	if err != nil {
		t.Fatalf("PlanApplyTasksChecked: %v", err)
	}
	for _, task := range tasks {
		if task.Entry.ID == "nodeaccess.ceph-ibm" {
			t.Fatal("node access task must not be planned when no node manages a dedicated account")
		}
	}
}

func TestPlanStorageNodeAccessRunsInMachinesPhase(t *testing.T) {
	phase, ok := applyTaskKindPhase(ApplyTaskKindStorageNodeAccess)
	if !ok {
		t.Fatal("storage node access kind has no apply phase")
	}
	if phase != ApplyPhaseMachines {
		t.Fatalf("storage node access phase = %q, want %q so the account exists before any storage task connects as it", phase, ApplyPhaseMachines)
	}
}
