package workflow

import (
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/render"
	desiredstate "github.com/crmarques/bootwright/internal/state/desired"
)

func TestMultiDCExampleOrdersChildVMsAfterStorageAndNetworkAddons(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join("..", "..", "..", "examples", "baremetal-redfish-multidc-virtualized-odf-ceph")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	tasks, err := PlanApplyTasksChecked(applyAllTarget(), state)
	if err != nil {
		t.Fatalf("PlanApplyTasksChecked: %v", err)
	}
	dc1 := assertTaskPresent(t, tasks, "infra.dc1-child-ocp.localhost")
	for _, machine := range []string{"dc1-child-ocp-infra-master-0", "dc1-child-ocp-infra-worker-2"} {
		if !slices.Contains(dc1.Entry.Nodes, machine) {
			t.Fatalf("infra.dc1-child-ocp.localhost nodes = %v, want to provision %s in the same run", dc1.Entry.Nodes, machine)
		}
	}
	assertTaskHasDeps(t, tasks, "infra.dc1-child-ocp.localhost",
		"addon.dc1-metal-ocp.openshift-data-foundation",
		"addon.dc1-metal-ocp.child-network-dc1",
		"addon.dc1-metal-ocp.openshift-virtualization",
	)
	assertTaskHasDeps(t, tasks, "infra.dc2-child-ocp.localhost",
		"addon.dc2-metal-ocp.openshift-data-foundation",
		"addon.dc2-metal-ocp.child-network-dc2",
	)
	assertNoTaskPath(t, tasks, "addon.dc1-metal-ocp.child-network-dc1", "addon.dc1-metal-ocp.openshift-data-foundation")
	assertNoTaskPath(t, tasks, "addon.dc1-metal-ocp.openshift-data-foundation", "addon.dc1-metal-ocp.child-network-dc1")
	assertNoTaskPath(t, tasks, "iso.dc1-child-ocp", "wait.dc1-metal-ocp")
}

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
	assertTaskResourceKeys(t, tasks, "infra.child-ocp.localhost",
		"kubevirt:metal-ocp:bootwright-child-ocp:vm:child-ocp-child-master-0",
		"kubevirt:metal-ocp:bootwright-child-ocp:vm:child-ocp-child-worker-0",
	)
	assertTaskResourceKeys(t, tasks, "boot.child-ocp",
		"kubevirt:metal-ocp:bootwright-child-ocp:vm:child-ocp-child-master-0",
		"kubevirt:metal-ocp:bootwright-child-ocp:vm:child-ocp-child-worker-0",
	)
}

func kubeVirtChildResourceRefPlanningState() v1alpha1.State {
	state := kubeVirtChildPlanningState(true)
	provider := state.InfraProviders[0]
	provider.Spec.KubeVirt.StorageClassRef = &v1alpha1.LocalObjectReference{Name: "ocs-external-storagecluster-ceph-rbd"}
	provider.Spec.NetworkAttachments = []v1alpha1.NetworkAttachmentCapability{{
		Name: "child-net",
		KubeVirt: &v1alpha1.NetworkAttachmentKubeVirt{
			NetworkRef: v1alpha1.KubeVirtNetworkRef{
				APIGroup:  v1alpha1.KubeVirtNetworkGroupOVN,
				Kind:      v1alpha1.KubeVirtNetworkKindCUDN,
				Name:      "child-net",
				Namespace: "bootwright-child-ocp",
			},
		},
	}}
	state.InfraProviders = []v1alpha1.InfraProvider{provider}
	state.ClusterAddons = append(state.ClusterAddons,
		v1alpha1.ClusterAddon{
			Metadata: v1alpha1.Metadata{Name: "openshift-data-foundation"},
			Spec: v1alpha1.ClusterAddonSpec{
				Type:     v1alpha1.ClusterAddonTypeManifestSet,
				Provides: []string{v1alpha1.ClusterAddonProvidesDataFoundation},
			},
		},
		v1alpha1.ClusterAddon{
			Metadata: v1alpha1.Metadata{Name: "child-network"},
			Spec: v1alpha1.ClusterAddonSpec{
				Type: v1alpha1.ClusterAddonTypeManifestSet,
				Readiness: v1alpha1.ClusterAddonReadiness{
					Checks: []v1alpha1.ClusterAddonReadinessCheck{{
						ResourceExists: &v1alpha1.ClusterAddonResourceExistsReadiness{
							APIVersion: v1alpha1.KubeVirtNetworkGroupOVN + "/v1",
							Kind:       v1alpha1.KubeVirtNetworkKindCUDN,
							Name:       "child-net",
						},
					}},
				},
			},
		},
	)
	state.ClusterAddonBindings[0].Spec.AddonRefs = append(state.ClusterAddonBindings[0].Spec.AddonRefs,
		v1alpha1.LocalObjectReference{Name: "openshift-data-foundation"},
		v1alpha1.LocalObjectReference{Name: "child-network"},
	)
	return state
}

func TestKubeVirtChildInfraWaitsForReferencedStorageAndNetworkAddons(t *testing.T) {
	state := kubeVirtChildResourceRefPlanningState()

	tasks, err := PlanApplyTasksChecked(applyAllTarget(), state)
	if err != nil {
		t.Fatalf("PlanApplyTasksChecked: %v", err)
	}
	assertTaskHasDeps(t, tasks, "infra.child-ocp.localhost",
		"addon.metal-ocp.openshift-data-foundation",
		"addon.metal-ocp.child-network",
	)
	assertTaskDependsTransitively(t, tasks, "infra.child-ocp.localhost", "addon.metal-ocp.openshift-data-foundation")
	assertTaskDependsTransitively(t, tasks, "infra.child-ocp.localhost", "addon.metal-ocp.child-network")
	assertTaskDeps(t, tasks, "addon.metal-ocp.openshift-data-foundation", "wait.metal-ocp")
	assertTaskDeps(t, tasks, "addon.metal-ocp.child-network", "wait.metal-ocp")
}

func TestKubeVirtChildInfraIgnoresUnrelatedAddons(t *testing.T) {
	state := kubeVirtChildResourceRefPlanningState()
	state.ClusterAddons = append(state.ClusterAddons, v1alpha1.ClusterAddon{
		Metadata: v1alpha1.Metadata{Name: "unrelated"},
		Spec:     v1alpha1.ClusterAddonSpec{Type: v1alpha1.ClusterAddonTypeManifestSet},
	})
	state.ClusterAddonBindings[0].Spec.AddonRefs = append(state.ClusterAddonBindings[0].Spec.AddonRefs,
		v1alpha1.LocalObjectReference{Name: "unrelated"})

	tasks, err := PlanApplyTasksChecked(applyAllTarget(), state)
	if err != nil {
		t.Fatalf("PlanApplyTasksChecked: %v", err)
	}
	infra := assertTaskPresent(t, tasks, "infra.child-ocp.localhost")
	for _, dep := range infra.Entry.Dependencies {
		if dep == "addon.metal-ocp.unrelated" {
			t.Fatalf("infra.child-ocp.localhost deps = %v, must not wait for an unreferenced add-on", infra.Entry.Dependencies)
		}
	}
}

func TestKubeVirtChildStorageClassRefResolvesDeclaredStorageClass(t *testing.T) {
	state := kubeVirtChildResourceRefPlanningState()
	for i, addon := range state.ClusterAddons {
		if addon.Metadata.Name != "openshift-data-foundation" {
			continue
		}
		state.ClusterAddons[i].Spec.Provides = nil
		state.ClusterAddons[i].Spec.Readiness.Checks = []v1alpha1.ClusterAddonReadinessCheck{{
			ResourceExists: &v1alpha1.ClusterAddonResourceExistsReadiness{
				APIVersion: "storage.k8s.io/v1",
				Kind:       "StorageClass",
				Name:       "ocs-external-storagecluster-ceph-rbd",
			},
		}}
	}

	tasks, err := PlanApplyTasksChecked(applyAllTarget(), state)
	if err != nil {
		t.Fatalf("PlanApplyTasksChecked: %v", err)
	}
	assertTaskHasDeps(t, tasks, "infra.child-ocp.localhost", "addon.metal-ocp.openshift-data-foundation")
}

func TestKubeVirtChildResourceRefsIgnoredWhenAddonsOutOfScope(t *testing.T) {
	state := kubeVirtChildResourceRefPlanningState()

	tasks, err := PlanApplyTasksChecked(ApplyTarget{Name: "machines", PhaseNames: []string{ApplyPhaseMachines}}, state)
	if err != nil {
		t.Fatalf("PlanApplyTasksChecked: %v", err)
	}
	assertTaskDeps(t, tasks, "infra.child-ocp.localhost")
}

func TestKubeVirtInfraTaskProvisionsEveryMachineInOneRun(t *testing.T) {
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
	infraIDs := []string(nil)
	for _, task := range tasks {
		if strings.HasPrefix(task.Entry.ID, "infra.child-ocp.") {
			infraIDs = append(infraIDs, task.Entry.ID)
		}
	}
	if len(infraIDs) != 1 || infraIDs[0] != "infra.child-ocp.localhost" {
		t.Fatalf("machine infra tasks = %v, want one grouped run so a task banner covers every machine", infraIDs)
	}
	task := assertTaskPresent(t, tasks, "infra.child-ocp.localhost")
	if got, want := task.Forks, 2; got != want {
		t.Fatalf("forks = %d, want %d so every machine converges concurrently inside the one run", got, want)
	}
	wantLimit := render.MachineInfraHostName("child-ocp", "child-master-0") + ":" + render.MachineInfraHostName("child-ocp", "child-worker-0")
	if task.Limit != wantLimit {
		t.Fatalf("limit = %q, want the explicit selected machine hosts %q", task.Limit, wantLimit)
	}
	if task.HostSlotKey != "" || task.Entry.HostSlotKey != "" {
		t.Fatalf("host slot key = %q/%q, want empty", task.HostSlotKey, task.Entry.HostSlotKey)
	}
	if got, want := task.Entry.Nodes, []string{"child-master-0", "child-worker-0"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("nodes = %v, want %v", got, want)
	}
	if task.Entry.Node != "" {
		t.Fatalf("node = %q, want empty because the task covers more than one machine", task.Entry.Node)
	}
}

func TestKubeVirtMachineTasksChargeNoHostSlot(t *testing.T) {
	state := kubeVirtChildPlanningState(true)
	tasks, err := PlanApplyTasksChecked(applyAllTarget(), state)
	if err != nil {
		t.Fatalf("PlanApplyTasksChecked: %v", err)
	}
	task := assertTaskPresent(t, tasks, "infra.child-ocp.localhost")
	if key, count := taskHostSlot(task); key != "" || count != 0 {
		t.Fatalf("taskHostSlot = (%q, %d), want (\"\", 0): independent VMs in one namespace must converge concurrently, and a shared slot key would clamp dispatch to ParallelismPerHost", key, count)
	}
	if !taskHostSlotAvailable(task, map[string]int{"kubevirt:metal-ocp:bootwright-child-ocp": 99}, 1) {
		t.Fatal("KubeVirt machine provisioning must not be gated by any host slot budget")
	}
}
