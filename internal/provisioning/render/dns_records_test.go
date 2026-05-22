package render

import (
	"reflect"
	"sort"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func TestNameResolutionRecordsIncludeResolverConsumers(t *testing.T) {
	state := dnsRecordsState("192.168.130.1")
	vars := nameResolutionComponentVars(state, state.ClusterInfras[0].Spec.Components.NameResolution)

	wantHosts := []string{
		"api-int.cluster-a.example.test=192.168.130.10",
		"api-int.cluster-b.example.test=192.168.130.30",
		"api.cluster-a.example.test=192.168.130.10",
		"api.cluster-b.example.test=192.168.130.30",
		"console-openshift-console.apps.cluster-a.example.test=192.168.130.11",
	}
	if got := recordPairs(vars["hostRecords"]); !reflect.DeepEqual(got, wantHosts) {
		t.Fatalf("hostRecords = %v, want %v", got, wantHosts)
	}

	wantDomains := []string{
		"apps.cluster-a.example.test=192.168.130.11",
		"apps.cluster-b.example.test=192.168.130.31",
	}
	if got := recordPairs(vars["domainRecords"]); !reflect.DeepEqual(got, wantDomains) {
		t.Fatalf("domainRecords = %v, want %v", got, wantDomains)
	}
}

func TestNameResolutionRecordsIgnoreUnmatchedResolverConsumers(t *testing.T) {
	state := dnsRecordsState("192.168.130.254")
	vars := nameResolutionComponentVars(state, state.ClusterInfras[0].Spec.Components.NameResolution)

	wantHosts := []string{
		"api-int.cluster-a.example.test=192.168.130.10",
		"api.cluster-a.example.test=192.168.130.10",
		"console-openshift-console.apps.cluster-a.example.test=192.168.130.11",
	}
	if got := recordPairs(vars["hostRecords"]); !reflect.DeepEqual(got, wantHosts) {
		t.Fatalf("hostRecords = %v, want %v", got, wantHosts)
	}

	wantDomains := []string{"apps.cluster-a.example.test=192.168.130.11"}
	if got := recordPairs(vars["domainRecords"]); !reflect.DeepEqual(got, wantDomains) {
		t.Fatalf("domainRecords = %v, want %v", got, wantDomains)
	}
}

func dnsRecordsState(externalResolver string) v1alpha1.State {
	return v1alpha1.State{
		NetworkConfigs: []v1alpha1.NetworkConfig{
			{
				Metadata: v1alpha1.Metadata{Name: "managed-net"},
				Spec: v1alpha1.NetworkConfigSpec{
					MachineNetwork: []v1alpha1.MachineNetworkCIDR{{CIDR: "192.168.130.0/24"}},
				},
			},
			{
				Metadata: v1alpha1.Metadata{Name: "external-net"},
				Spec: v1alpha1.NetworkConfigSpec{
					MachineNetwork: []v1alpha1.MachineNetworkCIDR{{CIDR: "192.168.130.0/24"}},
					Template: v1alpha1.NetworkConfigTemplate{NetworkConfig: map[string]any{
						"dns-resolver": map[string]any{
							"config": map[string]any{
								"server": []any{externalResolver},
							},
						},
					}},
				},
			},
		},
		ClusterInfras: []v1alpha1.ClusterInfra{
			dnsRecordsClusterInfra("infra-a", "managed-net", "192.168.130.10", "192.168.130.11", true),
			dnsRecordsClusterInfra("infra-b", "external-net", "192.168.130.30", "192.168.130.31", false),
		},
		ContainerClusters: []v1alpha1.ContainerCluster{
			dnsRecordsContainerCluster("cluster-a", "infra-a", "master-a"),
			dnsRecordsContainerCluster("cluster-b", "infra-b", "master-b"),
		},
	}
}

func dnsRecordsClusterInfra(name, networkName, apiVIP, ingressVIP string, managedDNS bool) v1alpha1.ClusterInfra {
	ci := v1alpha1.ClusterInfra{
		Metadata: v1alpha1.Metadata{Name: name},
		Spec: v1alpha1.ClusterInfraSpec{
			Endpoints: map[string]v1alpha1.Endpoint{
				v1alpha1.EndpointAPI:     {ExternalVIP: apiVIP},
				v1alpha1.EndpointAPIInt:  {ExternalVIP: apiVIP},
				v1alpha1.EndpointIngress: {ExternalVIP: ingressVIP},
			},
			Components: v1alpha1.ClusterComponents{
				Machines: []v1alpha1.ClusterMachineComponent{{
					Name: "master-" + name[len(name)-1:],
					NetworkConfig: v1alpha1.ClusterMachineNetworkConfig{
						Ref: v1alpha1.LocalObjectReference{Name: networkName},
					},
				}},
			},
		},
	}
	if managedDNS {
		ci.Spec.Components.NameResolution = &v1alpha1.ClusterNameResolutionComponent{
			From:                   v1alpha1.From{Provider: "lab", Name: "default"},
			BindAddress:            "192.168.130.1",
			AdditionalIngressHosts: []string{"console-openshift-console.apps.cluster-a.example.test"},
		}
	}
	return ci
}

func dnsRecordsContainerCluster(name, infra, machine string) v1alpha1.ContainerCluster {
	return v1alpha1.ContainerCluster{
		Metadata: v1alpha1.Metadata{Name: name},
		Spec: v1alpha1.ContainerClusterSpec{
			Install: v1alpha1.OCPInstallSpec{BaseDomain: "example.test"},
			Nodes: []v1alpha1.OCPNodeSpec{{
				Hostname: machine,
				MachineRef: v1alpha1.NodeMachineRef{
					ClusterInfra: infra,
					Name:         machine,
				},
			}},
		},
	}
}

func recordPairs(raw any) []string {
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		record := item.(map[string]any)
		out = append(out, record["name"].(string)+"="+record["address"].(string))
	}
	sort.Strings(out)
	return out
}
