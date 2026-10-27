//go:build linux && amd64

package main

import (
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func TestObservabilityAndHostProfilesProductionPlans(t *testing.T) {
	for _, name := range []string{"libvirt-4-nodes-ceph-observability", "libvirt-3-nodes-ceph-host-variants"} {
		t.Run(name, func(t *testing.T) {
			state, report := compileAcceptance(t, exampleDirectory(t, name))
			wantCount := 28
			if name == "libvirt-4-nodes-ceph-observability" {
				wantCount = 32
			}
			if report.Counts.FilesSeen != wantCount || report.Counts.ObjectsDecoded != wantCount {
				t.Fatalf("counts: %+v, want %d files and objects", report.Counts, wantCount)
			}
			if refused := productionUnsupported(t, state); len(refused) != 0 {
				t.Fatalf("refusals: %+v", refused)
			}
			plan := planIngressFixture(t, state, name)
			if len(plan.Blocks) == 0 {
				t.Fatal("empty production plan")
			}
			cluster := requireObject(t, state.Effective(), api.CephCluster, "ceph-lab")
			if cluster.Spec().Get("config", "osd", "osd_memory_target").Text() != "4294967296" {
				t.Fatal("missing 4 GiB OSD budget")
			}
		})
	}
}
