package workflow

import (
	"reflect"
	"testing"
)

// BareMetalFirstInstallClusters names only clusters that have a nodeBoot (Redfish
// bare-metal) task AND carry no convergence-safety record — the first-apply,
// physical-disk-wipe case the CLI must warn about. A recorded (owned) bare-metal
// cluster is excluded (its install-state healthy-skip guards it), and a cluster
// with no nodeBoot task (KubeVirt/vSphere VM boot, or a non-boot phase) is excluded
// because no physical host is wiped.
func TestBareMetalFirstInstallClusters(t *testing.T) {
	objects := []ObjectClassification{
		// fresh bare-metal cluster: no record → first install
		{Kind: ObjectKindContainerCluster, Cluster: "fresh-bm", counts: map[ConvergeSafetyClassification]int{}},
		// owned bare-metal cluster: recorded (match) → excluded
		{Kind: ObjectKindContainerCluster, Cluster: "owned-bm", counts: map[ConvergeSafetyClassification]int{ConvergeSafetyMatch: 1}},
		// fresh cluster booted as a VM (no nodeBoot task) → excluded
		{Kind: ObjectKindContainerCluster, Cluster: "fresh-vm", counts: map[ConvergeSafetyClassification]int{}},
	}
	tasks := []ApplyTask{
		{Entry: TaskLedgerEntry{Kind: ApplyTaskKindNodeBoot, Cluster: "fresh-bm"}},
		{Entry: TaskLedgerEntry{Kind: ApplyTaskKindNodeBoot, Cluster: "fresh-bm"}}, // dup node → deduped
		{Entry: TaskLedgerEntry{Kind: ApplyTaskKindNodeBoot, Cluster: "owned-bm"}},
		{Entry: TaskLedgerEntry{Kind: ApplyTaskKindClusterInstall, Cluster: "fresh-vm"}}, // no nodeBoot
	}

	got := BareMetalFirstInstallClusters(objects, tasks)
	want := []string{"fresh-bm"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("BareMetalFirstInstallClusters = %v, want %v", got, want)
	}
}
