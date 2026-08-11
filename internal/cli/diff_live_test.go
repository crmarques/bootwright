package cli

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/storage/cephdiff"
	"github.com/crmarques/bootwright/internal/storage/cephstate"
)

func TestPrintLiveDiffRendersGitStyle(t *testing.T) {
	var buf bytes.Buffer
	live := liveDiffReport{
		InSync: false,
		Storage: []liveStorageDiff{
			{
				Cluster: "ceph-prod",
				Probed:  true,
				InSync:  false,
				Report: cephdiff.Report{
					Cluster: "ceph-prod",
					Probed:  true,
					Facets: []cephdiff.FacetDiff{
						{Name: "pools", Objects: []cephdiff.ObjectDiff{
							{Key: "rbd", State: cephdiff.ObjectChanged, Fields: []cephdiff.FieldDiff{
								{Name: "size", Desired: "3", Real: "2", HasDesired: true, HasReal: true},
							}},
							{Key: "backups", State: cephdiff.ObjectDesiredOnly, Fields: []cephdiff.FieldDiff{
								{Name: "type", Desired: "replicated", HasDesired: true},
							}},
						}},
					},
				},
			},
			{Cluster: "ceph-ext", Note: "external (imported); not compared"},
		},
		Container: []liveContainerDiff{
			{Cluster: "dc1-ocp", Installed: true, Reachable: true, Available: true, Version: "4.16.3"},
		},
		Absent: []string{"StorageCluster/ceph-new"},
	}
	printLiveDiff(&buf, live)
	got := buf.String()

	for _, want := range []string{
		"StorageCluster/ceph-prod",
		"@@ pools rbd (changed) @@",
		"-size: 3",
		"+size: 2",
		"@@ pools backups (desired-only) @@",
		"-type: replicated",
		"ceph-ext",
		"external (imported)",
		"dc1-ocp",
		"Available",
		"4.16.3",
		"absent (never applied)",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("live diff output missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "+type: replicated") {
		t.Fatalf("desired-only field rendered as an addition:\n%s", got)
	}
}

func TestDiffStorageClusterUnreachableIsNotInSync(t *testing.T) {
	cluster := v1alpha1.StorageCluster{
		Metadata: v1alpha1.Metadata{Name: "ceph-prod"},
		Spec:     v1alpha1.StorageClusterSpec{Type: "ceph", Ceph: &v1alpha1.StorageClusterCephSpec{}},
	}
	state := v1alpha1.State{StorageClusters: []v1alpha1.StorageCluster{cluster}}
	live := liveDiffReport{InSync: true}
	result := diffStorageCluster(state, cluster, "ceph-prod", map[string]cephstate.Discovery{}, &live)
	if result.InSync || live.InSync {
		t.Fatalf("unreachable managed cluster must not report in sync: result=%+v live=%+v", result, live)
	}
	if !strings.Contains(result.Note, "unreachable") {
		t.Fatalf("unreachable note missing: %+v", result)
	}

	external := cluster
	external.Spec.Management = v1alpha1.StorageClusterManagementExternal
	live = liveDiffReport{InSync: true}
	result = diffStorageCluster(state, external, "ceph-prod", map[string]cephstate.Discovery{}, &live)
	if !result.InSync || !live.InSync {
		t.Fatalf("external cluster comparison must stay neutral: result=%+v live=%+v", result, live)
	}
}

func TestSelectedManagedStorageDiscoveryClustersDoesNotWidenAcrossTwoClusters(t *testing.T) {
	first := v1alpha1.StorageCluster{
		Metadata: v1alpha1.Metadata{Name: "ceph-a"},
		Spec:     v1alpha1.StorageClusterSpec{Type: v1alpha1.StorageClusterTypeCeph, Ceph: &v1alpha1.StorageClusterCephSpec{}},
	}
	second := first
	second.Metadata.Name = "ceph-b"
	byName := map[string]v1alpha1.StorageCluster{first.Metadata.Name: first, second.Metadata.Name: second}
	roots := []workflow.StateCheckRoot{{Kind: workflow.ApplyClusterKindStorage, Name: second.Metadata.Name}}

	if got := selectedManagedStorageDiscoveryClusters(byName, roots); !slices.Equal(got, []string{second.Metadata.Name}) {
		t.Fatalf("selected discovery clusters = %v, want only %q", got, second.Metadata.Name)
	}
}

func TestProbeContainerClustersKeepsRootIndexMapping(t *testing.T) {
	roots := []workflow.StateCheckRoot{
		{Kind: workflow.ApplyClusterKindStorage, Name: "ceph-prod"},
		{Kind: workflow.ApplyClusterKindContainer, Name: "dc1-ocp"},
		{Kind: workflow.ApplyClusterKindContainer, Name: "dc2-ocp", Absent: true},
		{Kind: workflow.ApplyClusterKindContainer, Name: "dc3-ocp"},
	}
	probed := probeContainerClusters(context.Background(), v1alpha1.State{}, "lab", t.TempDir(), t.TempDir(), roots)
	if len(probed) != 2 {
		t.Fatalf("probed = %v, want only the present container roots", probed)
	}
	for index, want := range map[int]string{1: "dc1-ocp", 3: "dc3-ocp"} {
		got, ok := probed[index]
		if !ok {
			t.Fatalf("root index %d missing from probe results: %v", index, probed)
		}
		if got.Cluster != want {
			t.Fatalf("probed[%d].Cluster = %q, want %q; concurrent probing must stay aligned with report order", index, got.Cluster, want)
		}
		if got.Installed || !strings.Contains(got.Note, "not installed") {
			t.Fatalf("probed[%d] = %+v, want a not-installed result without a kubeconfig", index, got)
		}
	}
	if _, ok := probed[0]; ok {
		t.Fatal("storage root must not be probed as a container cluster")
	}
	if _, ok := probed[2]; ok {
		t.Fatal("absent container root must not be probed")
	}
}

func TestPrintLiveDiffInSync(t *testing.T) {
	var buf bytes.Buffer
	printLiveDiff(&buf, liveDiffReport{
		InSync:  true,
		Storage: []liveStorageDiff{{Cluster: "ceph-prod", Probed: true, InSync: true}},
	})
	got := buf.String()
	if !strings.Contains(got, "in sync with the live cluster") {
		t.Fatalf("in-sync storage cluster not reported:\n%s", got)
	}
	if !strings.Contains(got, "desired state matches the live clusters") {
		t.Fatalf("in-sync summary missing:\n%s", got)
	}
}
