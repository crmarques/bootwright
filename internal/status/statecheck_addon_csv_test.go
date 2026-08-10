package status

import (
	"testing"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
	extensionplan "github.com/crmarques/bootwright/internal/addons/plan"
	extensionrecords "github.com/crmarques/bootwright/internal/addons/records"
	"github.com/crmarques/bootwright/internal/converge/workflow"
)

func TestAddonCSVReportsUseOnlySelectedAddonTasks(t *testing.T) {
	clustersDir := t.TempDir()
	observedAt := time.Date(2026, 8, 10, 14, 0, 0, 0, time.UTC)
	for _, cluster := range []string{"selected", "unselected"} {
		if err := extensionrecords.SaveRecord(clustersDir, extensionrecords.Record{
			Cluster: cluster, Extension: "odf", Status: extensionrecords.RecordStatusReady,
			CSVObservations: []extensionrecords.CSVObservation{{
				Namespace: "openshift-storage", Subscription: "odf-operator",
				InstalledCSV: "odf-operator.v4.21.1", Version: "4.21.1", ObservedAt: observedAt,
			}},
		}); err != nil {
			t.Fatalf("SaveRecord(%s): %v", cluster, err)
		}
	}
	extension := v1alpha1.ClusterAddon{
		Metadata: v1alpha1.Metadata{Name: "odf"},
		Spec: v1alpha1.ClusterAddonSpec{Readiness: v1alpha1.ClusterAddonReadiness{Checks: []v1alpha1.ClusterAddonReadinessCheck{{
			CSVSucceeded: &v1alpha1.ClusterAddonCSVReadiness{Namespace: "openshift-storage", Subscription: "odf-operator"},
		}}}},
	}
	task := workflow.ApplyTask{
		Entry:     workflow.TaskLedgerEntry{Kind: workflow.ApplyTaskKindClusterAddon, Cluster: "selected", Addon: "odf"},
		Extension: &extensionplan.ExtensionPlan{Name: "odf", Cluster: "selected", Extension: extension},
	}

	reports := addonCSVReports([]workflow.ApplyTask{task}, clustersDir, nil)
	if len(reports) != 1 || reports[0].Cluster != "selected" || reports[0].Recorded == nil || reports[0].Recorded.Version != "4.21.1" {
		t.Fatalf("selected add-on CSV reports = %+v", reports)
	}
}

func TestAddonCSVReportsKeepMissingLegacyEvidenceAdvisory(t *testing.T) {
	clustersDir := t.TempDir()
	extension := v1alpha1.ClusterAddon{
		Metadata: v1alpha1.Metadata{Name: "odf"},
		Spec: v1alpha1.ClusterAddonSpec{Readiness: v1alpha1.ClusterAddonReadiness{Checks: []v1alpha1.ClusterAddonReadinessCheck{{
			CSVSucceeded: &v1alpha1.ClusterAddonCSVReadiness{Namespace: "openshift-storage", Subscription: "odf-operator"},
		}}}},
	}
	task := workflow.ApplyTask{
		Entry:     workflow.TaskLedgerEntry{Kind: workflow.ApplyTaskKindClusterAddon, Cluster: "demo", Addon: "odf"},
		Extension: &extensionplan.ExtensionPlan{Name: "odf", Cluster: "demo", Extension: extension},
	}

	reports := addonCSVReports([]workflow.ApplyTask{task}, clustersDir, nil)
	if len(reports) != 1 || reports[0].Recorded != nil || reports[0].Note == "" {
		t.Fatalf("legacy record advisory = %+v", reports)
	}
}

func TestAddonCSVReportsOmitAbsentContainerRoots(t *testing.T) {
	extension := v1alpha1.ClusterAddon{
		Metadata: v1alpha1.Metadata{Name: "odf"},
		Spec: v1alpha1.ClusterAddonSpec{Readiness: v1alpha1.ClusterAddonReadiness{Checks: []v1alpha1.ClusterAddonReadinessCheck{{
			CSVSucceeded: &v1alpha1.ClusterAddonCSVReadiness{Namespace: "openshift-storage", Subscription: "odf-operator"},
		}}}},
	}
	task := workflow.ApplyTask{
		Entry:     workflow.TaskLedgerEntry{Kind: workflow.ApplyTaskKindClusterAddon, Cluster: "fresh", Addon: "odf"},
		Extension: &extensionplan.ExtensionPlan{Name: "odf", Cluster: "fresh", Extension: extension},
	}
	roots := []workflow.StateCheckRoot{{Kind: workflow.ApplyClusterKindContainer, Name: "fresh", Absent: true}}

	if reports := addonCSVReports([]workflow.ApplyTask{task}, t.TempDir(), roots); len(reports) != 0 {
		t.Fatalf("absent container add-on CSV reports = %+v", reports)
	}
}
