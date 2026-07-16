package converge

import (
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/converge/workflow"
)

func newContainerClusterObject(name string) workflow.ObjectClassification {
	return workflow.ObjectClassification{Kind: workflow.ObjectKindContainerCluster, Label: "ContainerCluster/" + name}
}

func stateWithClusters(names ...string) v1alpha1.State {
	var st v1alpha1.State
	for _, n := range names {
		st.ContainerClusters = append(st.ContainerClusters, v1alpha1.ContainerCluster{Metadata: v1alpha1.Metadata{Name: n}})
	}
	return st
}

func TestCheckApplyRenameOrphan(t *testing.T) {
	dir := t.TempDir()
	if err := workflow.SaveClusterInstallRecord(dir, workflow.ClusterInstallRecord{
		Cluster: "prod-a", Status: workflow.ClusterInstallStatusInstalled, DesiredHash: "h",
	}); err != nil {
		t.Fatalf("seed record: %v", err)
	}
	created := []workflow.ObjectClassification{newContainerClusterObject("prod-east")}

	t.Run("rename co-occurrence refuses", func(t *testing.T) {
		if err := CheckApplyRenameOrphan(stateWithClusters("prod-east"), created, dir); err == nil {
			t.Fatal("a new cluster + an undeclared provisioned cluster must refuse as a possible rename")
		}
	})
	t.Run("scoped apply refuses against the full declared state", func(t *testing.T) {
		if err := CheckApplyRenameOrphan(stateWithClusters("prod-east"), created, dir); err == nil {
			t.Fatal("a --clusters-scoped apply must still refuse when the full state no longer declares a provisioned cluster")
		}
	})
	t.Run("fully declared fleet is safe", func(t *testing.T) {
		if err := CheckApplyRenameOrphan(stateWithClusters("prod-a", "prod-east"), created, dir); err != nil {
			t.Fatalf("a declared cluster is not an orphan: %v", err)
		}
	})
	t.Run("pure orphan with no new cluster is left alone", func(t *testing.T) {
		if err := CheckApplyRenameOrphan(stateWithClusters(), nil, dir); err != nil {
			t.Fatalf("an undeclared cluster with no new cluster must not refuse (apply never touches it): %v", err)
		}
	})
}
