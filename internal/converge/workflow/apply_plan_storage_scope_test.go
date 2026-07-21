package workflow

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/render"
	desiredstate "github.com/crmarques/bootwright/internal/state/desired"
	stategraph "github.com/crmarques/bootwright/internal/state/graph"
)

func machineScopedApplyTarget(state v1alpha1.State, machines ...string) ApplyTarget {
	provision, hosts := stategraph.MachineWorkObjects(state, machines)
	return ApplyTarget{
		Name:             "infra",
		PhaseNames:       []string{ApplyPhaseFabric, ApplyPhaseMachines},
		MachineProvision: provision,
		MachineHosts:     hosts,
	}
}

func TestManagedStorageOSInstallCoalescesSelectedMachines(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join("..", "..", "..", "test", "e2e", "006-ceph-3nodes-libvirt-managed-os")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	tasks, err := PlanApplyTasksChecked(machineScopedApplyTarget(state, "ceph-0", "ceph-1"), state)
	if err != nil {
		t.Fatalf("PlanApplyTasksChecked: %v", err)
	}

	task := assertTaskPresent(t, tasks, "osinstall.ceph-libvirt")
	wantLimit := render.ManagedOSHostName("ceph-libvirt", "ceph-0") + ":" + render.ManagedOSHostName("ceph-libvirt", "ceph-1")
	if task.Limit != wantLimit {
		t.Fatalf("scoped managed OS limit = %q, want %q (one grouped run over the selected hosts, not per-machine)", task.Limit, wantLimit)
	}
	if task.Limit == render.ManagedOSGroupName("ceph-libvirt") {
		t.Fatalf("scoped managed OS must not target the whole group; got %q", task.Limit)
	}
	if task.Forks != 2 || task.RedfishSlots != 2 {
		t.Fatalf("scoped managed OS forks/redfish = %d/%d, want 2/2", task.Forks, task.RedfishSlots)
	}
	assertTaskMissing(t, tasks, "osinstall.ceph-libvirt.ceph-0")
	assertTaskMissing(t, tasks, "osinstall.ceph-libvirt.ceph-1")
	assertTaskMissing(t, tasks, "osinstall.ceph-libvirt.ceph-2")
}

func TestMachineRegistrationCoalescesSelectedMachines(t *testing.T) {
	state := cephSubscriptionExampleState(t)
	tasks, err := PlanApplyTasksChecked(machineScopedApplyTarget(state, "ceph-1", "ceph-2"), state)
	if err != nil {
		t.Fatalf("PlanApplyTasksChecked: %v", err)
	}

	task := assertTaskPresent(t, tasks, "registration.ceph-ibm")
	wantHosts := append(append([]string{}, render.MachineInventoryHosts(state, "ceph-1")...), render.MachineInventoryHosts(state, "ceph-2")...)
	wantLimit := strings.Join(wantHosts, ":")
	if task.Limit != wantLimit {
		t.Fatalf("scoped registration limit = %q, want %q (one grouped run over the selected hosts, not per-machine)", task.Limit, wantLimit)
	}
	if task.Limit == render.StorageClusterGroupName("ceph-ibm") {
		t.Fatalf("scoped registration must not target the whole cluster group; got %q", task.Limit)
	}
	if task.Forks != len(wantHosts) {
		t.Fatalf("scoped registration forks = %d, want %d", task.Forks, len(wantHosts))
	}
	assertTaskMissing(t, tasks, "registration.ceph-ibm.ceph-1")
	assertTaskMissing(t, tasks, "registration.ceph-ibm.ceph-2")
}
