package render

import (
	"reflect"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func TestProviderServicesVarsMergeSharedDNSWithoutMutatingState(t *testing.T) {
	state := dnsRecordsState()
	state.ClusterInfras = append(state.ClusterInfras, state.ClusterInfras[0])
	state.ClusterInfras[1].Metadata.Name = "infra-b"
	state.ContainerClusters = append(state.ContainerClusters, state.ContainerClusters[0])
	state.ContainerClusters[1].Metadata.Name = "cluster-b"
	state.ContainerClusters[1].Spec.Nodes[0].MachineRef.ClusterInfra = "infra-b"
	state.Environments[0].Spec.InfraComponents.NameResolution[0].AdditionalIngressHosts = []string{"app-a.example.test", "shared.example.test"}
	state.InfraComponents[0].Spec.NameResolution.AdditionalIngressHosts = []string{"app-b.example.test", "shared.example.test"}

	before := append([]string(nil), state.Environments[0].Spec.InfraComponents.NameResolution[0].AdditionalIngressHosts...)
	services := providerServicesVars(state)
	if len(services) != 1 {
		t.Fatalf("provider services = %d, want 1: %+v", len(services), services)
	}
	service := services[0].(map[string]any)
	got := service["additionalIngressHosts"]
	want := []string{"app-a.example.test", "app-b.example.test", "shared.example.test"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("additionalIngressHosts = %v, want %v", got, want)
	}
	if !reflect.DeepEqual(state.Environments[0].Spec.InfraComponents.NameResolution[0].AdditionalIngressHosts, before) {
		t.Fatalf("render mutated authored state: %v", state.Environments[0].Spec.InfraComponents.NameResolution[0].AdditionalIngressHosts)
	}
}

func TestProviderServicesVarsUsesGraphEntryConsumersForSharedDNS(t *testing.T) {
	state := dnsRecordsState()
	state.Environments[0].Spec.InfraComponents.NameResolution = append(state.Environments[0].Spec.InfraComponents.NameResolution, v1alpha1.EnvironmentNameResolutionComponent{
		Name:                   "alternate",
		Type:                   v1alpha1.EnvironmentComponentManaged,
		ComponentRef:           v1alpha1.LocalObjectReference{Name: "dns"},
		AdditionalIngressHosts: []string{"console-openshift-console.apps.cluster-b.example.test"},
	})
	state.NetworkConfigs = append(state.NetworkConfigs, state.NetworkConfigs[0])
	state.NetworkConfigs[1].Metadata.Name = "managed-net-b"
	state.NetworkConfigs[1].Spec.DNSRefs = []string{"alternate"}
	state.ClusterInfras = append(state.ClusterInfras, state.ClusterInfras[0])
	state.ClusterInfras[1].Metadata.Name = "infra-b"
	state.ClusterInfras[1].Spec.Endpoints = map[string]v1alpha1.Endpoint{
		v1alpha1.EndpointAPI: {Address: "192.168.131.10"},
		"api-int":            {Address: "192.168.131.10"},
		"apps":               {Address: "192.168.131.11"},
	}
	state.ClusterInfras[1].Spec.Components.Machines = append([]v1alpha1.ClusterMachineComponent(nil), state.ClusterInfras[1].Spec.Components.Machines...)
	state.ClusterInfras[1].Spec.Components.Machines[0].NetworkConfig.Ref.Name = "managed-net-b"
	state.ContainerClusters = append(state.ContainerClusters, state.ContainerClusters[0])
	state.ContainerClusters[1].Metadata.Name = "cluster-b"
	state.ContainerClusters[1].Spec.Nodes = append([]v1alpha1.OCPNodeSpec(nil), state.ContainerClusters[1].Spec.Nodes...)
	state.ContainerClusters[1].Spec.Nodes[0].MachineRef.ClusterInfra = "infra-b"

	services := providerServicesVars(state)
	if len(services) != 1 {
		t.Fatalf("provider services = %d, want 1: %+v", len(services), services)
	}
	service := services[0].(map[string]any)
	wantHosts := []string{
		"api-int.cluster-a.example.test=192.168.130.10",
		"api-int.cluster-b.example.test=192.168.131.10",
		"api.cluster-a.example.test=192.168.130.10",
		"api.cluster-b.example.test=192.168.131.10",
		"console-openshift-console.apps.cluster-a.example.test=192.168.130.11",
		"console-openshift-console.apps.cluster-b.example.test=192.168.131.11",
	}
	if got := recordPairs(service["hostRecords"]); !reflect.DeepEqual(got, wantHosts) {
		t.Fatalf("hostRecords = %v, want %v", got, wantHosts)
	}
}
