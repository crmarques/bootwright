package render

import (
	"reflect"
	"sort"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func TestNameResolutionRecordsIncludeDNSRefConsumers(t *testing.T) {
	state := dnsRecordsState()
	entry := state.Environments[0].Spec.InfraComponents.NameResolution[0]
	component := state.InfraComponents[0]
	vars := nameResolutionComponentVars(state, entry, component)

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

func dnsRecordsState() v1alpha1.State {
	return v1alpha1.State{
		Environments: []v1alpha1.Environment{{
			Spec: v1alpha1.EnvironmentSpec{
				BaseDomain: "example.test",
				InfraComponents: v1alpha1.EnvironmentInfraComponentsSpec{
					NameResolution: []v1alpha1.EnvironmentNameResolutionComponent{{
						Name:                   "default",
						Type:                   v1alpha1.EnvironmentComponentManaged,
						ComponentRef:           v1alpha1.LocalObjectReference{Name: "dns"},
						AdditionalIngressHosts: []string{"console-openshift-console.apps.cluster-a.example.test"},
					}},
				},
			},
		}},
		InfraComponents: []v1alpha1.InfraComponent{{
			Metadata: v1alpha1.Metadata{Name: "dns"},
			Spec: v1alpha1.InfraComponentSpec{NameResolution: &v1alpha1.NameResolutionComponent{
				Type:    v1alpha1.InfraComponentTypeDnsmasq,
				HostRef: v1alpha1.LocalObjectReference{Name: "host"},
			}},
		}},
		NetworkConfigs: []v1alpha1.NetworkConfig{{
			Metadata: v1alpha1.Metadata{Name: "managed-net"},
			Spec: v1alpha1.NetworkConfigSpec{
				DNSRefs: []string{"default"},
			},
		}},
		ClusterInfras: []v1alpha1.ClusterInfra{{
			Metadata: v1alpha1.Metadata{Name: "infra-a"},
			Spec: v1alpha1.ClusterInfraSpec{
				Endpoints: map[string]v1alpha1.Endpoint{
					v1alpha1.EndpointAPI:     {ExternalVIP: "192.168.130.10"},
					v1alpha1.EndpointAPIInt:  {ExternalVIP: "192.168.130.10"},
					v1alpha1.EndpointIngress: {ExternalVIP: "192.168.130.11"},
				},
				Components: v1alpha1.ClusterComponents{Machines: []v1alpha1.ClusterMachineComponent{{
					Name: "master-a",
					NetworkConfig: v1alpha1.ClusterMachineNetworkConfig{
						Ref: v1alpha1.LocalObjectReference{Name: "managed-net"},
					},
				}}},
			},
		}},
		ContainerClusters: []v1alpha1.ContainerCluster{{
			Metadata: v1alpha1.Metadata{Name: "cluster-a"},
			Spec: v1alpha1.ContainerClusterSpec{Nodes: []v1alpha1.OCPNodeSpec{{
				Hostname: "master-a",
				MachineRef: v1alpha1.NodeMachineRef{
					ClusterInfra: "infra-a",
					Name:         "master-a",
				},
			}}},
		}},
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
