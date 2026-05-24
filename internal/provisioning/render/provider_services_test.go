package render

import (
	"reflect"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func TestProviderServicesVarsMergeSharedDNSWithoutMutatingState(t *testing.T) {
	state := v1alpha1.State{
		Hosts: []v1alpha1.Host{{
			Metadata: v1alpha1.Metadata{Name: "lab-host"},
			Spec: v1alpha1.HostSpec{
				Addresses:    []v1alpha1.HostAddress{{Name: "ssh", Address: "10.0.0.5"}},
				SSH:          &v1alpha1.HostSSHSpec{AddressName: "ssh"},
				Capabilities: []string{"container-runtime"},
			},
		}},
		InfraProviders: []v1alpha1.InfraProvider{{
			Metadata: v1alpha1.Metadata{Name: "lab"},
			Spec: v1alpha1.InfraProviderSpec{
				DNS: []v1alpha1.DNSCapability{{
					Name:    "default",
					Dnsmasq: &v1alpha1.DnsmasqCapability{HostRef: v1alpha1.LocalObjectReference{Name: "lab-host"}},
				}},
			},
		}},
		ClusterInfras: []v1alpha1.ClusterInfra{
			{
				Metadata: v1alpha1.Metadata{Name: "infra-a"},
				Spec: v1alpha1.ClusterInfraSpec{
					Components: v1alpha1.ClusterComponents{
						NameResolution: &v1alpha1.ClusterNameResolutionComponent{
							From:                   v1alpha1.From{Provider: "lab", Name: "default"},
							AdditionalIngressHosts: []string{"app-a.example.test", "shared.example.test"},
						},
					},
				},
			},
			{
				Metadata: v1alpha1.Metadata{Name: "infra-b"},
				Spec: v1alpha1.ClusterInfraSpec{
					Components: v1alpha1.ClusterComponents{
						NameResolution: &v1alpha1.ClusterNameResolutionComponent{
							From:                   v1alpha1.From{Provider: "lab", Name: "default"},
							AdditionalIngressHosts: []string{"app-b.example.test", "shared.example.test"},
						},
					},
				},
			},
		},
		ContainerClusters: []v1alpha1.ContainerCluster{
			clusterForInfra("cluster-a", "infra-a", "master-a"),
			clusterForInfra("cluster-b", "infra-b", "master-b"),
		},
	}
	before := append([]string(nil), state.ClusterInfras[0].Spec.Components.NameResolution.AdditionalIngressHosts...)
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
	if !reflect.DeepEqual(state.ClusterInfras[0].Spec.Components.NameResolution.AdditionalIngressHosts, before) {
		t.Fatalf("render mutated authored state: %v", state.ClusterInfras[0].Spec.Components.NameResolution.AdditionalIngressHosts)
	}
}

func clusterForInfra(name, infra, machine string) v1alpha1.ContainerCluster {
	return v1alpha1.ContainerCluster{
		Metadata: v1alpha1.Metadata{Name: name},
		Spec: v1alpha1.ContainerClusterSpec{
			Nodes: []v1alpha1.OCPNodeSpec{{
				Hostname: machine,
				Role:     v1alpha1.NodeRoleMaster,
				MachineRef: v1alpha1.NodeMachineRef{
					ClusterInfra: infra,
					Name:         machine,
				},
			}},
		},
	}
}
