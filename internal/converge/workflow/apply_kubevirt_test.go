package workflow

import (
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func TestKubeVirtMachineTasksUsePerVMResourceKeys(t *testing.T) {
	state := kubeVirtChildPlanningState(true)
	secondMachine := state.Machines[0]
	secondMachine.Metadata.Name = "child-worker-0"
	state.Machines = append(state.Machines, secondMachine)
	state.ContainerClusters[0].Spec.Nodes = append(state.ContainerClusters[0].Spec.Nodes, v1alpha1.OCPNodeSpec{
		Name:       "worker-0",
		MachineRef: v1alpha1.LocalObjectReference{Name: "child-worker-0"},
	})

	tasks, err := PlanApplyTasksChecked(applyAllTarget(), state)
	if err != nil {
		t.Fatalf("PlanApplyTasksChecked: %v", err)
	}
	assertTaskResourceKeys(t, tasks, "infra.child-ocp.child-master-0", "kubevirt:metal-ocp:bootwright-child-ocp:vm:child-ocp-child-master-0")
	assertTaskResourceKeys(t, tasks, "infra.child-ocp.child-worker-0", "kubevirt:metal-ocp:bootwright-child-ocp:vm:child-ocp-child-worker-0")
	assertTaskResourceKeys(t, tasks, "boot.child-ocp",
		"kubevirt:metal-ocp:bootwright-child-ocp:vm:child-ocp-child-master-0",
		"kubevirt:metal-ocp:bootwright-child-ocp:vm:child-ocp-child-worker-0",
	)
}
