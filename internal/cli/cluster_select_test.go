package cli

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func TestValidateScopedApplySharedServicesFailsForInfraScope(t *testing.T) {
	state := cliStateWithSharedDNS()

	err := validateScopedApplySharedServices(state, "infra", "cluster-a")
	if err == nil {
		t.Fatal("expected shared service conflict, got nil")
	}
	if !strings.Contains(err.Error(), "--scope would narrow shared provider service") {
		t.Fatalf("error %q does not explain scoped apply conflict", err)
	}
	if !strings.Contains(err.Error(), "unscoped {cluster-b}") {
		t.Fatalf("error %q does not list unscoped consumer", err)
	}
}

func TestValidateScopedApplySharedServicesAllowsClusterScope(t *testing.T) {
	if err := validateScopedApplySharedServices(cliStateWithSharedDNS(), "cluster", "cluster-a"); err != nil {
		t.Fatalf("cluster apply scope should not validate provider services: %v", err)
	}
}

func TestValidateScopedApplySharedServicesAllowsAllConsumers(t *testing.T) {
	if err := validateScopedApplySharedServices(cliStateWithSharedDNS(), "infra", "cluster-a,cluster-b"); err != nil {
		t.Fatalf("infra apply with all consumers should validate: %v", err)
	}
}

func cliStateWithSharedDNS() v1alpha1.State {
	return v1alpha1.State{
		ClusterInfras: []v1alpha1.ClusterInfra{
			cliClusterInfraWithDNS("infra-a", "machines-a"),
			cliClusterInfraWithDNS("infra-b", "machines-b"),
		},
		ContainerClusters: []v1alpha1.ContainerCluster{
			cliContainerCluster("cluster-a", "infra-a"),
			cliContainerCluster("cluster-b", "infra-b"),
		},
	}
}

func cliClusterInfraWithDNS(name, machineProvider string) v1alpha1.ClusterInfra {
	return v1alpha1.ClusterInfra{
		Metadata: v1alpha1.Metadata{Name: name},
		Spec: v1alpha1.ClusterInfraSpec{Components: v1alpha1.ClusterComponents{
			Machines: []v1alpha1.ClusterMachineComponent{{
				Name: "master-0",
				From: v1alpha1.From{Provider: machineProvider, Name: "node-0"},
			}},
			NameResolution: &v1alpha1.ClusterNameResolutionComponent{
				From: v1alpha1.From{Provider: "services", Name: "default"},
			},
		}},
	}
}

func cliContainerCluster(name, infraName string) v1alpha1.ContainerCluster {
	return v1alpha1.ContainerCluster{
		Metadata: v1alpha1.Metadata{Name: name},
		Spec: v1alpha1.ContainerClusterSpec{Nodes: []v1alpha1.OCPNodeSpec{{
			Hostname: "master-0",
			Role:     "master",
			MachineRef: v1alpha1.NodeMachineRef{
				ClusterInfra: infraName,
				Name:         "master-0",
			},
		}}},
	}
}
