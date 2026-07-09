package clusteraccess

import (
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func selectionTestState() v1alpha1.State {
	return v1alpha1.State{
		ContainerClusters: []v1alpha1.ContainerCluster{
			{Metadata: v1alpha1.Metadata{Name: "ocp"}},
		},
		StorageClusters: []v1alpha1.StorageCluster{
			{Metadata: v1alpha1.Metadata{Name: "ceph"}},
		},
	}
}

func TestResolveEmptyScopeIsWholeTarget(t *testing.T) {
	state := selectionTestState()
	sel, err := Resolve(state, "clusters", "")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if sel.Active {
		t.Fatal("empty scope must be inactive")
	}
	if sel.StorageWorkNames() != nil {
		t.Fatalf("empty scope StorageWorkNames must be nil (tear down/provision all); got %v", sel.StorageWorkNames())
	}
	if len(sel.RenderState.ContainerClusters) != 1 || len(sel.RenderState.StorageClusters) != 1 {
		t.Fatalf("empty scope must keep the full render state; got %+v", sel.RenderState)
	}
}

func TestResolveContainerOnlyExcludesStorageFromWorkSet(t *testing.T) {
	sel, err := Resolve(selectionTestState(), "clusters", "ocp")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !sel.Active {
		t.Fatal("a --clusters selection must be active")
	}
	if len(sel.ContainerRoots) != 1 || sel.ContainerRoots[0] != "ocp" {
		t.Fatalf("ContainerRoots = %v, want [ocp]", sel.ContainerRoots)
	}
	names := sel.StorageWorkNames()
	if names == nil || len(names) != 0 {
		t.Fatalf("container-only selection must yield a non-nil empty storage work set; got %v", names)
	}
	if len(sel.AllRoots) != 1 || sel.AllRoots[0] != "ocp" {
		t.Fatalf("AllRoots = %v, want [ocp]", sel.AllRoots)
	}
}

func TestResolveStorageRootIsWorkObject(t *testing.T) {
	sel, err := Resolve(selectionTestState(), "clusters", "ceph")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if names := sel.StorageWorkNames(); len(names) != 1 || names[0] != "ceph" {
		t.Fatalf("StorageWorkNames = %v, want [ceph]", names)
	}
}

func TestResolveUnknownClusterErrors(t *testing.T) {
	if _, err := Resolve(selectionTestState(), "clusters", "nope"); err == nil {
		t.Fatal("expected an error for an unknown cluster name")
	}
}

func TestResolveRejectsClusterScopeForUnsupportedTarget(t *testing.T) {
	if _, err := Resolve(selectionTestState(), "bastion", "ocp"); err == nil {
		t.Fatal("expected --clusters to be rejected for an unsupported target")
	}
}
