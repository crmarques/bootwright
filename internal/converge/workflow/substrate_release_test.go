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
	if strings.Join(SubstrateReleaseClusterNames(got), ",") != "ceph-prd,ocp-prd-01" {
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

func TestMarkSubstrateMachinesReleasedMergesAndYieldsToWholeCluster(t *testing.T) {
	runsDir := t.TempDir()
	now := time.Unix(1700000000, 0)
	if err := MarkSubstrateMachinesReleased(runsDir, "ceph-prd", []string{"m02"}, now); err != nil {
		t.Fatalf("mark m02: %v", err)
	}
	if err := MarkSubstrateMachinesReleased(runsDir, "ceph-prd", []string{"m01"}, now); err != nil {
		t.Fatalf("mark m01: %v", err)
	}
	record, found, err := loadSubstrateRelease(runsDir, "ceph-prd")
	if err != nil || !found {
		t.Fatalf("load: found=%v err=%v", found, err)
	}
	if got := strings.Join(record.Machines, ","); got != "m01,m02" {
		t.Fatalf("machines = %q, want merged sorted list", got)
	}
	if err := MarkSubstrateReleased(runsDir, "ceph-prd", now); err != nil {
		t.Fatalf("mark whole cluster: %v", err)
	}
	if err := MarkSubstrateMachinesReleased(runsDir, "ceph-prd", []string{"m03"}, now); err != nil {
		t.Fatalf("mark after whole-cluster: %v", err)
	}
	record, _, err = loadSubstrateRelease(runsDir, "ceph-prd")
	if err != nil || len(record.Machines) != 0 {
		t.Fatalf("whole-cluster release must stay whole-cluster, got machines=%v err=%v", record.Machines, err)
	}
}

func TestConsumeSubstrateReleaseCoverageSemantics(t *testing.T) {
	runsDir := t.TempDir()
	now := time.Unix(1700000000, 0)
	clusterMachines := []string{"m01", "m02", "m03"}
	if err := MarkSubstrateReleased(runsDir, "ceph-prd", now); err != nil {
		t.Fatalf("mark: %v", err)
	}
	if err := ConsumeSubstrateRelease(runsDir, "ceph-prd", []string{"m01"}, clusterMachines); err != nil {
		t.Fatalf("consume m01: %v", err)
	}
	record, found, err := loadSubstrateRelease(runsDir, "ceph-prd")
	if err != nil || !found {
		t.Fatalf("partial consume must keep the record, found=%v err=%v", found, err)
	}
	if got := strings.Join(record.Machines, ","); got != "m02,m03" {
		t.Fatalf("remaining machines = %q, want uncovered machines", got)
	}
	if err := ConsumeSubstrateRelease(runsDir, "ceph-prd", []string{"m02", "m03"}, clusterMachines); err != nil {
		t.Fatalf("consume rest: %v", err)
	}
	if _, found, _ := loadSubstrateRelease(runsDir, "ceph-prd"); found {
		t.Fatalf("full coverage must remove the record")
	}
	if err := MarkSubstrateMachinesReleased(runsDir, "ceph-prd", []string{"m02"}, now); err != nil {
		t.Fatalf("re-mark m02: %v", err)
	}
	if err := ConsumeSubstrateRelease(runsDir, "ceph-prd", nil, clusterMachines); err != nil {
		t.Fatalf("unscoped consume: %v", err)
	}
	if _, found, _ := loadSubstrateRelease(runsDir, "ceph-prd"); found {
		t.Fatalf("unscoped run must clear the record entirely")
	}
	if err := ConsumeSubstrateRelease(runsDir, "never-released", []string{"m01"}, clusterMachines); err != nil {
		t.Fatalf("consuming an absent release must be a no-op, got %v", err)
	}
}
