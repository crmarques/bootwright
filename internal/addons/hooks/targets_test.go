package hooks

import (
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
	hook := v1alpha1.ClusterAddonHook{
		Target: v1alpha1.ClusterAddonHookTarget{
			FromInput: &v1alpha1.ClusterAddonHookInputTarget{Input: "external-storage"},
		},
	}
	inputs := []v1alpha1.ClusterAddonBindingInput{{Name: "external-storage", Value: "odf-export"}}
	containers, storage := TargetClusters(state, dfAddon(), "metal-ocp", hook, inputs)
	if len(containers) != 0 {
		t.Errorf("containers = %v want none", containers)
	}
	if len(storage) != 1 || storage[0] != "ceph" {
		t.Errorf("storage = %v want [ceph]", storage)
	}
}

func TestTargetClustersBoundCluster(t *testing.T) {
	hook := v1alpha1.ClusterAddonHook{Target: v1alpha1.ClusterAddonHookTarget{BoundCluster: &v1alpha1.ClusterAddonHookBoundTarget{}}}
	containers, storage := TargetClusters(v1alpha1.State{}, dfAddon(), "metal-ocp", hook, nil)
	if len(containers) != 1 || containers[0] != "metal-ocp" {
		t.Errorf("containers = %v want [metal-ocp]", containers)
	}
	if len(storage) != 0 {
		t.Errorf("storage = %v want none", storage)
	}
}

func TestTargetClustersFromInputMissingValueNoPanic(t *testing.T) {
	hook := v1alpha1.ClusterAddonHook{
		Target: v1alpha1.ClusterAddonHookTarget{
			FromInput: &v1alpha1.ClusterAddonHookInputTarget{Input: "external-storage"},
		},
	}
	containers, storage := TargetClusters(v1alpha1.State{}, dfAddon(), "metal-ocp", hook, nil)
	if len(containers) != 0 || len(storage) != 0 {
		t.Errorf("unresolved fromInput should yield no clusters, got %v %v", containers, storage)
	}
}
