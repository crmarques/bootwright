package converge

import (
	"testing"

	"github.com/crmarques/bootwright/internal/converge/workflow"
)

func TestArtifactServerRecordName(t *testing.T) {
	if got := ArtifactServerRecordName("art"); got != "InfraComponent-art" {
		t.Fatalf("record name = %q, want InfraComponent-art", got)
	}
}

func TestArtifactServerProvisionSkipRecords(t *testing.T) {
	dir := t.TempDir()
	saveInstallRecordForTest(t, dir, "installed", workflow.ClusterInstallStatusInstalled)
	saveInstallRecordForTest(t, dir, "installing", workflow.ClusterInstallStatusInstalling)
	targets := []ArtifactServerReclaimTarget{
		{RecordName: "InfraComponent-settled", RefClusters: []string{"installed"}},
		{RecordName: "InfraComponent-busy", RefClusters: []string{"installed", "installing"}},
		{RecordName: "InfraComponent-noconsumer", RefClusters: nil},
	}
	skip, err := ArtifactServerProvisionSkipRecords(targets, dir, workflow.ApplyModeReconcile)
	if err != nil {
		t.Fatal(err)
	}
	if len(skip) != 1 || skip[0] != "InfraComponent-settled" {
		t.Fatalf("continue skip = %v; want [InfraComponent-settled]", skip)
	}
	overrideSkip, err := ArtifactServerProvisionSkipRecords(targets, dir, workflow.ApplyModeRebuild)
	if err != nil {
		t.Fatal(err)
	}
	if len(overrideSkip) != 0 {
		t.Fatalf("override skip = %v; want none (rebuild must re-provision)", overrideSkip)
	}
}

func TestSelectArtifactServerReclaims(t *testing.T) {
	dir := t.TempDir()
	saveInstallRecordForTest(t, dir, "c1", workflow.ClusterInstallStatusInstalled)
	saveInstallRecordForTest(t, dir, "c2", workflow.ClusterInstallStatusInstalling)
	targets := []ArtifactServerReclaimTarget{
		{RecordName: "InfraComponent-ready", RefClusters: []string{"c1"}},
		{RecordName: "InfraComponent-busy", RefClusters: []string{"c1", "c2"}},
		{RecordName: "InfraComponent-gone", RefClusters: []string{"c1"}},
		{RecordName: "InfraComponent-shared", RefClusters: []string{"c1"}},
		{RecordName: "InfraComponent-reference", RefClusters: []string{"c1"}},
	}
	owned := map[string]bool{
		"InfraComponent-ready":  true,
		"InfraComponent-busy":   true,
		"InfraComponent-shared": true,
	}
	blocked := map[string]bool{"InfraComponent-shared": true}
	reclaim, err := selectArtifactServerReclaims(targets, owned, blocked, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(reclaim) != 1 || reclaim[0] != "InfraComponent-ready" {
		t.Fatalf("reclaim = %v; want [InfraComponent-ready] (busy still installing, gone has no record, shared cross-context blocked, reference not owned)", reclaim)
	}
}

func saveInstallRecordForTest(t *testing.T, clustersDir, cluster string, status workflow.ClusterInstallStatus) {
	t.Helper()
	if err := workflow.SaveClusterInstallRecord(clustersDir, workflow.ClusterInstallRecord{Cluster: cluster, Status: status}); err != nil {
		t.Fatal(err)
	}
}

func TestAllReferencingClustersInstalled(t *testing.T) {
	dir := t.TempDir()
	if ok, err := AllReferencingClustersInstalled(dir, nil); err != nil || ok {
		t.Fatalf("empty set: ok=%v err=%v; want false,nil", ok, err)
	}
	saveInstallRecordForTest(t, dir, "a", workflow.ClusterInstallStatusInstalled)
	saveInstallRecordForTest(t, dir, "b", workflow.ClusterInstallStatusInstalling)
	if ok, err := AllReferencingClustersInstalled(dir, []string{"a", "b"}); err != nil || ok {
		t.Fatalf("one still installing: ok=%v err=%v; want false,nil", ok, err)
	}
	if ok, err := AllReferencingClustersInstalled(dir, []string{"a", "missing"}); err != nil || ok {
		t.Fatalf("one missing record: ok=%v err=%v; want false,nil", ok, err)
	}
	saveInstallRecordForTest(t, dir, "b", workflow.ClusterInstallStatusInstalled)
	if ok, err := AllReferencingClustersInstalled(dir, []string{"a", "b"}); err != nil || !ok {
		t.Fatalf("all installed: ok=%v err=%v; want true,nil", ok, err)
	}
}
