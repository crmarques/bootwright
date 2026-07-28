package workflow

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/render"
	desiredstate "github.com/crmarques/bootwright/internal/state/desired"
)

func cephManagedOSExampleState(t *testing.T) v1alpha1.State {
	t.Helper()
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join("..", "..", "..", "test", "e2e", "006-ceph-3nodes-libvirt-managed-os")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	return state
}

func TestPlaybookMachineScopeNarrowsStorageClusterTarget(t *testing.T) {
	state := cephManagedOSExampleState(t)
	state.CustomPlaybooks = []v1alpha1.CustomPlaybook{
		provisioningPlaybook("node-hook", v1alpha1.CustomPlaybookAnchorMachines, anchorKeyFollows,
			v1alpha1.CustomPlaybookTarget{Clusters: []string{"ceph-libvirt"}}),
	}
	tasks, err := PlanApplyTasksChecked(machineScopedApplyTarget(state, "ceph-0"), state)
	if err != nil {
		t.Fatalf("PlanApplyTasksChecked: %v", err)
	}

	task := assertTaskPresent(t, tasks, "playbook.node-hook")
	wantLimit := strings.Join(render.MachineInventoryHosts(state, "ceph-0"), ":")
	if task.Limit != wantLimit {
		t.Fatalf("machine-scoped playbook limit = %q, want %q (only the selected machine's hosts)", task.Limit, wantLimit)
	}
	if task.Limit == render.StorageClusterGroupName("ceph-libvirt") {
		t.Fatalf("machine-scoped playbook must not target the whole cluster group; got %q", task.Limit)
	}
	for _, unselected := range []string{"ceph-1", "ceph-2"} {
		for _, host := range render.MachineInventoryHosts(state, unselected) {
			if strings.Contains(task.Limit, host) {
				t.Fatalf("machine-scoped playbook limit %q reaches unselected machine %s (host %s)", task.Limit, unselected, host)
			}
		}
	}
}

func TestPlaybookMachineScopeNarrowsContainerClusterTarget(t *testing.T) {
	state := loadWorkflowFixtureState(t, "003-3nodes-libvirt")
	state.CustomPlaybooks = []v1alpha1.CustomPlaybook{
		provisioningPlaybook("node-hook", v1alpha1.CustomPlaybookAnchorMachines, anchorKeyFollows,
			v1alpha1.CustomPlaybookTarget{Clusters: []string{"3-nodes-ocp-libvirt"}}),
	}
	tasks, err := PlanApplyTasksChecked(machineScopedApplyTarget(state, "master-1"), state)
	if err != nil {
		t.Fatalf("PlanApplyTasksChecked: %v", err)
	}

	task := assertTaskPresent(t, tasks, "playbook.node-hook")
	wantLimit := strings.Join(render.MachineInventoryHosts(state, "master-1"), ":")
	if task.Limit != wantLimit {
		t.Fatalf("machine-scoped playbook limit = %q, want %q (only the selected machine's hosts)", task.Limit, wantLimit)
	}
	if task.Entry.Cluster != "3-nodes-ocp-libvirt" {
		t.Fatalf("narrowed playbook cluster = %q, want 3-nodes-ocp-libvirt", task.Entry.Cluster)
	}
}

func TestPlaybookMachineScopeSkipsPlaybookWithoutSelectedHost(t *testing.T) {
	state := cephManagedOSExampleState(t)
	state.CustomPlaybooks = []v1alpha1.CustomPlaybook{
		provisioningPlaybook("other-node-hook", v1alpha1.CustomPlaybookAnchorMachines, anchorKeyFollows,
			v1alpha1.CustomPlaybookTarget{Machines: []string{"ceph-2"}}),
	}
	tasks, err := PlanApplyTasksChecked(machineScopedApplyTarget(state, "ceph-0"), state)
	if err != nil {
		t.Fatalf("a playbook targeting machines outside the selection must be skipped, not an error: %v", err)
	}
	assertTaskMissing(t, tasks, "playbook.other-node-hook")
}

func TestPlaybookWithoutMachineScopeKeepsClusterGroupLimit(t *testing.T) {
	state := cephManagedOSExampleState(t)
	state.CustomPlaybooks = []v1alpha1.CustomPlaybook{
		provisioningPlaybook("node-hook", v1alpha1.CustomPlaybookAnchorMachines, anchorKeyFollows,
			v1alpha1.CustomPlaybookTarget{Clusters: []string{"ceph-libvirt"}}),
	}
	tasks, err := PlanApplyTasksChecked(applyAllTarget(), state)
	if err != nil {
		t.Fatalf("PlanApplyTasksChecked: %v", err)
	}

	task := assertTaskPresent(t, tasks, "playbook.node-hook")
	if task.Limit != render.StorageClusterGroupName("ceph-libvirt") {
		t.Fatalf("unscoped playbook limit = %q, want the cluster group %q", task.Limit, render.StorageClusterGroupName("ceph-libvirt"))
	}
}
