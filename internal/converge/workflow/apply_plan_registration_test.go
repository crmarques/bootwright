package workflow

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/render"
	desiredstate "github.com/crmarques/bootwright/internal/state/desired"
)

func cephSubscriptionExampleState(t *testing.T) v1alpha1.State {
	t.Helper()
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join("..", "..", "..", "examples", "ceph-ibm-libvirt-lab")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	return state
}

func setRHSMManagementExternal(t *testing.T, state *v1alpha1.State, entitlementName string) {
	t.Helper()
	for i := range state.Entitlements {
		if state.Entitlements[i].Metadata.Name != entitlementName {
			continue
		}
		if state.Entitlements[i].Spec.RHSM == nil {
			t.Fatalf("entitlement %s has no rhsm arm", entitlementName)
		}
		state.Entitlements[i].Spec.RHSM = &v1alpha1.EntitlementRHSM{Management: v1alpha1.EntitlementRHSMManagementExternal}
		return
	}
	t.Fatalf("entitlement %s not found", entitlementName)
}

func TestPlanMachineRegistrationBetweenOSInstallAndStorageInfra(t *testing.T) {
	state := cephSubscriptionExampleState(t)
	tasks, err := PlanApplyTasksChecked(applyAllTarget(), state)
	if err != nil {
		t.Fatalf("PlanApplyTasksChecked: %v", err)
	}
	task := assertTaskPresent(t, tasks, "registration.ceph-ibm")
	if task.Entry.Kind != ApplyTaskKindMachineRegistration {
		t.Fatalf("registration task kind = %q, want %q", task.Entry.Kind, ApplyTaskKindMachineRegistration)
	}
	if task.Limit != render.StorageClusterGroupName("ceph-ibm") {
		t.Fatalf("registration limit = %q, want %q", task.Limit, render.StorageClusterGroupName("ceph-ibm"))
	}
	assertTaskDeps(t, tasks, "registration.ceph-ibm", "provider.bastion", "infra-component.bastion", "osinstall.ceph-ibm")
	assertTaskDeps(t, tasks, "storageinfra.ceph-ibm", "provider.bastion", "infra-component.bastion", "osinstall.ceph-ibm", "registration.ceph-ibm")
}

func TestPlanMachineRegistrationSkipsProvidedOSNode(t *testing.T) {
	state := cephSubscriptionExampleState(t)
	const providedNode = "ceph-3"
	flipped := false
	for i := range state.Machines {
		if state.Machines[i].Metadata.Name == providedNode {
			state.Machines[i].Spec.OS = v1alpha1.MachineOSSpec{Provided: v1alpha1.BoolPtr(true)}
			flipped = true
		}
	}
	if !flipped {
		t.Fatalf("precondition: machine %q not found in the ceph-ibm example", providedNode)
	}
	tasks, err := PlanApplyTasksChecked(applyAllTarget(), state)
	if err != nil {
		t.Fatalf("PlanApplyTasksChecked: %v", err)
	}
	task := assertTaskPresent(t, tasks, "registration.ceph-ibm")
	if task.Limit == render.StorageClusterGroupName("ceph-ibm") {
		t.Fatalf("registration limit must exclude the provided-OS node, got the whole group %q", task.Limit)
	}
	for _, host := range render.MachineInventoryHosts(state, providedNode) {
		if host != "" && strings.Contains(task.Limit, host) {
			t.Fatalf("registration limit %q must not target provided-OS node host %q", task.Limit, host)
		}
	}
	managedHost := render.MachineInventoryHosts(state, "ceph-1")
	if len(managedHost) == 0 || !strings.Contains(task.Limit, managedHost[0]) {
		t.Fatalf("registration limit %q must still target the managed-OS node ceph-1 (%v)", task.Limit, managedHost)
	}
}

func TestPlanMachineRegistrationSkippedWhenRHSMManagementExternal(t *testing.T) {
	state := cephSubscriptionExampleState(t)
	setRHSMManagementExternal(t, &state, "rhel")
	tasks, err := PlanApplyTasksChecked(applyAllTarget(), state)
	if err != nil {
		t.Fatalf("PlanApplyTasksChecked: %v", err)
	}
	assertTaskMissing(t, tasks, "registration.ceph-ibm")
	assertTaskPresent(t, tasks, "storageinfra.ceph-ibm")
}

func TestPlanMachineRegistrationSkippedForClustersScope(t *testing.T) {
	state := cephSubscriptionExampleState(t)
	tasks, err := PlanApplyTasksChecked(applyClustersTarget(), state)
	if err != nil {
		t.Fatalf("PlanApplyTasksChecked: %v", err)
	}
	assertTaskMissing(t, tasks, "registration.ceph-ibm")
	assertTaskPresent(t, tasks, "storageinfra.ceph-ibm")
}

func TestPlanProvisioningPlaybookAfterMachinesAnchorsOnRegistration(t *testing.T) {
	state := cephSubscriptionExampleState(t)
	state.ProvisioningPlaybooks = []v1alpha1.ProvisioningPlaybook{
		provisioningPlaybook("corporate-rhsm", v1alpha1.ProvisioningStageMachines, v1alpha1.ProvisioningPlaybookTimingAfter,
			v1alpha1.ProvisioningPlaybookTarget{Clusters: []string{"ceph-ibm"}}),
	}
	tasks, err := PlanApplyTasksChecked(applyAllTarget(), state)
	if err != nil {
		t.Fatalf("PlanApplyTasksChecked: %v", err)
	}
	hook := taskByID(t, tasks, "playbook.corporate-rhsm")
	assertDependsOn(t, hook, "registration.ceph-ibm")
	assertDependsOn(t, hook, "osinstall.ceph-ibm")
}
