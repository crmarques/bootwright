package status

import (
	"testing"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
	extensionrecords "github.com/crmarques/bootwright/internal/addons/records"
)

func TestBuildAddonsSurfacesLastObservedForFailedRecord(t *testing.T) {
	dir := t.TempDir()
	state := v1alpha1.State{
		ContainerClusters: []v1alpha1.ContainerCluster{{Metadata: v1alpha1.Metadata{Name: "demo"}}},
		ClusterAddons: []v1alpha1.ClusterAddon{{
			Metadata: v1alpha1.Metadata{Name: "odf"},
			Spec:     v1alpha1.ClusterAddonSpec{Type: v1alpha1.ClusterAddonTypeManifestSet, ManifestSet: &v1alpha1.ClusterAddonManifestSet{}},
		}},
		ClusterAddonBindings: []v1alpha1.ClusterAddonBinding{{
			Metadata: v1alpha1.Metadata{Name: "odf-binding"},
			Spec: v1alpha1.ClusterAddonBindingSpec{
				ClusterRef: v1alpha1.LocalObjectReference{Name: "demo"},
				AddonRefs:  []v1alpha1.LocalObjectReference{{Name: "odf"}},
			},
		}},
	}
	record := extensionrecords.Record{
		Cluster:      "demo",
		Extension:    "odf",
		Status:       extensionrecords.RecordStatusFailed,
		Phase:        extensionrecords.RecordPhaseApplying,
		UpdatedAt:    time.Date(2026, 7, 23, 0, 0, 0, 0, time.UTC),
		LastObserved: `hook "attach-external-storage" (postOperatorReady) failed: boom`,
	}
	if err := extensionrecords.SaveRecord(dir, record); err != nil {
		t.Fatalf("SaveRecord: %v", err)
	}

	addons := BuildAddons(state, dir)
	got := addons["demo"]
	if len(got) != 1 {
		t.Fatalf("BuildAddons[demo] = %#v, want exactly one entry", got)
	}
	if got[0].Status != string(extensionrecords.RecordStatusFailed) {
		t.Fatalf("Extension.Status = %q, want %q", got[0].Status, extensionrecords.RecordStatusFailed)
	}
	if got[0].LastObserved != record.LastObserved {
		t.Fatalf("Extension.LastObserved = %q, want %q", got[0].LastObserved, record.LastObserved)
	}
}
