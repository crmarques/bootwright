package cli

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	extensionrecords "github.com/crmarques/bootwright/internal/addons/records"
	"github.com/crmarques/bootwright/internal/workspace"
)

func TestStatusSurfacesAddonCSVObservationsInHumanAndJSON(t *testing.T) {
	initTestContextWithClusterAddon(t)
	ctx, err := workspace.ResolveExistingContext("test")
	if err != nil {
		t.Fatal(err)
	}
	observedAt := time.Date(2026, 8, 10, 12, 30, 0, 0, time.UTC)
	if err := extensionrecords.SaveRecord(ctx.ClustersDir, extensionrecords.Record{
		Cluster: "sno-libvirt", Extension: "openshift-virtualization",
		Status: extensionrecords.RecordStatusReady, Phase: extensionrecords.RecordPhaseComplete,
		UpdatedAt: observedAt,
		CSVObservations: []extensionrecords.CSVObservation{{
			Namespace: "openshift-cnv", Subscription: "hco-operatorhub",
			InstalledCSV: "kubevirt-hyperconverged-operator.v4.21.2", Version: "4.21.2", ObservedAt: observedAt,
		}},
	}); err != nil {
		t.Fatalf("SaveRecord: %v", err)
	}

	stdout, stderr, code := runCLI(t, "status")
	if code != 0 {
		t.Fatalf("status exited %d, stderr=%q", code, stderr)
	}
	for _, want := range []string{"CSV openshift-cnv/hco-operatorhub", "kubevirt-hyperconverged-operator.v4.21.2", "version=4.21.2", observedAt.Format(time.RFC3339)} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("status output missing %q:\n%s", want, stdout)
		}
	}

	stdout, stderr, code = runCLI(t, "status", "--output", "json")
	if code != 0 {
		t.Fatalf("status JSON exited %d, stderr=%q", code, stderr)
	}
	var report statusReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("decode status JSON: %v\n%s", err, stdout)
	}
	if len(report.Clusters) != 1 || len(report.Clusters[0].Addons) == 0 {
		t.Fatalf("status clusters = %+v", report.Clusters)
	}
	var found bool
	for _, addon := range report.Clusters[0].Addons {
		if addon.Name != "openshift-virtualization" {
			continue
		}
		found = len(addon.CSVObservations) == 1 && addon.CSVObservations[0].Version == "4.21.2"
	}
	if !found {
		t.Fatalf("status JSON omitted CSV observations: %+v", report.Clusters[0].Addons)
	}
}
