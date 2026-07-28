package workflow

import (
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func declareProfileRepositories(t *testing.T, state *v1alpha1.State) {
	t.Helper()
	if len(state.MachineInstallProfiles) == 0 {
		t.Fatal("fixture has no MachineInstallProfile to decorate")
	}
	state.MachineInstallProfiles[0].Spec.Customizations.Repositories = v1alpha1.MachineInstallRepositories{
		Configure: []v1alpha1.MachineInstallRepositoryFile{{
			ID:        "vendor-tools",
			BaseURL:   "https://mirror.test/vendor",
			GPGKeyURL: "https://mirror.test/KEY",
		}},
	}
}

func TestPlanMachineRepositoriesAbsentWhenNoProfileDeclaresThem(t *testing.T) {
	state := cephSubscriptionExampleState(t)
	tasks, err := PlanApplyTasksChecked(applyAllTarget(), state)
	if err != nil {
		t.Fatalf("PlanApplyTasksChecked: %v", err)
	}
	assertTaskMissing(t, tasks, "repositories.ceph-ibm")
}

func TestPlanMachineRepositoriesRunsAfterRegistration(t *testing.T) {
	state := cephSubscriptionExampleState(t)
	declareProfileRepositories(t, &state)
	tasks, err := PlanApplyTasksChecked(applyAllTarget(), state)
	if err != nil {
		t.Fatalf("PlanApplyTasksChecked: %v", err)
	}
	task := assertTaskPresent(t, tasks, "repositories.ceph-ibm")
	if task.Entry.Kind != ApplyTaskKindMachineRepositories {
		t.Fatalf("repositories task kind = %q, want %q", task.Entry.Kind, ApplyTaskKindMachineRepositories)
	}
	assertDependsOn(t, task, "registration.ceph-ibm")
	assertDependsOn(t, task, "osinstall.ceph-ibm")
}

func TestPlanStorageInfraWaitsForMachineRepositories(t *testing.T) {
	state := cephSubscriptionExampleState(t)
	declareProfileRepositories(t, &state)
	tasks, err := PlanApplyTasksChecked(applyAllTarget(), state)
	if err != nil {
		t.Fatalf("PlanApplyTasksChecked: %v", err)
	}
	assertDependsOn(t, assertTaskPresent(t, tasks, "storageinfra.ceph-ibm"), "repositories.ceph-ibm")
}

func TestPlanMachineRepositoriesSkipsProvidedOSNodes(t *testing.T) {
	state := cephSubscriptionExampleState(t)
	declareProfileRepositories(t, &state)
	for i := range state.Machines {
		state.Machines[i].Spec.OS = v1alpha1.MachineOSSpec{Provided: v1alpha1.BoolPtr(true)}
	}
	tasks, err := PlanApplyTasksChecked(applyAllTarget(), state)
	if err != nil {
		t.Fatalf("PlanApplyTasksChecked: %v", err)
	}
	assertTaskMissing(t, tasks, "repositories.ceph-ibm")
}
