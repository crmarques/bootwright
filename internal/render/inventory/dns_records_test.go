package inventory

import (
	"reflect"
	"sort"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	desiredstate "github.com/crmarques/bootwright/internal/state/desired"
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

// TestNameResolutionRecordsPublishStorageNodesAndGateway covers the storage-only
// path: the resolver publishes each served node by its normalized FQDN and bare
// name (so the cephadm dashboard can dial e.g. an alertmanager host), plus the
// object gateway's public dnsName at its ingress VIP and the management dnsName
// at its mgmt-gateway VIP.
func TestNameResolutionRecordsPublishStorageNodesAndGateway(t *testing.T) {
	state := v1alpha1.State{
		Environments: []v1alpha1.Environment{{
			Spec: v1alpha1.EnvironmentSpec{
				BaseDomain: "example.test",
				InfraComponents: v1alpha1.EnvironmentInfraComponentsSpec{
					NameResolution: []v1alpha1.EnvironmentNameResolutionComponent{{
						Name:         "lab-dns",
						Management:   v1alpha1.EnvironmentComponentManaged,
						ComponentRef: v1alpha1.LocalObjectReference{Name: "dns"},
					}},
				},
			},
		}},
		InfraComponents: []v1alpha1.InfraComponent{{
			Metadata: v1alpha1.Metadata{Name: "dns"},
			Spec: v1alpha1.InfraComponentSpec{
				Type: v1alpha1.ComponentSlotNameResolution,
				NameResolution: &v1alpha1.NameResolutionComponent{
					Implementation: v1alpha1.InfraComponentTypeDnsmasq,
					MachineRef:     v1alpha1.LocalObjectReference{Name: "bastion"},
				},
			},
		}},
		NetworkConfigs: []v1alpha1.NetworkConfig{{
			Metadata: v1alpha1.Metadata{Name: "ceph-net"},
			Spec: v1alpha1.NetworkConfigSpec{
				NameResolutionRefs: []v1alpha1.LocalObjectReference{{Name: "lab-dns"}},
			},
		}},
		Machines: []v1alpha1.Machine{{
			Metadata: v1alpha1.Metadata{Name: "ceph-1"},
			Spec: v1alpha1.MachineSpec{
				Network: v1alpha1.MachineNetwork{
					Config: v1alpha1.MachineNetworkConfig{
						NetworkConfigRef: v1alpha1.LocalObjectReference{Name: "ceph-net"},
					},
				},
				Addresses: []v1alpha1.MachineAddress{{Name: "ssh", Address: "192.168.140.21"}},
				Access: v1alpha1.MachineAccess{
					SSH: &v1alpha1.MachineSSHSpec{AddressRef: v1alpha1.LocalObjectReference{Name: "ssh"}},
				},
			},
		}},
		StorageClusters: []v1alpha1.StorageCluster{{
			Metadata: v1alpha1.Metadata{Name: "ceph"},
			Spec: v1alpha1.StorageClusterSpec{
				Ceph: &v1alpha1.StorageClusterCephSpec{
					Topology: v1alpha1.StorageCephTopology{
						Hosts: []v1alpha1.StorageCephHost{{
							MachineRef: v1alpha1.LocalObjectReference{Name: "ceph-1"},
							Roles:      []string{v1alpha1.StorageCephRoleMON},
						}},
					},
					Management: &v1alpha1.StorageCephManagement{
						DNSName: "dashboard.ceph.example.test",
						Ingress: v1alpha1.StorageCephManagementIngress{Name: "lab", Address: "192.168.140.81"},
					},
				},
			},
		}},
		StorageObjectGateways: []v1alpha1.StorageObjectGateway{{
			Metadata: v1alpha1.Metadata{Name: "rgw"},
			Spec: v1alpha1.StorageObjectGatewaySpec{
				StorageClusterRef: v1alpha1.LocalObjectReference{Name: "ceph"},
				Public:            v1alpha1.StorageObjectGatewayPublic{DNSName: "rgw.example.test"},
				Ceph: v1alpha1.StorageObjectGatewayCephSpec{
					Ingresses: []v1alpha1.StorageObjectGatewayIngress{{Name: "lab", Address: "192.168.140.80"}},
				},
			},
		}},
	}
	desiredstate.Normalize(&state)

	entry := state.Environments[0].Spec.InfraComponents.NameResolution[0]
	component := state.InfraComponents[0]
	vars := nameResolutionComponentVars(state, entry, component)

	want := []string{
		"ceph-1.ceph.example.test=192.168.140.21",
		"ceph-1=192.168.140.21",
		"dashboard.ceph.example.test=192.168.140.81",
		"rgw.example.test=192.168.140.80",
	}
	if got := recordPairs(vars["hostRecords"]); !reflect.DeepEqual(got, want) {
		t.Fatalf("hostRecords = %v, want %v", got, want)
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
						Management:             v1alpha1.EnvironmentComponentManaged,
						ComponentRef:           v1alpha1.LocalObjectReference{Name: "dns"},
						AdditionalIngressHosts: []string{"console-openshift-console.apps.cluster-a.example.test"},
					}},
				},
			},
		}},
		InfraComponents: []v1alpha1.InfraComponent{{
			Metadata: v1alpha1.Metadata{Name: "dns"},
			Spec: v1alpha1.InfraComponentSpec{
				Type: v1alpha1.ComponentSlotNameResolution, NameResolution: &v1alpha1.NameResolutionComponent{
					Implementation: v1alpha1.InfraComponentTypeDnsmasq,
					MachineRef:     v1alpha1.LocalObjectReference{Name: "host"},
				}},
		}},
		NetworkConfigs: []v1alpha1.NetworkConfig{{
			Metadata: v1alpha1.Metadata{Name: "managed-net"},
			Spec: v1alpha1.NetworkConfigSpec{
				NameResolutionRefs: []v1alpha1.LocalObjectReference{{Name: "default"}},
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
				Hosts: []v1alpha1.OCPHostSpec{{
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
