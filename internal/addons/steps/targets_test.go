package steps

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func dfAddon() v1alpha1.ClusterAddon {
	return v1alpha1.ClusterAddon{
		Metadata: v1alpha1.Metadata{Name: "odf"},
		Spec: v1alpha1.ClusterAddonSpec{
			Accepts: v1alpha1.ClusterAddonAccepts{
				Inputs: []v1alpha1.ClusterAddonAcceptedInput{{
					Name:        "external-storage",
					ResourceRef: &v1alpha1.ClusterAddonInputRef{Kind: v1alpha1.KindStorageExport},
				}},
			},
		},
	}
}

func TestTargetClustersFromInputStorageExport(t *testing.T) {
	state := v1alpha1.State{
		StorageExports: []v1alpha1.StorageExport{{
			Metadata: v1alpha1.Metadata{Name: "odf-export"},
			Spec:     v1alpha1.StorageExportSpec{StorageClusterRef: v1alpha1.LocalObjectReference{Name: "ceph"}},
		}},
	}
	step := v1alpha1.ClusterAddonStep{
		Target: v1alpha1.ClusterAddonStepTarget{
			FromInput: &v1alpha1.ClusterAddonStepInputTarget{Input: "external-storage"},
		},
	}
	inputs := []v1alpha1.ClusterAddonBindingInput{{Name: "external-storage", Value: "odf-export"}}
	containers, storage := TargetClusters(state, dfAddon(), "metal-ocp", step, inputs)
	if len(containers) != 0 {
		t.Errorf("containers = %v want none", containers)
	}
	if len(storage) != 1 || storage[0] != "ceph" {
		t.Errorf("storage = %v want [ceph]", storage)
	}
}

func TestTargetClustersBoundCluster(t *testing.T) {
	step := v1alpha1.ClusterAddonStep{Target: v1alpha1.ClusterAddonStepTarget{BoundCluster: &v1alpha1.ClusterAddonStepBoundTarget{}}}
	containers, storage := TargetClusters(v1alpha1.State{}, dfAddon(), "metal-ocp", step, nil)
	if len(containers) != 1 || containers[0] != "metal-ocp" {
		t.Errorf("containers = %v want [metal-ocp]", containers)
	}
	if len(storage) != 0 {
		t.Errorf("storage = %v want none", storage)
	}
}

func TestTargetClustersFromInputMissingValueNoPanic(t *testing.T) {
	step := v1alpha1.ClusterAddonStep{
		Target: v1alpha1.ClusterAddonStepTarget{
			FromInput: &v1alpha1.ClusterAddonStepInputTarget{Input: "external-storage"},
		},
	}
	containers, storage := TargetClusters(v1alpha1.State{}, dfAddon(), "metal-ocp", step, nil)
	if len(containers) != 0 || len(storage) != 0 {
		t.Errorf("unresolved fromInput should yield no clusters, got %v %v", containers, storage)
	}
}

func TestStorageMutationTargetsResolveStorageExportExactly(t *testing.T) {
	state := v1alpha1.State{
		StorageClusters: []v1alpha1.StorageCluster{{Metadata: v1alpha1.Metadata{Name: "ceph"}}},
		StorageExports: []v1alpha1.StorageExport{{
			Metadata: v1alpha1.Metadata{Name: "odf-export"},
			Spec:     v1alpha1.StorageExportSpec{StorageClusterRef: v1alpha1.LocalObjectReference{Name: "ceph"}},
		}},
	}
	step := v1alpha1.ClusterAddonStep{
		Name:     "attach-external-storage",
		Playbook: "playbooks/export.yaml",
		Target: v1alpha1.ClusterAddonStepTarget{
			FromInput: &v1alpha1.ClusterAddonStepInputTarget{Input: "external-storage"},
		},
	}
	inputs := []v1alpha1.ClusterAddonBindingInput{{Name: "external-storage", Value: "odf-export"}}
	targets, err := StorageMutationTargets(state, dfAddon(), "metal-ocp", step, inputs)
	if err != nil {
		t.Fatalf("StorageMutationTargets: %v", err)
	}
	if len(targets) != 1 || targets[0] != "ceph" {
		t.Fatalf("storage mutation targets = %v, want [ceph]", targets)
	}
}

func TestStorageMutationTargetsRefuseUnknownExportCluster(t *testing.T) {
	state := v1alpha1.State{
		StorageExports: []v1alpha1.StorageExport{{
			Metadata: v1alpha1.Metadata{Name: "odf-export"},
			Spec:     v1alpha1.StorageExportSpec{StorageClusterRef: v1alpha1.LocalObjectReference{Name: "missing-ceph"}},
		}},
	}
	step := v1alpha1.ClusterAddonStep{
		Name:     "attach-external-storage",
		Playbook: "playbooks/export.yaml",
		Target: v1alpha1.ClusterAddonStepTarget{
			FromInput: &v1alpha1.ClusterAddonStepInputTarget{Input: "external-storage"},
		},
	}
	inputs := []v1alpha1.ClusterAddonBindingInput{{Name: "external-storage", Value: "odf-export"}}
	_, err := StorageMutationTargets(state, dfAddon(), "metal-ocp", step, inputs)
	if err == nil {
		t.Fatal("StorageMutationTargets accepted an export whose mutation target is unknown")
	}
	for _, want := range []string{"StorageExport/odf-export", "StorageCluster", "missing-ceph"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not contain %q", err, want)
		}
	}
}

func TestStorageMutationTargetsKeepUnrelatedAndManifestOnlyStepsUnlocked(t *testing.T) {
	state := v1alpha1.State{
		ContainerClusters: []v1alpha1.ContainerCluster{{Metadata: v1alpha1.Metadata{Name: "metal-ocp"}}},
	}
	bound := v1alpha1.ClusterAddonStep{
		Playbook: "playbooks/configure.yaml",
		Target:   v1alpha1.ClusterAddonStepTarget{BoundCluster: &v1alpha1.ClusterAddonStepBoundTarget{}},
	}
	targets, err := StorageMutationTargets(state, dfAddon(), "metal-ocp", bound, nil)
	if err != nil || len(targets) != 0 {
		t.Fatalf("bound-cluster targets = %v err=%v, want no storage serialization", targets, err)
	}
	manifestOnly := v1alpha1.ClusterAddonStep{Manifests: []v1alpha1.ClusterAddonStepManifest{{Path: "manifests/config.yaml"}}}
	targets, err = StorageMutationTargets(v1alpha1.State{}, dfAddon(), "", manifestOnly, nil)
	if err != nil || len(targets) != 0 {
		t.Fatalf("manifest-only targets = %v err=%v, want no storage serialization", targets, err)
	}
}

func TestStorageMutationTargetsSortAndDedupeStaticMachineOwners(t *testing.T) {
	state := v1alpha1.State{
		Machines: []v1alpha1.Machine{{Metadata: v1alpha1.Metadata{Name: "shared-node"}}},
		StorageClusters: []v1alpha1.StorageCluster{
			{Metadata: v1alpha1.Metadata{Name: "zeta"}, Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{Topology: v1alpha1.StorageCephTopology{Nodes: []v1alpha1.StorageCephNode{{MachineRef: v1alpha1.LocalObjectReference{Name: "shared-node"}}}}}}},
			{Metadata: v1alpha1.Metadata{Name: "alpha"}, Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{Topology: v1alpha1.StorageCephTopology{Nodes: []v1alpha1.StorageCephNode{{MachineRef: v1alpha1.LocalObjectReference{Name: "shared-node"}}}}}}},
		},
	}
	step := v1alpha1.ClusterAddonStep{
		Playbook: "playbooks/configure.yaml",
		Target: v1alpha1.ClusterAddonStepTarget{Static: &v1alpha1.ClusterAddonStepStaticTarget{
			Clusters: []string{"zeta"},
			Machines: []string{"shared-node"},
		}},
	}
	targets, err := StorageMutationTargets(state, dfAddon(), "", step, nil)
	if err != nil {
		t.Fatalf("StorageMutationTargets: %v", err)
	}
	if len(targets) != 2 || targets[0] != "alpha" || targets[1] != "zeta" {
		t.Fatalf("storage mutation targets = %v, want [alpha zeta]", targets)
	}
}
