package clusteraccess

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func TestStorageRenderStateDropsContainerClusters(t *testing.T) {
	state := loadFixtureState(t, cephFixture)
	state.ContainerClusters = []v1alpha1.ContainerCluster{{Metadata: v1alpha1.Metadata{Name: "phantom-ocp"}}}

	rendered, err := StorageRenderState(state, "")
	if err != nil {
		t.Fatalf("StorageRenderState: %v", err)
	}
	if len(rendered.ContainerClusters) != 0 {
		t.Fatalf("storage render must drop container clusters; got %+v", rendered.ContainerClusters)
	}
	if len(rendered.StorageClusters) != 1 || rendered.StorageClusters[0].Metadata.Name != "ceph-libvirt" {
		t.Fatalf("storage render must keep the storage cluster; got %+v", rendered.StorageClusters)
	}
}

func TestStorageRenderStateRejectsUnknownCluster(t *testing.T) {
	state := loadFixtureState(t, cephFixture)

	if _, err := StorageRenderState(state, "no-such"); err == nil {
		t.Fatal("StorageRenderState must reject an unknown storage cluster")
	} else if !strings.Contains(err.Error(), "no-such") {
		t.Fatalf("error must name the unknown cluster; got %v", err)
	}
}
