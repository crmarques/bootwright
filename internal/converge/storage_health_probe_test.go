package converge

import (
	"testing"

	"github.com/crmarques/bootwright/internal/converge/workflow"
)

func TestStorageHealthDegraded(t *testing.T) {
	cases := []struct {
		name     string
		mode     string
		expected int
		obs      StorageHealthObservation
		want     bool
	}{
		{"exact healthy", "exact", 3, StorageHealthObservation{Probed: true, NumInOSDs: 3, Health: "HEALTH_OK"}, false},
		{"exact short", "exact", 3, StorageHealthObservation{Probed: true, NumInOSDs: 2, Health: "HEALTH_WARN"}, true},
		{"health err always degraded", "exact", 3, StorageHealthObservation{Probed: true, NumInOSDs: 3, Health: "HEALTH_ERR"}, true},
		{"health warn not degraded", "exact", 3, StorageHealthObservation{Probed: true, NumInOSDs: 3, Health: "HEALTH_WARN"}, false},
		{"atLeastOne with some", "atLeastOne", 0, StorageHealthObservation{Probed: true, NumInOSDs: 1, Health: "HEALTH_OK"}, false},
		{"atLeastOne with none", "atLeastOne", 0, StorageHealthObservation{Probed: true, NumInOSDs: 0, Health: "HEALTH_WARN"}, true},
		{"skip never degraded", "skip", 0, StorageHealthObservation{Probed: true, NumInOSDs: 0, Health: "HEALTH_WARN"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, _ := storageHealthDegraded(tc.mode, tc.expected, tc.obs)
			if got != tc.want {
				t.Fatalf("storageHealthDegraded = %v, want %v", got, tc.want)
			}
		})
	}
}

func storageRootReport() workflow.StateCheckReport {
	return workflow.StateCheckReport{
		InSync: true,
		Roots:  []workflow.StateCheckRoot{{Kind: "StorageCluster", Name: "ceph", Total: 1, Matched: 1}},
	}
}

func TestOverlayStorageHealthProbeFlagsDegraded(t *testing.T) {
	report := storageRootReport()
	exp := map[string]StorageOSDExpectation{"ceph": {Mode: "exact", Count: 3}}
	obs := map[string]StorageHealthObservation{
		"ceph": {Cluster: "ceph", Probed: true, NumInOSDs: 2, NumUpOSDs: 2, Health: "HEALTH_WARN"},
	}
	OverlayStorageHealthProbe(&report, obs, exp)

	if report.InSync {
		t.Fatalf("degraded cluster must clear InSync")
	}
	root := report.Roots[0]
	if len(root.Resources) != 1 || root.Resources[0].Classification != workflow.ConvergeSafetyDrift {
		t.Fatalf("degraded cluster must gain a drift osd-health resource, got %+v", root.Resources)
	}
}

func TestOverlayStorageHealthProbeIgnoresHealthyAndUnprobed(t *testing.T) {
	exp := map[string]StorageOSDExpectation{"ceph": {Mode: "exact", Count: 1}}

	// Healthy: not flagged.
	report := storageRootReport()
	OverlayStorageHealthProbe(&report, map[string]StorageHealthObservation{
		"ceph": {Cluster: "ceph", Probed: true, NumInOSDs: 1, Health: "HEALTH_OK"},
	}, exp)
	if !report.InSync || len(report.Roots[0].Resources) != 0 {
		t.Fatalf("healthy cluster must not be flagged")
	}

	// Unreachable (probed=false): keep offline classification.
	report = storageRootReport()
	OverlayStorageHealthProbe(&report, map[string]StorageHealthObservation{
		"ceph": {Cluster: "ceph", Probed: false},
	}, exp)
	if !report.InSync || len(report.Roots[0].Resources) != 0 {
		t.Fatalf("unprobed cluster must keep its offline classification")
	}
}
