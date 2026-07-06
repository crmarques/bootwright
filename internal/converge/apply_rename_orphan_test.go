package converge

import (
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/converge/workflow"
)

func newContainerClusterObject(name string) workflow.ObjectClassification {
	// A zero-value counts map means Recorded() is false — a greenfield/created object.
	return workflow.ObjectClassification{Kind: workflow.ObjectKindContainerCluster, Label: "ContainerCluster/" + name}
}

func stateWithClusters(names ...string) v1alpha1.State {
	var st v1alpha1.State
	for _, n := range names {
		st.ContainerClusters = append(st.ContainerClusters, v1alpha1.ContainerCluster{Metadata: v1alpha1.Metadata{Name: n}})
	}
	return st
}

// CheckApplyRenameOrphan fails closed only on the rename SIGNATURE: a full apply
// that creates a new cluster while another remains provisioned but undeclared. It
// must not fire for a scoped apply, a fully-declared fleet, or a pure orphan.
func TestCheckApplyRenameOrphan(t *testing.T) {
	dir := t.TempDir()
	if err := workflow.SaveClusterInstallRecord(dir, workflow.ClusterInstallRecord{
		Cluster: "prod-a", Status: workflow.ClusterInstallStatusInstalled, DesiredHash: "h",
	}); err != nil {
		t.Fatalf("seed record: %v", err)
	}
	created := []workflow.ObjectClassification{newContainerClusterObject("prod-east")}

	t.Run("rename co-occurrence refuses", func(t *testing.T) {
		if err := CheckApplyRenameOrphan(stateWithClusters("prod-east"), created, dir, false); err == nil {
			t.Fatal("a new cluster + an undeclared provisioned cluster must refuse as a possible rename")
		}
	})
	t.Run("scoped apply is suppressed", func(t *testing.T) {
		if err := CheckApplyRenameOrphan(stateWithClusters("prod-east"), created, dir, true); err != nil {
			t.Fatalf("a --clusters-scoped apply legitimately omits clusters: %v", err)
		}
	})
	t.Run("fully declared fleet is safe", func(t *testing.T) {
		if err := CheckApplyRenameOrphan(stateWithClusters("prod-a", "prod-east"), created, dir, false); err != nil {
			t.Fatalf("a declared cluster is not an orphan: %v", err)
		}
	})
	t.Run("pure orphan with no new cluster is left alone", func(t *testing.T) {
		if err := CheckApplyRenameOrphan(stateWithClusters(), nil, dir, false); err != nil {
			t.Fatalf("an undeclared cluster with no new cluster must not refuse (apply never touches it): %v", err)
		}
	})
}
