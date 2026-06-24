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

// TestResolveEmptyScopeIsWholeTarget pins that an absent --clusters selection
// yields an inactive selection with the full render state and no work-set
// narrowing — the unscoped run path every handler relies on.
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
	if !sel.IsStorageWorkObject("ceph") {
		t.Fatal("with no narrowing every storage cluster is a work object")
	}
}

// TestResolveContainerOnlyExcludesStorageFromWorkSet is the selection-level
// expression of the destroy bug fix and the apply/destroy symmetry: a
// container-only --clusters selection resolves to an empty (non-nil) storage
// work set, so neither apply provisioning nor destroy teardown touches storage,
// while the directly-named container root is the selection's container root.
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
	if sel.IsStorageWorkObject("ceph") {
		t.Fatal("a storage cluster not named in --clusters must not be a work object")
	}
	if len(sel.AllRoots) != 1 || sel.AllRoots[0] != "ocp" {
		t.Fatalf("AllRoots = %v, want [ocp]", sel.AllRoots)
	}
}

// TestResolveStorageRootIsWorkObject confirms a directly-named storage root is a
// work object and the single source of StorageWorkNames.
func TestResolveStorageRootIsWorkObject(t *testing.T) {
	sel, err := Resolve(selectionTestState(), "clusters", "ceph")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if names := sel.StorageWorkNames(); len(names) != 1 || names[0] != "ceph" {
		t.Fatalf("StorageWorkNames = %v, want [ceph]", names)
	}
	if !sel.IsStorageWorkObject("ceph") {
		t.Fatal("a directly-named storage root must be a work object")
	}
}

// TestResolveUnknownClusterErrors keeps Resolve a drop-in for the resolvers it
// replaces: an unknown --clusters name is rejected, not silently dropped.
func TestResolveUnknownClusterErrors(t *testing.T) {
	if _, err := Resolve(selectionTestState(), "clusters", "nope"); err == nil {
		t.Fatal("expected an error for an unknown cluster name")
	}
}

// TestResolveRejectsClusterScopeForUnsupportedTarget mirrors ScopeState's
// unsupported-target guard so the selector and the filter agree.
func TestResolveRejectsClusterScopeForUnsupportedTarget(t *testing.T) {
	if _, err := Resolve(selectionTestState(), "bastion", "ocp"); err == nil {
		t.Fatal("expected --clusters to be rejected for an unsupported target")
	}
}
