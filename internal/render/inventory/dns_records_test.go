package inventory

import (
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/render/installer"
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

func storageDNSRecordsState(baseDomain string) v1alpha1.State {
	return v1alpha1.State{
		Environments: []v1alpha1.Environment{{
			Spec: v1alpha1.EnvironmentSpec{
				BaseDomain: baseDomain,
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
}

func TestNameResolutionRecordsPublishStorageNodesAndGateway(t *testing.T) {
	state := storageDNSRecordsState("example.test")
	desiredstate.Normalize(&state)

	entry := state.Environments[0].Spec.InfraComponents.NameResolution[0]
	component := state.InfraComponents[0]
	vars := nameResolutionComponentVars(state, entry, component)

	want := []string{
		"ceph-1.example.test=192.168.140.21",
		"dashboard.ceph.example.test=192.168.140.81",
		"rgw.example.test=192.168.140.80",
	}
	if got := recordPairs(vars["hostRecords"]); !reflect.DeepEqual(got, want) {
		t.Fatalf("hostRecords = %v, want %v", got, want)
	}
	for _, pair := range recordPairs(vars["hostRecords"]) {
		if strings.HasPrefix(pair, "ceph-1=") {
			t.Fatalf("hostRecords = %v, must not publish the bare machine label", vars["hostRecords"])
		}
	}
	wantCnames := []string{"node01.ceph.example.test=ceph-1.example.test"}
	if got := recordPairs(vars["cnameRecords"]); !reflect.DeepEqual(got, wantCnames) {
		t.Fatalf("cnameRecords = %v, want %v", got, wantCnames)
	}
}

func TestNameResolutionRecordsFallBackToMachineLabelWithoutBaseDomain(t *testing.T) {
	state := storageDNSRecordsState("")
	desiredstate.Normalize(&state)

	entry := state.Environments[0].Spec.InfraComponents.NameResolution[0]
	component := state.InfraComponents[0]
	vars := nameResolutionComponentVars(state, entry, component)

	want := []string{
		"ceph-1=192.168.140.21",
		"dashboard.ceph.example.test=192.168.140.81",
		"node01=192.168.140.21",
		"rgw.example.test=192.168.140.80",
	}
	if got := recordPairs(vars["hostRecords"]); !reflect.DeepEqual(got, want) {
		t.Fatalf("hostRecords = %v, want %v", got, want)
	}
	if got := recordPairs(vars["cnameRecords"]); got != nil {
		t.Fatalf("cnameRecords = %v, want none without a dnsEntry", got)
	}
}

func TestClusterControllerNameResolversFromManagedDnsmasq(t *testing.T) {
	state := dnsRecordsState()
	state.InfraComponents[0].Spec.NameResolution.BindAddress = "192.168.130.1"
	ci, err := installer.ClusterInstallForOCP(state, state.ContainerClusters[0])
	if err != nil {
		t.Fatalf("ClusterInstallForOCP: %v", err)
	}

	got := ClusterControllerNameResolvers(state, ci)
	want := []any{map[string]any{"bindAddress": "192.168.130.1", "domain": "example.test"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ClusterControllerNameResolvers = %v, want %v", got, want)
	}
}

func TestSNOLibvirtRedfishExampleWiresControllerResolver(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join("..", "..", "..", "examples", "sno-libvirt-redfish")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}

	clusters, ok := Vars(state)["bootwright_clusters"].([]any)
	if !ok || len(clusters) == 0 {
		t.Fatal("no bootwright_clusters rendered")
	}
	entry := clusters[0].(map[string]any)
	resolvers, ok := entry["controllerNameResolvers"].([]any)
	if !ok || len(resolvers) != 1 {
		t.Fatalf("controllerNameResolvers = %v, want exactly one entry", entry["controllerNameResolvers"])
	}
	if got := resolvers[0].(map[string]any); got["bindAddress"] != "192.168.132.1" || got["domain"] != "bootwright.test" {
		t.Fatalf("controllerNameResolvers[0] = %v, want {192.168.132.1, bootwright.test}", got)
	}
}

func TestClusterControllerNameResolversSkipsExternal(t *testing.T) {
	state := dnsRecordsState()
	state.InfraComponents[0].Spec.NameResolution.BindAddress = "192.168.130.1"
	state.Environments[0].Spec.InfraComponents.NameResolution[0].Management = v1alpha1.EnvironmentComponentExternal
	ci, err := installer.ClusterInstallForOCP(state, state.ContainerClusters[0])
	if err != nil {
		t.Fatalf("ClusterInstallForOCP: %v", err)
	}

	if got := ClusterControllerNameResolvers(state, ci); got != nil {
		t.Fatalf("ClusterControllerNameResolvers = %v, want nil for external entry", got)
	}
}

func TestClusterControllerNameResolversSkipsWildcardBind(t *testing.T) {
	for _, bind := range []string{"::", "0.0.0.0", ""} {
		state := dnsRecordsState()
		state.InfraComponents[0].Spec.NameResolution.BindAddress = bind
		ci, err := installer.ClusterInstallForOCP(state, state.ContainerClusters[0])
		if err != nil {
			t.Fatalf("ClusterInstallForOCP: %v", err)
		}
		if got := ClusterControllerNameResolvers(state, ci); got != nil {
			t.Fatalf("ClusterControllerNameResolvers(bind=%q) = %v, want nil", bind, got)
		}
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
