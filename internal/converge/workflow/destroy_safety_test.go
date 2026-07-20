package workflow

import (
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func TestEvaluateDestroySafetyProtectedKinds(t *testing.T) {
	protectStorage := v1alpha1.State{Environments: []v1alpha1.Environment{{
		Metadata: v1alpha1.Metadata{Name: "nprd"},
		Spec:     v1alpha1.EnvironmentSpec{Safety: v1alpha1.EnvironmentSafetySpec{ProtectedKinds: []string{v1alpha1.KindStorageCluster}}},
	}}}
	withStorage := protectStorage
	withStorage.StorageClusters = []v1alpha1.StorageCluster{{Metadata: v1alpha1.Metadata{Name: "ceph"}}}
	containerOnly := protectStorage
	containerOnly.ContainerClusters = []v1alpha1.ContainerCluster{{Metadata: v1alpha1.Metadata{Name: "ocp"}}}

	clusterScope := DestroySafetyScope{TearsClusters: true}
	if d := EvaluateDestroySafety(withStorage, false, nil, clusterScope); !d.RequiredOverride {
		t.Fatalf("protected StorageCluster in scope must require override, got %+v", d)
	}
	if d := EvaluateDestroySafety(withStorage, true, nil, clusterScope); d.RequiredOverride {
		t.Fatal("--force must clear the granular gate")
	}
	if d := EvaluateDestroySafety(containerOnly, false, nil, clusterScope); d.RequiredOverride {
		t.Fatal("a protected kind absent from the scope must not gate the teardown")
	}
	if d := EvaluateDestroySafety(withStorage, false, []string{}, clusterScope); d.RequiredOverride {
		t.Fatal("a render-reference StorageCluster outside the teardown work set must not gate")
	}
	if d := EvaluateDestroySafety(withStorage, false, []string{"ceph"}, clusterScope); !d.RequiredOverride {
		t.Fatal("a StorageCluster in the teardown work set must gate")
	}
	protectMachines := v1alpha1.State{
		Environments: []v1alpha1.Environment{{
			Metadata: v1alpha1.Metadata{Name: "nprd"},
			Spec:     v1alpha1.EnvironmentSpec{Safety: v1alpha1.EnvironmentSafetySpec{ProtectedKinds: []string{v1alpha1.KindMachine}}},
		}},
		Machines: []v1alpha1.Machine{{Metadata: v1alpha1.Metadata{Name: "bastion"}}},
	}
	if d := EvaluateDestroySafety(protectMachines, false, nil, clusterScope); d.RequiredOverride {
		t.Fatal("protected Machines must not gate a clusters-stage teardown that never touches machines")
	}
	if d := EvaluateDestroySafety(protectMachines, false, nil, DestroySafetyScope{TearsMachines: true}); !d.RequiredOverride {
		t.Fatal("protected Machines must gate an infra-stage teardown")
	}
	if d := EvaluateDestroySafety(withStorage, false, nil, DestroySafetyScope{}); d.RequiredOverride {
		t.Fatal("an artifact-server-only teardown must not trip protectedKinds")
	}
}
