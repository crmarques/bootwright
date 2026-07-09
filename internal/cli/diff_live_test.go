package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/storage/cephdiff"
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
