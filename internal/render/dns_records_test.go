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
				Type:       v1alpha1.InfraComponentTypeDnsmasq,
				MachineRef: v1alpha1.LocalObjectReference{Name: "host"},
			}},
		}},
		NetworkConfigs: []v1alpha1.NetworkConfig{{
			Metadata: v1alpha1.Metadata{Name: "managed-net"},
			Spec: v1alpha1.NetworkConfigSpec{
				DNSRefs: []string{"default"},
			},
		}},
		Machines: []v1alpha1.Machine{{
			Metadata: v1alpha1.Metadata{Name: "host"},
			Spec: v1alpha1.MachineSpec{
				Capabilities: []string{v1alpha1.MachineCapabilityNameResolution},
				OS: v1alpha1.MachineOSSpec{
					Provided: v1alpha1.BoolPtr(true),
				},
				Addresses: []v1alpha1.MachineAddress{{Name: "ssh", Address: "10.0.0.5"}},
				Access: v1alpha1.MachineAccess{
					SSH: &v1alpha1.MachineSSHSpec{AddressRef: v1alpha1.LocalObjectReference{Name: "ssh"}},
				},
			},
		}, {
			Metadata: v1alpha1.Metadata{Name: "master-a"},
			Spec: v1alpha1.MachineSpec{
				Capabilities: []string{v1alpha1.MachineCapabilityOpenShiftNode},
				OS: v1alpha1.MachineOSSpec{
					Provided: v1alpha1.BoolPtr(false),
				},
				Network: v1alpha1.MachineNetwork{
					Config: v1alpha1.MachineNetworkConfig{
						NetworkConfigRef: v1alpha1.LocalObjectReference{Name: "managed-net"},
					},
				},
			},
		}},
		ContainerClusters: []v1alpha1.ContainerCluster{{
			Metadata: v1alpha1.Metadata{Name: "cluster-a"},
			Spec: v1alpha1.ContainerClusterSpec{
				Install: v1alpha1.OCPInstallSpec{Endpoints: map[string]v1alpha1.Endpoint{
					v1alpha1.EndpointAPI:     {Address: "192.168.130.10"},
					v1alpha1.EndpointAPIInt:  {Address: "192.168.130.10"},
					v1alpha1.EndpointIngress: {Address: "192.168.130.11"},
				}},
				Nodes: []v1alpha1.OCPNodeSpec{{
					Hostname:   "master-a",
					MachineRef: v1alpha1.LocalObjectReference{Name: "master-a"},
				}},
			},
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
