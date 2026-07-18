package workflow

import (
	"strings"
	"testing"
	"time"
)

func TestSubstrateReleaseRecordsRoundTrip(t *testing.T) {
	runsDir := t.TempDir()
	now := time.Unix(1700000000, 0)
	if released, err := ReleasedSubstrateClusters(runsDir); err != nil || len(released) != 0 {
		t.Fatalf("empty store must list nothing, got %v err=%v", released, err)
	}
	for _, name := range []string{"ocp-prd-01", "ceph-prd"} {
		if err := MarkSubstrateReleased(runsDir, name, now); err != nil {
			t.Fatalf("mark %s: %v", name, err)
		}
	}
	if err := MarkSubstrateReleased(runsDir, "ceph-prd", now.Add(time.Hour)); err != nil {
		t.Fatalf("re-mark must be idempotent: %v", err)
	}
	released, err := ReleasedSubstrateClusters(runsDir)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if got := strings.Join(released, ","); got != "ceph-prd,ocp-prd-01" {
		t.Fatalf("released = %q, want sorted unique names", got)
	}
	if err := ClearSubstrateRelease(runsDir, "ceph-prd"); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if err := ClearSubstrateRelease(runsDir, "never-released"); err != nil {
		t.Fatalf("clearing an absent release must be a no-op, got %v", err)
	}
	released, err = ReleasedSubstrateClusters(runsDir)
	if err != nil || strings.Join(released, ",") != "ocp-prd-01" {
		t.Fatalf("released after clear = %v err=%v", released, err)
	}
}

func TestConsumableSubstrateReleasesIntersectPlannedMachineWork(t *testing.T) {
	runsDir := t.TempDir()
	now := time.Unix(1700000000, 0)
	for _, name := range []string{"ceph-prd", "ocp-prd-01", "hub-prd-01"} {
		if err := MarkSubstrateReleased(runsDir, name, now); err != nil {
			t.Fatalf("mark %s: %v", name, err)
		}
	}
	tasks := []ApplyTask{
		{Entry: TaskLedgerEntry{ID: "osinstall.ceph-prd", Kind: ApplyTaskKindManagedMachineOS, Cluster: "ceph-prd"}},
		{Entry: TaskLedgerEntry{ID: "machines.ocp-prd-01", Kind: ApplyTaskKindMachineInfraPrepare, Cluster: "ocp-prd-01"}},
		{Entry: TaskLedgerEntry{ID: "addons.hub-prd-01", Kind: ApplyTaskKindClusterAddon, Cluster: "hub-prd-01"}},
	}
	got, err := ConsumableSubstrateReleases(runsDir, tasks)
	if err != nil {
		t.Fatalf("consumable: %v", err)
	}
	if strings.Join(got, ",") != "ceph-prd,ocp-prd-01" {
		t.Fatalf("consumable = %v, want only released clusters with planned machine-substrate work", got)
	}
}

func TestSubstrateReleaseClearKindCoversMachineOSAndClusterBoot(t *testing.T) {
	for kind, want := range map[string]bool{
		ApplyTaskKindManagedMachineOS:    true,
		ApplyTaskKindNodeBoot:            true,
		ApplyTaskKindInstallWait:         true,
		ApplyTaskKindMachineInfraPrepare: false,
		ApplyTaskKindClusterAddon:        false,
	} {
		if got := SubstrateReleaseClearKind(kind); got != want {
			t.Fatalf("SubstrateReleaseClearKind(%s) = %v, want %v", kind, got, want)
		}
	}
}

func TestUnionClusterNamesDeduplicatesAndSorts(t *testing.T) {
	got := UnionClusterNames([]string{"b", "a"}, nil, []string{"a", "", "c"})
	if strings.Join(got, ",") != "a,b,c" {
		t.Fatalf("union = %v", got)
	}
	if UnionClusterNames(nil, nil) != nil {
		t.Fatalf("union of empty lists must be nil")
	}
}
