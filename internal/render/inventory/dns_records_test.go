package inventory

import (
	"path/filepath"
	"reflect"
	"sort"
	"strings"
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

func storageDNSRecordsState(baseDomain string) v1alpha1.State {
	return v1alpha1.State{
		Environments: []v1alpha1.Environment{{
			Spec: v1alpha1.EnvironmentSpec{
				Domains: v1alpha1.EnvironmentDomainsSpec{Base: baseDomain},
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
						Nodes: []v1alpha1.StorageCephNode{{
							Name:       "node01",
							MachineRef: v1alpha1.LocalObjectReference{Name: "ceph-1"},
							Roles:      []string{v1alpha1.StorageCephRoleMON},
						}},
					},
					MgmtGateway: &v1alpha1.StorageCephMgmtGateway{
						DNSLabel: "dashboard",
						Ingress:  v1alpha1.StorageCephMgmtGatewayIngress{Name: "lab", Address: "192.168.140.81"},
					},
				},
			},
		}},
		StorageObjectGateways: []v1alpha1.StorageObjectGateway{{
			Metadata: v1alpha1.Metadata{Name: "rgw"},
			Spec: v1alpha1.StorageObjectGatewaySpec{
				StorageClusterRef: v1alpha1.LocalObjectReference{Name: "ceph"},
				Public:            v1alpha1.StorageObjectGatewayPublic{DNSLabel: "rgw"},
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
		"rgw.ceph.example.test=192.168.140.80",
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
		"node01=192.168.140.21",
	}
	if got := recordPairs(vars["hostRecords"]); !reflect.DeepEqual(got, want) {
		t.Fatalf("hostRecords = %v, want %v", got, want)
	}
	if got := recordPairs(vars["cnameRecords"]); got != nil {
		t.Fatalf("cnameRecords = %v, want none without a fqdn", got)
	}
}

func TestNameResolutionRecordsPublishEveryGatewayIngressUnderOneDNSName(t *testing.T) {
	state := storageDNSRecordsState("example.test")
	state.StorageObjectGateways[0].Spec.Ceph.Ingresses = []v1alpha1.StorageObjectGatewayIngress{
		{Name: "dc1", Address: "192.168.141.80"},
		{Name: "dc2", Address: "192.168.142.80"},
	}
	desiredstate.Normalize(&state)

	entry := state.Environments[0].Spec.InfraComponents.NameResolution[0]
	component := state.InfraComponents[0]
	vars := nameResolutionComponentVars(state, entry, component)

	got := recordPairs(vars["hostRecords"])
	for _, want := range []string{"rgw.ceph.example.test=192.168.141.80", "rgw.ceph.example.test=192.168.142.80"} {
		found := false
		for _, pair := range got {
			if pair == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("hostRecords = %v, missing %q — a gateway sharing one public.dnsName across multiple ingresses must publish every VIP, not just the first", got, want)
		}
	}
}

func TestControllerNameResolutionVarsFromManagedDnsmasq(t *testing.T) {
	state := dnsRecordsState()
	state.Environments[0].Spec.Domains = v1alpha1.EnvironmentDomainsSpec{
		Base:              "example.test",
		Machines:          "machines.example.test",
		ContainerClusters: "ocp.example.test",
		StorageClusters:   "storage.example.test",
	}
	state.InfraComponents[0].Spec.NameResolution.BindAddress = "192.168.130.1"
	services, ok := Vars(state)["bootwright_controller_name_resolution_services"].([]any)
	if !ok || len(services) != 1 {
		t.Fatalf("controller name-resolution services = %#v, want one", services)
	}
	service := services[0].(map[string]any)
	if got, want := service["controllerAddresses"], []any{"192.168.130.1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("controllerAddresses = %v, want %v", got, want)
	}
	if got, want := service["controllerDomains"], []any{
		"cluster-a.ocp.example.test",
		"console-openshift-console.apps.cluster-a.example.test",
		"machines.example.test",
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("controllerDomains = %v, want %v", got, want)
	}
	if got := controllerProbePairs(service["controllerProbes"]); !reflect.DeepEqual(got, []string{
		"api-int.cluster-a.ocp.example.test=192.168.130.10",
		"api.cluster-a.ocp.example.test=192.168.130.10",
		"apps.cluster-a.ocp.example.test=192.168.130.11",
		"bootwright-probe.apps.cluster-a.ocp.example.test=192.168.130.11",
		"console-openshift-console.apps.cluster-a.example.test=192.168.130.11",
	}) {
		t.Fatalf("controllerProbes = %v", got)
	}
}

func TestSNOLibvirtRedfishExampleWiresControllerResolver(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join("..", "..", "..", "examples", "sno-libvirt-redfish")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}

	services, ok := Vars(state)["bootwright_controller_name_resolution_services"].([]any)
	if !ok || len(services) != 1 {
		t.Fatalf("controller name-resolution services = %#v, want one", services)
	}
	service := services[0].(map[string]any)
	if got, want := service["controllerAddresses"], []any{"192.168.132.1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("controllerAddresses = %v, want %v", got, want)
	}
	if got, want := service["controllerDomains"], []any{"bootwright.test", "sno-libvirt.bootwright.test"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("controllerDomains = %v, want %v", got, want)
	}
}

func TestControllerNameResolutionVarsSkipExternalResolver(t *testing.T) {
	state := dnsRecordsState()
	state.Environments[0].Spec.InfraComponents.NameResolution[0].Management = v1alpha1.EnvironmentComponentExternal
	state.Environments[0].Spec.InfraComponents.NameResolution[0].ComponentRef = v1alpha1.LocalObjectReference{}
	state.Environments[0].Spec.InfraComponents.NameResolution[0].Address = "192.168.130.53"
	if got := Vars(state)["bootwright_controller_name_resolution_services"]; got != nil {
		t.Fatalf("external resolver produced managed controller work: %v", got)
	}
}

func TestControllerNameResolutionVarsResolveWildcardBindEndpoint(t *testing.T) {
	state := dnsRecordsState()
	state.Environments[0].Spec.InfraComponents.NameResolution[0].EndpointRef.Name = "cluster"
	state.InfraComponents[0].Spec.NameResolution.BindAddress = "0.0.0.0"
	state.InfraComponents[0].Spec.NameResolution.Endpoints = []v1alpha1.ServiceEndpoint{{
		Name:       "cluster",
		AddressRef: v1alpha1.LocalObjectReference{Name: "cluster-lan"},
	}}
	state.Machines[0].Spec.Addresses = append(state.Machines[0].Spec.Addresses, v1alpha1.MachineAddress{
		Name:    "cluster-lan",
		Address: "192.168.130.1",
	})
	services := Vars(state)["bootwright_controller_name_resolution_services"].([]any)
	if got, want := services[0].(map[string]any)["controllerAddresses"], []any{"192.168.130.1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("controllerAddresses = %v, want endpoint address %v", got, want)
	}
}

func TestControllerNameResolutionVarsPreferSelectedEndpoint(t *testing.T) {
	state := dnsRecordsState()
	state.Environments[0].Spec.InfraComponents.NameResolution[0].EndpointRef.Name = "cluster"
	state.InfraComponents[0].Spec.NameResolution.BindAddress = "192.168.130.53"
	state.InfraComponents[0].Spec.NameResolution.Endpoints = []v1alpha1.ServiceEndpoint{{
		Name:       "cluster",
		AddressRef: v1alpha1.LocalObjectReference{Name: "cluster-lan"},
	}}
	state.Machines[0].Spec.Addresses = append(state.Machines[0].Spec.Addresses, v1alpha1.MachineAddress{
		Name:    "cluster-lan",
		Address: "192.168.130.1",
	})
	services := Vars(state)["bootwright_controller_name_resolution_services"].([]any)
	if got, want := services[0].(map[string]any)["controllerAddresses"], []any{"192.168.130.1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("controllerAddresses = %v, want selected endpoint only %v", got, want)
	}
}

func TestControllerNameResolutionVarsDoNotWidenBrokenEndpointToBind(t *testing.T) {
	state := dnsRecordsState()
	state.Environments[0].Spec.InfraComponents.NameResolution[0].EndpointRef.Name = "cluster"
	state.InfraComponents[0].Spec.NameResolution.BindAddress = "192.168.130.53"
	state.InfraComponents[0].Spec.NameResolution.Endpoints = []v1alpha1.ServiceEndpoint{{
		Name:       "cluster",
		AddressRef: v1alpha1.LocalObjectReference{Name: "missing-address"},
	}}
	services := Vars(state)["bootwright_controller_name_resolution_services"].([]any)
	if got, want := services[0].(map[string]any)["controllerAddresses"], []any{}; !reflect.DeepEqual(got, want) {
		t.Fatalf("controllerAddresses = %v, want %v; an explicit endpoint must fail closed rather than widen to bindAddress", got, want)
	}
}

func TestControllerNameResolutionVarsRejectNonRoutableBind(t *testing.T) {
	state := dnsRecordsState()
	state.InfraComponents[0].Spec.NameResolution.BindAddress = "127.0.0.1"
	services := Vars(state)["bootwright_controller_name_resolution_services"].([]any)
	if got, want := services[0].(map[string]any)["controllerAddresses"], []any{}; !reflect.DeepEqual(got, want) {
		t.Fatalf("controllerAddresses = %v, want no controller-unreachable loopback address", got)
	}
}

func TestControllerNameResolutionVarsCanonicalizeIPv6ProbeEvidence(t *testing.T) {
	state := dnsRecordsState()
	state.InfraComponents[0].Spec.NameResolution.BindAddress = "2001:0db8:0000:0000:0000:0000:0000:0053"
	state.ContainerClusters[0].Spec.Install.Endpoints[v1alpha1.EndpointAPI] = v1alpha1.Endpoint{
		Address: "2001:0db8:0000:0000:0000:0000:0000:0010",
		Source:  v1alpha1.EndpointSource{Type: v1alpha1.EndpointSourceExternal},
	}
	services := Vars(state)["bootwright_controller_name_resolution_services"].([]any)
	service := services[0].(map[string]any)
	if got, want := service["controllerAddresses"], []any{"2001:db8::53"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("controllerAddresses = %v, want canonical IPv6 %v", got, want)
	}
	for _, pair := range controllerProbePairs(service["controllerProbes"]) {
		if strings.HasPrefix(pair, "api.") && strings.HasSuffix(pair, "=2001:db8::10") {
			return
		}
	}
	t.Fatalf("controllerProbes = %v, want canonical IPv6 API evidence", service["controllerProbes"])
}

func TestControllerNameResolutionProbesGroupExactAddressSet(t *testing.T) {
	probes := nameResolutionControllerProbes([]any{
		map[string]any{"name": "api.example.test", "address": "192.0.2.11"},
		map[string]any{"name": "api.example.test", "address": "192.0.2.10"},
		map[string]any{"name": "api.example.test", "address": "2001:0db8:0:0:0:0:0:10"},
	}, nil, nil)
	want := []any{
		map[string]any{"name": "api.example.test", "addresses": []any{"192.0.2.10", "192.0.2.11", "2001:db8::10"}},
	}
	if !reflect.DeepEqual(probes, want) {
		t.Fatalf("controller probes = %#v, want exact family sets %#v", probes, want)
	}
}

func TestControllerNameResolutionProbesCoverDomainSubtree(t *testing.T) {
	probes := nameResolutionControllerProbes(nil, []any{
		map[string]any{"name": "apps.example.test", "address": "192.0.2.20"},
	}, nil)
	want := []any{
		map[string]any{"name": "apps.example.test", "addresses": []any{"192.0.2.20"}},
		map[string]any{"name": "bootwright-probe.apps.example.test", "addresses": []any{"192.0.2.20"}},
	}
	if !reflect.DeepEqual(probes, want) {
		t.Fatalf("controller probes = %#v, want apex and deterministic subtree proof %#v", probes, want)
	}
}

func TestControllerNameResolutionSubtreeProbeAvoidsExactNameCollision(t *testing.T) {
	probes := nameResolutionControllerProbes([]any{
		map[string]any{"name": "bootwright-probe.apps.example.test", "address": "192.0.2.21"},
	}, []any{
		map[string]any{"name": "apps.example.test", "address": "192.0.2.20"},
	}, nil)
	want := []any{
		map[string]any{"name": "apps.example.test", "addresses": []any{"192.0.2.20"}},
		map[string]any{"name": "bootwright-probe-2.apps.example.test", "addresses": []any{"192.0.2.20"}},
		map[string]any{"name": "bootwright-probe.apps.example.test", "addresses": []any{"192.0.2.21"}},
	}
	if !reflect.DeepEqual(probes, want) {
		t.Fatalf("controller probes = %#v, want collision-free subtree proof %#v", probes, want)
	}
}

func TestControllerNameResolutionVarsKeepMultipleManagedServicesDistinct(t *testing.T) {
	state := dnsRecordsState()
	state.InfraComponents[0].Spec.NameResolution.BindAddress = "192.168.130.1"
	secondEntry := state.Environments[0].Spec.InfraComponents.NameResolution[0]
	secondEntry.Name = "secondary"
	secondEntry.ComponentRef.Name = "dns-secondary"
	state.Environments[0].Spec.InfraComponents.NameResolution = append(state.Environments[0].Spec.InfraComponents.NameResolution, secondEntry)
	state.NetworkConfigs[0].Spec.NameResolutionRefs = append(state.NetworkConfigs[0].Spec.NameResolutionRefs, v1alpha1.LocalObjectReference{Name: "secondary"})
	secondComponent := state.InfraComponents[0]
	secondComponent.Metadata.Name = "dns-secondary"
	secondDNS := *secondComponent.Spec.NameResolution
	secondDNS.BindAddress = "192.168.130.2"
	secondComponent.Spec.NameResolution = &secondDNS
	state.InfraComponents = append(state.InfraComponents, secondComponent)

	services := Vars(state)["bootwright_controller_name_resolution_services"].([]any)
	if len(services) != 2 {
		t.Fatalf("controller name-resolution services = %#v, want two independently owned services", services)
	}
	want := map[string][]any{
		"dns":           {"192.168.130.1"},
		"dns-secondary": {"192.168.130.2"},
	}
	for _, raw := range services {
		service := raw.(map[string]any)
		name := service["name"].(string)
		addresses, ok := want[name]
		if !ok {
			t.Fatalf("unexpected controller name-resolution service %q: %#v", name, service)
		}
		if got := service["controllerAddresses"]; !reflect.DeepEqual(got, addresses) {
			t.Fatalf("controllerAddresses for %s = %v, want %v", name, got, addresses)
		}
		delete(want, name)
	}
	if len(want) != 0 {
		t.Fatalf("controller name-resolution projection merged distinct services: missing %v", want)
	}
}

func dnsRecordsState() v1alpha1.State {
	return v1alpha1.State{
		Environments: []v1alpha1.Environment{{
			Spec: v1alpha1.EnvironmentSpec{
				Domains: v1alpha1.EnvironmentDomainsSpec{Base: "example.test"},
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
				Capabilities: []string{v1alpha1.MachineCapabilityContainerRuntime},
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
					Name:       "master-a",
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
		switch record := item.(type) {
		case map[string]any:
			out = append(out, record["name"].(string)+"="+record["address"].(string))
		case map[string]string:
			out = append(out, record["name"]+"="+record["address"])
		default:
			panic("unexpected DNS record projection")
		}
	}
	sort.Strings(out)
	return out
}

func controllerProbePairs(raw any) []string {
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	var out []string
	for _, item := range items {
		record, ok := item.(map[string]any)
		if !ok {
			panic("unexpected controller DNS probe projection")
		}
		name, _ := record["name"].(string)
		for _, rawAddress := range record["addresses"].([]any) {
			out = append(out, name+"="+rawAddress.(string))
		}
	}
	sort.Strings(out)
	return out
}
