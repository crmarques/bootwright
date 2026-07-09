package workflow

import (
	"reflect"
	"testing"
)

func TestBareMetalFirstInstallClusters(t *testing.T) {
	objects := []ObjectClassification{
		{Kind: ObjectKindContainerCluster, Cluster: "fresh-bm", counts: map[ConvergeSafetyClassification]int{}},
		{Kind: ObjectKindContainerCluster, Cluster: "owned-bm", counts: map[ConvergeSafetyClassification]int{ConvergeSafetyMatch: 1}},
		{Kind: ObjectKindContainerCluster, Cluster: "fresh-vm", counts: map[ConvergeSafetyClassification]int{}},
	}
	tasks := []ApplyTask{
		{Entry: TaskLedgerEntry{Kind: ApplyTaskKindNodeBoot, Cluster: "fresh-bm"}},
		{Entry: TaskLedgerEntry{Kind: ApplyTaskKindNodeBoot, Cluster: "fresh-bm"}},
		{Entry: TaskLedgerEntry{Kind: ApplyTaskKindNodeBoot, Cluster: "owned-bm"}},
		{Entry: TaskLedgerEntry{Kind: ApplyTaskKindClusterInstall, Cluster: "fresh-vm"}},
	}

	got := BareMetalFirstInstallClusters(objects, tasks)
	want := []string{"fresh-bm"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("BareMetalFirstInstallClusters = %v, want %v", got, want)
	}
}
