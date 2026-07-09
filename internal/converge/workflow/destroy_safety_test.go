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

	if d := EvaluateDestroySafety(withStorage, false); !d.RequiredOverride {
		t.Fatalf("protected StorageCluster in scope must require override, got %+v", d)
	}
	if d := EvaluateDestroySafety(withStorage, true); d.RequiredOverride {
		t.Fatal("--override must clear the granular gate")
	}
	if d := EvaluateDestroySafety(containerOnly, false); d.RequiredOverride {
		t.Fatal("a protected kind absent from the scope must not gate the teardown")
	}
}
