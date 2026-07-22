package workflow

import (
	"reflect"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func TestBootProvenContainerClusters(t *testing.T) {
	clustersDir := t.TempDir()
	records := []ClusterInstallRecord{
		{Cluster: "creating", Status: ClusterInstallStatusInstalling, Phase: ClusterInstallPhaseCreatingISO},
		{Cluster: "iso-only", Status: ClusterInstallStatusInstalling, Phase: ClusterInstallPhaseISOCreated},
		{Cluster: "booting", Status: ClusterInstallStatusFailed, Phase: ClusterInstallPhaseBooting},
		{Cluster: "booted", Status: ClusterInstallStatusInstalling, Phase: ClusterInstallPhaseNodesBooted},
		{Cluster: "waiting", Status: ClusterInstallStatusInstalling, Phase: ClusterInstallPhaseWaiting},
		{Cluster: "complete", Status: ClusterInstallStatusInstalling, Phase: ClusterInstallPhaseComplete},
		{Cluster: "installed", Status: ClusterInstallStatusInstalled, Phase: ClusterInstallPhaseComplete},
		{Cluster: "destroyed", Status: ClusterInstallStatusDestroyed, Phase: ClusterInstallPhaseComplete},
	}
	names := []string{"no-record"}
	for _, record := range records {
		if err := SaveClusterInstallRecord(clustersDir, record); err != nil {
			t.Fatalf("save record %s: %v", record.Cluster, err)
		}
		names = append(names, record.Cluster)
	}
	var tasks []ApplyTask
	for _, name := range names {
		tasks = append(tasks, ApplyTask{Entry: TaskLedgerEntry{Kind: ApplyTaskKindNodeBoot, Cluster: name}})
	}

	got := BootProvenContainerClusters(clustersDir, tasks)
	want := []string{"booted", "booting", "complete", "installed", "waiting"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("BootProvenContainerClusters = %v, want %v", got, want)
	}
}

func TestBareMetalFirstInstallClustersWarnsISOCreatedResume(t *testing.T) {
	clustersDir := t.TempDir()
	if err := SaveClusterInstallRecord(clustersDir, ClusterInstallRecord{Cluster: "resume-bm", Status: ClusterInstallStatusInstalling, Phase: ClusterInstallPhaseISOCreated}); err != nil {
		t.Fatalf("save record: %v", err)
	}
	tasks := []ApplyTask{
		{Entry: TaskLedgerEntry{Kind: ApplyTaskKindNodeBoot, Cluster: "resume-bm"}},
	}
	state := v1alpha1.State{
		InfraProviders: []v1alpha1.InfraProvider{
			{Metadata: v1alpha1.Metadata{Name: "bm"}, Spec: v1alpha1.InfraProviderSpec{Type: v1alpha1.ProvisionerBareMetal}},
		},
		Machines: []v1alpha1.Machine{
			{Metadata: v1alpha1.Metadata{Name: "bm-0"}, Spec: v1alpha1.MachineSpec{Substrate: v1alpha1.MachineSubstrate{ProviderRef: v1alpha1.LocalObjectReference{Name: "bm"}}}},
		},
		ContainerClusters: []v1alpha1.ContainerCluster{
			{Metadata: v1alpha1.Metadata{Name: "resume-bm"}, Spec: v1alpha1.ContainerClusterSpec{Nodes: []v1alpha1.OCPNodeSpec{{MachineRef: v1alpha1.LocalObjectReference{Name: "bm-0"}}}}},
		},
	}

	proven := BootProvenContainerClusters(clustersDir, tasks)
	if len(proven) != 0 {
		t.Fatalf("iso-created record must not prove a boot, got %v", proven)
	}
	got := BareMetalFirstInstallClusters(proven, tasks, state)
	want := []string{"resume-bm"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("BareMetalFirstInstallClusters = %v, want %v", got, want)
	}
}
