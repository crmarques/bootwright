package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
	extensionrecords "github.com/crmarques/bootwright/internal/addons/records"
	"github.com/crmarques/bootwright/internal/converge/workflow"
)

func TestRecordedDiffSurfacesAddonCSVAdvisoryInHumanAndJSON(t *testing.T) {
	observedAt := time.Date(2026, 8, 10, 15, 0, 0, 0, time.UTC)
	report := workflow.StateCheckReport{
		InSync: true,
		AddonCSVs: []workflow.AddonCSVReport{{
			Cluster: "dc1-ocp", Addon: "fusion-data-foundation",
			Namespace: "openshift-storage", Subscription: "odf-operator",
			Recorded: &extensionrecords.CSVObservation{
				Namespace: "openshift-storage", Subscription: "odf-operator",
				InstalledCSV: "odf-operator.v4.21.4", Version: "4.21.4", ObservedAt: observedAt,
			},
		}},
	}
	var human bytes.Buffer
	printStateCheckReport(&human, report)
	for _, want := range []string{"Add-on CSV observations", "dc1-ocp/fusion-data-foundation CSV openshift-storage/odf-operator", "odf-operator.v4.21.4", "version=4.21.4", observedAt.Format(time.RFC3339)} {
		if !strings.Contains(human.String(), want) {
			t.Fatalf("recorded diff missing %q:\n%s", want, human.String())
		}
	}
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"inSync":true`, `"addonCSVs"`, `"installedCSV":"odf-operator.v4.21.4"`, `"version":"4.21.4"`} {
		if !bytes.Contains(data, []byte(want)) {
			t.Fatalf("recorded diff JSON missing %s: %s", want, data)
		}
	}
}

func TestLiveDiffCSVChangeIsAdvisoryInHumanAndJSON(t *testing.T) {
	observedAt := time.Date(2026, 8, 10, 15, 0, 0, 0, time.UTC)
	comparisons := []liveAddonCSVComparison{{
		Cluster: "dc1-ocp", Addon: "fusion-data-foundation",
		Namespace: "openshift-storage", Subscription: "odf-operator",
		Recorded: &extensionrecords.CSVObservation{
			Namespace: "openshift-storage", Subscription: "odf-operator",
			InstalledCSV: "odf-operator.v4.21.3", Version: "4.21.3", ObservedAt: observedAt,
		},
	}}
	matchLiveCSVObservations(comparisons, []extensionrecords.CSVObservation{{
		Namespace: "openshift-storage", Subscription: "odf-operator",
		InstalledCSV: "odf-operator.v4.21.4", Version: "4.21.4", ObservedAt: observedAt.Add(time.Hour),
	}}, "")
	live := liveDiffReport{InSync: true, AddonCSVs: comparisons}
	if comparisons[0].State != "changed" || !live.InSync {
		t.Fatalf("live CSV comparison changed sync verdict: %+v", live)
	}
	var human bytes.Buffer
	printLiveDiff(&human, live)
	for _, want := range []string{"desired state matches the live clusters", "Add-on CSV observations", "recorded odf-operator.v4.21.3", "live odf-operator.v4.21.4"} {
		if !strings.Contains(human.String(), want) {
			t.Fatalf("live diff missing %q:\n%s", want, human.String())
		}
	}
	data, err := json.Marshal(live)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"inSync":true`, `"addonCSVs"`, `"state":"changed"`, `"live":{"namespace":"openshift-storage"`} {
		if !bytes.Contains(data, []byte(want)) {
			t.Fatalf("live diff JSON missing %s: %s", want, data)
		}
	}
}

func TestLiveDiffCSVUnavailableStaysAdvisory(t *testing.T) {
	comparisons := baseAddonCSVComparisons([]workflow.AddonCSVReport{{
		Cluster: "dc1-ocp", Addon: "odf", Namespace: "openshift-storage", Subscription: "odf-operator",
	}})
	markAddonCSVUnavailable(comparisons, "cluster access unavailable")
	live := liveDiffReport{InSync: true, AddonCSVs: comparisons}
	if comparisons[0].State != "unavailable" || comparisons[0].Note != "cluster access unavailable" || !live.InSync {
		t.Fatalf("unavailable CSV advisory = %+v", live)
	}
}

func TestLiveDiffCSVProbeUsesOnlySelectedAdvisoryIdentities(t *testing.T) {
	selected := []workflow.AddonCSVReport{{
		Cluster: "selected", Addon: "odf", Namespace: "openshift-storage", Subscription: "odf-operator",
	}}
	comparisons := probeLiveAddonCSVs(t.Context(), v1alpha1.State{}, "ctx", t.TempDir(), t.TempDir(), selected)
	if len(comparisons) != 1 || comparisons[0].Cluster != "selected" || comparisons[0].State != "unavailable" || !strings.Contains(comparisons[0].Note, "cluster access unavailable") {
		t.Fatalf("selected live CSV probes = %+v", comparisons)
	}
}

func TestLiveAddonCSVSelectionOmitsAbsentContainerRoots(t *testing.T) {
	selected := []workflow.AddonCSVReport{
		{Cluster: "fresh", Addon: "odf", Namespace: "openshift-storage", Subscription: "odf-operator"},
		{Cluster: "ready", Addon: "odf", Namespace: "openshift-storage", Subscription: "odf-operator"},
	}
	roots := []workflow.StateCheckRoot{
		{Kind: workflow.ApplyClusterKindContainer, Name: "fresh", Absent: true},
		{Kind: workflow.ApplyClusterKindContainer, Name: "ready"},
	}

	got := liveAddonCSVSelection(selected, roots)
	if len(got) != 1 || got[0].Cluster != "ready" {
		t.Fatalf("live add-on CSV selection = %+v", got)
	}
}
