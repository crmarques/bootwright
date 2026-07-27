package workflow

import (
	"path/filepath"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
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
	for _, machine := range []string{"dc1-child-ocp-infra-master-0", "dc1-child-ocp-infra-worker-2"} {
		assertTaskHasDeps(t, tasks, "infra.dc1-child-ocp."+machine,
			"addon.dc1-metal-ocp.openshift-data-foundation",
			"addon.dc1-metal-ocp.child-network-dc1",
			"addon.dc1-metal-ocp.openshift-virtualization",
		)
	}
	assertTaskHasDeps(t, tasks, "infra.dc2-child-ocp.dc2-child-ocp-infra-master-0",
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
	assertTaskResourceKeys(t, tasks, "infra.child-ocp.child-master-0", "kubevirt:metal-ocp:bootwright-child-ocp:vm:child-ocp-child-master-0")
	assertTaskResourceKeys(t, tasks, "infra.child-ocp.child-worker-0", "kubevirt:metal-ocp:bootwright-child-ocp:vm:child-ocp-child-worker-0")
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
	assertTaskHasDeps(t, tasks, "infra.child-ocp.child-master-0",
		"addon.metal-ocp.openshift-data-foundation",
		"addon.metal-ocp.child-network",
	)
	assertTaskDependsTransitively(t, tasks, "infra.child-ocp.child-master-0", "addon.metal-ocp.openshift-data-foundation")
	assertTaskDependsTransitively(t, tasks, "infra.child-ocp.child-master-0", "addon.metal-ocp.child-network")
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
	infra := assertTaskPresent(t, tasks, "infra.child-ocp.child-master-0")
	for _, dep := range infra.Entry.Dependencies {
		if dep == "addon.metal-ocp.unrelated" {
			t.Fatalf("infra.child-ocp.child-master-0 deps = %v, must not wait for an unreferenced add-on", infra.Entry.Dependencies)
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
	assertTaskHasDeps(t, tasks, "infra.child-ocp.child-master-0", "addon.metal-ocp.openshift-data-foundation")
}

func TestKubeVirtChildResourceRefsIgnoredWhenAddonsOutOfScope(t *testing.T) {
	state := kubeVirtChildResourceRefPlanningState()

	tasks, err := PlanApplyTasksChecked(ApplyTarget{Name: "machines", PhaseNames: []string{ApplyPhaseMachines}}, state)
	if err != nil {
		t.Fatalf("PlanApplyTasksChecked: %v", err)
	}
	assertTaskDeps(t, tasks, "infra.child-ocp.child-master-0")
}
