package render

import (
	"reflect"
	"testing"
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
