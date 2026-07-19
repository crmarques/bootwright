package converge

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/converge/workflow"
)

func kubeVirtTenantState() v1alpha1.State {
	return v1alpha1.State{
		ContainerClusters: []v1alpha1.ContainerCluster{
			{Metadata: v1alpha1.Metadata{Name: "hub"}},
			{
				Metadata: v1alpha1.Metadata{Name: "nested"},
				Spec: v1alpha1.ContainerClusterSpec{
					Hosts: []v1alpha1.OCPHostSpec{{Role: "master", MachineRef: v1alpha1.LocalObjectReference{Name: "nested-m0"}}},
				},
			},
		},
		Machines: []v1alpha1.Machine{
			{Metadata: v1alpha1.Metadata{Name: "nested-m0"}, Spec: v1alpha1.MachineSpec{Substrate: v1alpha1.MachineSubstrate{ProviderRef: v1alpha1.LocalObjectReference{Name: "kv"}}}},
		},
		InfraProviders: []v1alpha1.InfraProvider{
			{
				Metadata: v1alpha1.Metadata{Name: "kv"},
				Spec: v1alpha1.InfraProviderSpec{
					Type:     v1alpha1.ProvisionerKubeVirt,
					KubeVirt: &v1alpha1.InfraProviderKubeVirt{HostClusterRef: &v1alpha1.LocalObjectReference{Name: "hub"}},
				},
			},
		},
	}
}

func seedInstalledTenant(t *testing.T, clustersDir, cluster string) {
	t.Helper()
	if err := workflow.SaveClusterInstallRecord(clustersDir, workflow.ClusterInstallRecord{
		Cluster: cluster, Status: workflow.ClusterInstallStatusInstalled, Phase: workflow.ClusterInstallPhaseComplete,
	}); err != nil {
		t.Fatalf("SaveClusterInstallRecord: %v", err)
	}
}

func TestKubeVirtTenantsByHost(t *testing.T) {
	byHost := KubeVirtTenantsByHost(kubeVirtTenantState())
	if got := byHost["hub"]; len(got) != 1 || got[0] != "nested" {
		t.Fatalf("tenantsByHost[hub] = %v, want [nested]", got)
	}
}

func TestKubeVirtTenantDestroyConflicts(t *testing.T) {
	state := kubeVirtTenantState()
	clustersDir := filepath.Join(t.TempDir(), "clusters")

	if conflicts := KubeVirtTenantDestroyConflicts(state, clustersDir, []string{"hub"}); len(conflicts) != 0 {
		t.Fatalf("no install record for the tenant must yield no conflict, got %v", conflicts)
	}

	seedInstalledTenant(t, clustersDir, "nested")
	conflicts := KubeVirtTenantDestroyConflicts(state, clustersDir, []string{"hub"})
	if len(conflicts) != 1 || conflicts[0].Host != "hub" || len(conflicts[0].Tenants) != 1 || conflicts[0].Tenants[0] != "nested" {
		t.Fatalf("installed tenant of a destroyed host must conflict, got %v", conflicts)
	}

	if conflicts := KubeVirtTenantDestroyConflicts(state, clustersDir, []string{"hub", "nested"}); len(conflicts) != 0 {
		t.Fatalf("selecting the tenant too must clear the conflict, got %v", conflicts)
	}

	err := FormatKubeVirtTenantConflicts(conflicts)
	_ = err
	msg := FormatKubeVirtTenantConflicts([]KubeVirtTenantConflict{{Host: "hub", Tenants: []string{"nested"}}}).Error()
	for _, want := range []string{"hub", "nested", "--clusters", "--override"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("conflict message must mention %q, got %q", want, msg)
		}
	}
}

func TestKubeVirtTenantDestroyDescriptors(t *testing.T) {
	state := kubeVirtTenantState()
	clustersDir := filepath.Join(t.TempDir(), "clusters")

	if got := KubeVirtTenantDestroyDescriptors(state, clustersDir, []string{"hub"}); len(got) != 0 {
		t.Fatalf("no installed tenant must yield no descriptor, got %v", got)
	}

	seedInstalledTenant(t, clustersDir, "nested")
	got := KubeVirtTenantDestroyDescriptors(state, clustersDir, []string{"hub"})
	if len(got) != 1 || !strings.Contains(got[0], "nested") || !strings.Contains(got[0], "hub") || !strings.Contains(got[0], "KubeVirt") {
		t.Fatalf("descriptor must name the destroyed tenant and its host, got %v", got)
	}
}
