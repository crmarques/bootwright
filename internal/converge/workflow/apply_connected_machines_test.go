package workflow

import (
	"slices"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	extensionplan "github.com/crmarques/bootwright/internal/addons/plan"
)

func hostTrustScopePlanningState() v1alpha1.State {
	cephMachine := func(name, address string) v1alpha1.Machine {
		return v1alpha1.Machine{
			Metadata: v1alpha1.Metadata{Name: name},
			Spec: v1alpha1.MachineSpec{
				Capabilities: []string{v1alpha1.MachineCapabilityCephNode},
				OS:           v1alpha1.MachineOSSpec{Provided: v1alpha1.BoolPtr(true)},
				Addresses:    []v1alpha1.MachineAddress{{Name: "ssh", Address: address}},
				Access: v1alpha1.MachineAccess{
					SSH: &v1alpha1.MachineSSHSpec{
						AddressRef: v1alpha1.LocalObjectReference{Name: "ssh"},
						KeyRef:     v1alpha1.SecretRef{Name: "ceph-node-ssh"},
					},
				},
			},
		}
	}
	return v1alpha1.State{
		ContainerClusters: []v1alpha1.ContainerCluster{{
			Metadata: v1alpha1.Metadata{Name: "demo"},
		}},
		Machines: []v1alpha1.Machine{
			cephMachine("ceph-seed", "10.10.10.10"),
			cephMachine("ceph-arb", "10.10.10.20"),
		},
		StorageClusters: []v1alpha1.StorageCluster{{
			Metadata: v1alpha1.Metadata{Name: "ceph"},
			Spec: v1alpha1.StorageClusterSpec{
				Type: v1alpha1.StorageClusterTypeCeph,
				Ceph: &v1alpha1.StorageClusterCephSpec{
					Cephadm: v1alpha1.StorageCephadmSpec{
						AddressRef: v1alpha1.LocalObjectReference{Name: "ssh"},
						Bootstrap:  v1alpha1.StorageCephadmBootstrap{Host: "ceph-seed"},
					},
					Topology: v1alpha1.StorageCephTopology{
						Hosts: []v1alpha1.StorageCephHost{
							{
								Hostname:   "ceph-seed",
								MachineRef: v1alpha1.LocalObjectReference{Name: "ceph-seed"},
								Site:       "dc1",
								Roles:      []string{v1alpha1.StorageCephRoleMON, v1alpha1.StorageCephRoleMGR, v1alpha1.StorageCephRoleOSD},
							},
							{
								Hostname:   "ceph-arb",
								MachineRef: v1alpha1.LocalObjectReference{Name: "ceph-arb"},
								Site:       "dc3",
								Roles:      []string{v1alpha1.StorageCephRoleMON},
							},
						},
					},
				},
			},
		}},
		StorageExports: []v1alpha1.StorageExport{{
			Metadata: v1alpha1.Metadata{Name: "export"},
			Spec: v1alpha1.StorageExportSpec{
				StorageClusterRef: v1alpha1.LocalObjectReference{Name: "ceph"},
			},
		}},
		ClusterAddons: []v1alpha1.ClusterAddon{{
			Metadata: v1alpha1.Metadata{Name: "odf"},
			Spec: v1alpha1.ClusterAddonSpec{
				Type:     v1alpha1.ClusterAddonTypeManifestSet,
				Provides: []string{v1alpha1.ClusterAddonProvidesDataFoundation},
				Accepts:  dataFoundationAccepts(),
			},
		}},
		ClusterAddonBindings: []v1alpha1.ClusterAddonBinding{{
			Metadata: v1alpha1.Metadata{Name: "ceph-binding"},
			Spec: v1alpha1.ClusterAddonBindingSpec{
				ClusterRef: v1alpha1.LocalObjectReference{Name: "demo"},
				Addons:     []v1alpha1.ClusterAddonBindingAddon{dataFoundationBindingAddon("export")},
			},
		}},
	}
}

func TestApplyTaskConnectedMachinesBaseExcludesArbiter(t *testing.T) {
	state := hostTrustScopePlanningState()
	target := ApplyTarget{Name: "base", PhaseNames: []string{ApplyPhaseBase}, StorageClusterNames: []string{"ceph"}}
	tasks, err := PlanApplyTasksChecked(target, state)
	if err != nil {
		t.Fatalf("plan base: %v", err)
	}
	if !hasTaskKind(tasks, ApplyTaskKindStorageCluster) {
		t.Fatalf("base plan did not schedule the storage cluster task: %+v", taskKinds(tasks))
	}
	connected := ApplyTaskConnectedMachines(tasks)
	if !connected["ceph-seed"] {
		t.Errorf("base plan should connect the cephadm seed; connected=%v", connected)
	}
	if connected["ceph-arb"] {
		t.Errorf("base plan must not require host trust for the arbiter; connected=%v", connected)
	}
}

func TestApplyTaskConnectedMachinesDepsIncludesArbiter(t *testing.T) {
	state := hostTrustScopePlanningState()
	target := ApplyTarget{Name: "deps", PhaseNames: []string{ApplyPhaseDeps}, StorageClusterNames: []string{"ceph"}}
	tasks, err := PlanApplyTasksChecked(target, state)
	if err != nil {
		t.Fatalf("plan deps: %v", err)
	}
	connected := ApplyTaskConnectedMachines(tasks)
	if !connected["ceph-arb"] {
		t.Errorf("deps plan should connect the arbiter; connected=%v", connected)
	}
	if !connected["ceph-seed"] {
		t.Errorf("deps plan should connect the seed; connected=%v", connected)
	}
}

func TestHookReferencedClustersPullsCrossClusterStorageIntoScope(t *testing.T) {
	state := hostTrustScopePlanningState()
	state.ClusterAddons[0].Spec.Hooks = []v1alpha1.ClusterAddonHook{{
		Name:      "seed-export",
		Lifecycle: v1alpha1.ClusterAddonHookLifecycles()[0],
		Target: v1alpha1.ClusterAddonHookTarget{
			FromInput: &v1alpha1.ClusterAddonHookInputTarget{Input: "external-storage", Property: "exportRef"},
		},
	}}
	binding := extensionplan.BindingPlan{Binding: "ceph-binding", Cluster: "demo"}

	containers, storage := hookReferencedClusters(state, binding, "odf", state.ClusterAddons[0])
	if !slices.Contains(containers, "demo") {
		t.Fatalf("the bound container cluster must stay in the addon task scope, got %v", containers)
	}
	if !slices.Contains(storage, "ceph") {
		t.Fatalf("a hook fromInput -> StorageExport must pull its storage cluster into the addon task scope so target resolution does not silently drop it, got %v", storage)
	}
}

func hasTaskKind(tasks []ApplyTask, kind string) bool {
	for _, task := range tasks {
		if task.Entry.Kind == kind {
			return true
		}
	}
	return false
}

func taskKinds(tasks []ApplyTask) []string {
	out := make([]string, 0, len(tasks))
	for _, task := range tasks {
		out = append(out, task.Entry.Kind)
	}
	return out
}
