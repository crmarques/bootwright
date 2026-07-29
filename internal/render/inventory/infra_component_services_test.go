package inventory

import (
	"reflect"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func TestInfraComponentServicesVarsMergeSharedDNSWithoutMutatingState(t *testing.T) {
	state := dnsRecordsState()
	state.ContainerClusters = append(state.ContainerClusters, state.ContainerClusters[0])
	state.ContainerClusters[1].Metadata.Name = "cluster-b"
	state.ContainerClusters[1].Spec.Nodes[0].MachineRef.Name = "master-b"
	state.Machines = append(state.Machines, state.Machines[1])
	state.Machines[2].Metadata.Name = "master-b"
	state.Environments[0].Spec.InfraComponents.NameResolution[0].AdditionalIngressHosts = []string{"app-a.example.test", "shared.example.test"}
	state.InfraComponents[0].Spec.NameResolution.AdditionalIngressHosts = []string{"app-b.example.test", "shared.example.test"}

	before := append([]string(nil), state.Environments[0].Spec.InfraComponents.NameResolution[0].AdditionalIngressHosts...)
	services := infraComponentServicesVars(state)
	if len(services) != 1 {
		t.Fatalf("infra component services = %d, want 1: %+v", len(services), services)
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

func TestFabricHostDesiredVarsFiltersByHost(t *testing.T) {
	state := dnsRecordsState()
	services := infraComponentServicesVars(state)
	if len(services) == 0 {
		t.Fatal("infra component services empty; fixture changed")
	}
	host, _ := services[0].(map[string]any)["machineRef"].(string)
	if host == "" {
		t.Fatal("infra component service has no machineRef; the fabric host projection would be silently empty")
	}

	got := FabricHostDesiredVars(state, host)
	if len(got) == 0 {
		t.Fatalf("FabricHostDesiredVars(%q) is empty; fabric host vars no longer carry machineRef", host)
	}
	for i, entry := range got {
		if mref, _ := entry.(map[string]any)["machineRef"].(string); mref != host {
			t.Fatalf("FabricHostDesiredVars[%d].machineRef = %q, want %q", i, mref, host)
		}
	}
	if other := FabricHostDesiredVars(state, host+"-does-not-exist"); len(other) != 0 {
		t.Fatalf("FabricHostDesiredVars for an unknown host = %d entries, want 0", len(other))
	}
}

func TestInfraComponentServicesVarsSchedulesStorageNameResolutionWithForwarders(t *testing.T) {
	state := v1alpha1.State{
		Environments: []v1alpha1.Environment{{
			Spec: v1alpha1.EnvironmentSpec{
				Domains: v1alpha1.EnvironmentDomainsSpec{Base: "example.test"},
				InfraComponents: v1alpha1.EnvironmentInfraComponentsSpec{
					NameResolution: []v1alpha1.EnvironmentNameResolutionComponent{{
						Name:         "lab-dns",
						Management:   v1alpha1.EnvironmentComponentManaged,
						ComponentRef: v1alpha1.LocalObjectReference{Name: "lab-dns"},
					}},
				},
			},
		}},
		InfraComponents: []v1alpha1.InfraComponent{{
			Metadata: v1alpha1.Metadata{Name: "lab-dns"},
			Spec: v1alpha1.InfraComponentSpec{
				Type: v1alpha1.ComponentSlotNameResolution, NameResolution: &v1alpha1.NameResolutionComponent{
					Implementation: v1alpha1.InfraComponentTypeDnsmasq,
					MachineRef:     v1alpha1.LocalObjectReference{Name: "bastion"},
					BindAddress:    "192.168.134.1",
					Forwarders:     []string{"1.1.1.1", "9.9.9.9"},
				}},
		}},
		NetworkConfigs: []v1alpha1.NetworkConfig{{
			Metadata: v1alpha1.Metadata{Name: "ceph-bridge"},
			Spec:     v1alpha1.NetworkConfigSpec{NameResolutionRefs: []v1alpha1.LocalObjectReference{{Name: "lab-dns"}}},
		}},
		Machines: []v1alpha1.Machine{{
			Metadata: v1alpha1.Metadata{Name: "bastion"},
			Spec: v1alpha1.MachineSpec{
				Capabilities: []string{v1alpha1.MachineCapabilityContainerRuntime},
				OS:           v1alpha1.MachineOSSpec{Provided: v1alpha1.BoolPtr(true)},
				Addresses:    []v1alpha1.MachineAddress{{Name: "cluster-lan", Address: "192.168.134.1"}},
			},
		}, {
			Metadata: v1alpha1.Metadata{Name: "ceph-0"},
			Spec: v1alpha1.MachineSpec{
				OS: v1alpha1.MachineOSSpec{Provided: v1alpha1.BoolPtr(false)},
				Network: v1alpha1.MachineNetwork{
					Config: v1alpha1.MachineNetworkConfig{
						NetworkConfigRef: v1alpha1.LocalObjectReference{Name: "ceph-bridge"},
					},
				},
			},
		}},
		StorageClusters: []v1alpha1.StorageCluster{{
			Metadata: v1alpha1.Metadata{Name: "ceph-libvirt"},
			Spec: v1alpha1.StorageClusterSpec{
				Type:       "ceph",
				Management: v1alpha1.StorageClusterManagementManaged,
				Ceph: &v1alpha1.StorageClusterCephSpec{
					Topology: v1alpha1.StorageCephTopology{
						Nodes: []v1alpha1.StorageCephNode{{
							MachineRef: v1alpha1.LocalObjectReference{Name: "ceph-0"},
						}},
					},
				},
			},
		}},
	}

	services := infraComponentServicesVars(state)
	if len(services) != 1 {
		t.Fatalf("infra component services = %d, want 1: %+v", len(services), services)
	}
	service := services[0].(map[string]any)
	if service["kind"] != v1alpha1.ComponentSlotNameResolution {
		t.Fatalf("service kind = %v, want %v", service["kind"], v1alpha1.ComponentSlotNameResolution)
	}
	if service["machineRef"] != "bastion" {
		t.Fatalf("service machineRef = %v, want bastion", service["machineRef"])
	}
	want := []any{"1.1.1.1", "9.9.9.9"}
	if got := service["forwarders"]; !reflect.DeepEqual(got, want) {
		t.Fatalf("forwarders = %v, want %v", got, want)
	}
}

func TestInfraComponentServicesVarsUsesGraphEntryConsumersForSharedDNS(t *testing.T) {
	state := dnsRecordsState()
	state.Environments[0].Spec.InfraComponents.NameResolution = append(state.Environments[0].Spec.InfraComponents.NameResolution, v1alpha1.EnvironmentNameResolutionComponent{
		Name:                   "alternate",
		Management:             v1alpha1.EnvironmentComponentManaged,
		ComponentRef:           v1alpha1.LocalObjectReference{Name: "dns"},
		AdditionalIngressHosts: []string{"console-openshift-console.apps.cluster-b.example.test"},
	})
	state.NetworkConfigs = append(state.NetworkConfigs, state.NetworkConfigs[0])
	state.NetworkConfigs[1].Metadata.Name = "managed-net-b"
	state.NetworkConfigs[1].Spec.NameResolutionRefs = []v1alpha1.LocalObjectReference{{Name: "alternate"}}
	state.ContainerClusters = append(state.ContainerClusters, state.ContainerClusters[0])
	state.ContainerClusters[1].Metadata.Name = "cluster-b"
	state.ContainerClusters[1].Spec.Install.Endpoints = map[string]v1alpha1.Endpoint{
		v1alpha1.EndpointAPI:     {Address: "192.168.131.10"},
		v1alpha1.EndpointAPIInt:  {Address: "192.168.131.10"},
		v1alpha1.EndpointIngress: {Address: "192.168.131.11"},
	}
	state.ContainerClusters[1].Spec.Nodes = append([]v1alpha1.OCPNodeSpec(nil), state.ContainerClusters[1].Spec.Nodes...)
	state.ContainerClusters[1].Spec.Nodes[0].MachineRef.Name = "master-b"
	state.Machines = append(state.Machines, state.Machines[1])
	state.Machines[2].Metadata.Name = "master-b"
	state.Machines[2].Spec.Network.Config.NetworkConfigRef.Name = "managed-net-b"

	services := infraComponentServicesVars(state)
	if len(services) != 1 {
		t.Fatalf("infra component services = %d, want 1: %+v", len(services), services)
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
