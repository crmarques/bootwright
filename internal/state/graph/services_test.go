package stategraph

import (
	"reflect"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	extensionplan "github.com/crmarques/bootwright/internal/addons/plan"
)

func TestSharedDestroyConflictsDetectsManagedInfraComponents(t *testing.T) {
	state := sharedManagedServiceState()

	conflicts := SharedDestroyConflicts(state, []string{"cluster-a"})
	got := map[string]DestroyScopeConflict{}
	for _, conflict := range conflicts {
		got[conflict.Slot] = conflict
	}
	for _, slot := range []string{
		v1alpha1.ComponentSlotArtifactServer,
		v1alpha1.ComponentSlotLoadBalancer,
		v1alpha1.ComponentSlotNameResolution,
		v1alpha1.ComponentSlotNTP,
		v1alpha1.ComponentSlotProxy,
		v1alpha1.ComponentSlotRegistry,
	} {
		conflict, ok := got[slot]
		if !ok {
			t.Fatalf("missing shared %s conflict in %#v", slot, conflicts)
		}
		if conflict.Provider != v1alpha1.KindInfraComponent {
			t.Fatalf("%s provider = %q", slot, conflict.Provider)
		}
		if !reflect.DeepEqual(conflict.ScopedClusters, []string{"cluster-a"}) {
			t.Fatalf("%s scoped clusters = %#v", slot, conflict.ScopedClusters)
		}
		if !reflect.DeepEqual(conflict.UnscopedClusters, []string{"cluster-b"}) {
			t.Fatalf("%s unscoped clusters = %#v", slot, conflict.UnscopedClusters)
		}
	}
}

func TestMachineServiceGraphIncludesSharedBMCServices(t *testing.T) {
	state := sharedBMCServiceState()
	graph := ResolveMachineServices(state)

	var bmc *MachineService
	for i := range graph.Services {
		if graph.Services[i].Identity.Kind == v1alpha1.ProviderServiceKindBMC {
			bmc = &graph.Services[i]
			break
		}
	}
	if bmc == nil {
		t.Fatalf("missing BMC provider service in %#v", graph.Services)
	}
	if bmc.Identity.ProviderName != "libvirt-provider" || bmc.Identity.Name != "emulated" {
		t.Fatalf("BMC identity = %#v", bmc.Identity)
	}
	if bmc.MachineRef != "libvirt-host" {
		t.Fatalf("BMC machineRef = %q, want libvirt-host", bmc.MachineRef)
	}
	if got := bmc.ConsumerClusters(); !reflect.DeepEqual(got, []string{"cluster-a", "cluster-b"}) {
		t.Fatalf("BMC consumers = %v", got)
	}

	conflicts := SharedDestroyConflicts(state, []string{"cluster-a"})
	found := false
	for _, conflict := range conflicts {
		if conflict.Slot == v1alpha1.ProviderServiceKindBMC {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing BMC scope conflict in %#v", conflicts)
	}
}

func TestMachineServiceGraphKeepsBMCServicesPerHost(t *testing.T) {
	state := sharedBMCServiceState()
	for i := range state.Machines {
		if state.Machines[i].Metadata.Name == "cluster-b-master-0" {
			state.Machines[i] = libvirtNodeMachine("cluster-b-master-0", "libvirt-provider-b")
		}
	}
	state.Machines = append(state.Machines, libvirtHostMachine("libvirt-host-b", "10.0.0.7"))
	state.InfraProviders = append(state.InfraProviders, libvirtProvider("libvirt-provider-b", "libvirt-host-b"))
	state.ContainerClusters[1].Spec.Nodes[0].MachineRef.Name = "cluster-b-master-0"

	var machineRefs []string
	for _, service := range ResolveMachineServices(state).Services {
		if service.Identity.Kind == v1alpha1.ProviderServiceKindBMC {
			machineRefs = append(machineRefs, service.MachineRef)
		}
	}
	want := []string{"libvirt-host", "libvirt-host-b"}
	if !reflect.DeepEqual(machineRefs, want) {
		t.Fatalf("BMC machineRefs = %v, want %v", machineRefs, want)
	}
}

func TestMachineServiceGraphMergesAdditionalIngressHosts(t *testing.T) {
	state := sharedManagedServiceState()
	state.Environments[0].Spec.InfraComponents.NameResolution[0].AdditionalIngressHosts = []string{"env.example.test"}
	state.InfraComponents[1].Spec.NameResolution.AdditionalIngressHosts = []string{"component.example.test"}

	got := ResolveMachineServices(state).MergedStringField(MachineServiceIdentity{
		Kind:         v1alpha1.ComponentSlotNameResolution,
		ProviderName: v1alpha1.KindInfraComponent,
		Name:         "name-resolution",
	}, "additionalIngressHosts")
	want := []string{"component.example.test", "env.example.test"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("additionalIngressHosts = %v, want %v", got, want)
	}
}

func TestSharedServicesReportsContainerClusterConsumers(t *testing.T) {
	groups := ResolveMachineServices(sharedManagedServiceState()).SharedServices()
	seen := false
	for _, group := range groups {
		if group.Kind != v1alpha1.ComponentSlotNameResolution {
			continue
		}
		seen = true
		want := []string{"cluster-a", "cluster-b"}
		if !reflect.DeepEqual(group.ConsumingClusters, want) {
			t.Fatalf("ConsumingClusters = %v, want %v", group.ConsumingClusters, want)
		}
	}
	if !seen {
		t.Fatalf("nameResolution shared group not found: %#v", groups)
	}
}

func TestFilterStateToClustersKeepsReferencedProviders(t *testing.T) {
	state := sharedManagedServiceState()

	filtered := FilterStateToClusters(state, []string{"cluster-a"})
	if got := namesOfClusters(filtered.ContainerClusters); !reflect.DeepEqual(got, []string{"cluster-a"}) {
		t.Fatalf("clusters = %#v", got)
	}
	if got := namesOfMachines(filtered.Machines); !reflect.DeepEqual(got, []string{"service-host", "cluster-a-master-0"}) {
		t.Fatalf("machines = %#v", got)
	}
	if got := namesOfProviders(filtered.InfraProviders); !reflect.DeepEqual(got, []string{"machines-a"}) {
		t.Fatalf("providers = %#v", got)
	}
}

func TestFilterStateToClustersExcludesUnconsumedInfraComponentMachines(t *testing.T) {
	state := sharedManagedServiceState()
	state.Machines = append(state.Machines, v1alpha1.Machine{
		Metadata: v1alpha1.Metadata{Name: "unused-service-host"},
		Spec: v1alpha1.MachineSpec{
			Capabilities: []string{v1alpha1.MachineCapabilityContainerRuntime, v1alpha1.MachineCapabilityProxy},
			OS:           v1alpha1.MachineOSSpec{Provided: v1alpha1.BoolPtr(true)},
		},
	})
	state.InfraComponents = append(state.InfraComponents, v1alpha1.InfraComponent{
		Metadata: v1alpha1.Metadata{Name: "unused-proxy"},
		Spec: v1alpha1.InfraComponentSpec{
			Type: v1alpha1.ComponentSlotProxy, Proxy: &v1alpha1.ProxyComponent{
				Implementation: v1alpha1.InfraComponentTypeSquid,
				MachineRef:     v1alpha1.LocalObjectReference{Name: "unused-service-host"},
				Port:           v1alpha1.DefaultSquidPort,
			}},
	})

	filtered := FilterStateToClusters(state, []string{"cluster-a"})
	if got := namesOfMachines(filtered.Machines); !reflect.DeepEqual(got, []string{"service-host", "cluster-a-master-0"}) {
		t.Fatalf("machines = %#v, want only selected cluster and consumed service machines", got)
	}
}

func TestFilterStateToClustersKeepsRelevantExtensionBindings(t *testing.T) {
	state := sharedManagedServiceState()
	state.ClusterAddons = []v1alpha1.ClusterAddon{
		{Metadata: v1alpha1.Metadata{Name: "base"}},
		{Metadata: v1alpha1.Metadata{Name: "console"}},
		{Metadata: v1alpha1.Metadata{Name: "unused"}},
	}
	state.ClusterAddonProfiles = []v1alpha1.ClusterAddonProfile{{
		Metadata: v1alpha1.Metadata{Name: "base-platform"},
		Spec: v1alpha1.ClusterAddonProfileSpec{
			Addons: []v1alpha1.LocalObjectReference{{Name: "base"}},
		},
	}}
	state.ClusterAddonBindings = []v1alpha1.ClusterAddonBinding{
		{
			Metadata: v1alpha1.Metadata{Name: "cluster-a-addons"},
			Spec: v1alpha1.ClusterAddonBindingSpec{
				ClusterRef:    v1alpha1.LocalObjectReference{Name: "cluster-a"},
				AddonProfiles: []v1alpha1.LocalObjectReference{{Name: "base-platform"}},
				Addons:        []v1alpha1.ClusterAddonBindingAddon{{Name: "console"}},
			},
		},
		{
			Metadata: v1alpha1.Metadata{Name: "cluster-b-addons"},
			Spec: v1alpha1.ClusterAddonBindingSpec{
				ClusterRef: v1alpha1.LocalObjectReference{Name: "cluster-b"},
				Addons:     []v1alpha1.ClusterAddonBindingAddon{{Name: "unused"}},
			},
		},
	}

	filtered := FilterStateToClusters(state, []string{"cluster-a"})
	if got := len(filtered.ClusterAddonBindings); got != 1 {
		t.Fatalf("bindings = %d, want 1", got)
	}
	if got := filtered.ClusterAddonBindings[0].Spec.ClusterRef.Name; got != "cluster-a" {
		t.Fatalf("binding selected cluster = %v, want cluster-a", got)
	}
	if got := namesOfProfiles(filtered.ClusterAddonProfiles); !reflect.DeepEqual(got, []string{"base-platform"}) {
		t.Fatalf("extension sets = %v, want [base-platform]", got)
	}
	if got := namesOfAddons(filtered.ClusterAddons); !reflect.DeepEqual(got, []string{"base", "console"}) {
		t.Fatalf("addons = %v, want [base console]", got)
	}
	plans, err := extensionplan.BindingPlans(filtered)
	if err != nil {
		t.Fatalf("BindingPlans: %v", err)
	}
	if len(plans) != 1 || plans[0].Cluster != "cluster-a" {
		t.Fatalf("plans = %+v, want one plan for cluster-a", plans)
	}
}

func sharedManagedServiceState() v1alpha1.State {
	return v1alpha1.State{
		Environments: []v1alpha1.Environment{{
			Metadata: v1alpha1.Metadata{Name: "env"},
			Spec: v1alpha1.EnvironmentSpec{
				BaseDomain: "example.test",
				ProxyFor: v1alpha1.EnvironmentProxyForSpec{
					Bootwright:              "default",
					ContainerClusterInstall: "default",
				},
				InfraComponents: v1alpha1.EnvironmentInfraComponentsSpec{
					Proxies: []v1alpha1.EnvironmentProxyComponent{{
						Name:         "default",
						Type:         v1alpha1.EnvironmentComponentManaged,
						ComponentRef: v1alpha1.LocalObjectReference{Name: "proxy"},
					}},
					NameResolution: []v1alpha1.EnvironmentNameResolutionComponent{{
						Name:         "default",
						Type:         v1alpha1.EnvironmentComponentManaged,
						ComponentRef: v1alpha1.LocalObjectReference{Name: "name-resolution"},
					}},
					NTPSources: []v1alpha1.EnvironmentNTPSourceComponent{{
						Name:         "default",
						Type:         v1alpha1.EnvironmentComponentManaged,
						ComponentRef: v1alpha1.LocalObjectReference{Name: "ntp-server"},
					}},
					ArtifactServers: []v1alpha1.EnvironmentArtifactServerComponent{{
						Name:         "default",
						Type:         v1alpha1.EnvironmentComponentManaged,
						ComponentRef: v1alpha1.LocalObjectReference{Name: "artifact-server"},
					}},
					Registries: []v1alpha1.EnvironmentRegistryComponent{{
						Name:         "default",
						Type:         v1alpha1.EnvironmentComponentManaged,
						ComponentRef: v1alpha1.LocalObjectReference{Name: "registry"},
					}},
				},
			},
		}},
		Machines: []v1alpha1.Machine{
			serviceMachine(),
			rawMachine("cluster-a-master-0", "machines-a", "net-a"),
			rawMachine("cluster-b-master-0", "machines-b", "net-b"),
		},
		NetworkConfigs: []v1alpha1.NetworkConfig{networkConfig("net-a"), networkConfig("net-b")},
		InfraProviders: []v1alpha1.InfraProvider{
			bareMetalProvider("machines-a"),
			bareMetalProvider("machines-b"),
		},
		InfraComponents: []v1alpha1.InfraComponent{
			loadBalancerComponent(),
			nameResolutionComponent(),
			ntpComponent(),
			proxyComponent(),
			registryComponent(),
			artifactServerComponent(),
		},
		ContainerClusters: []v1alpha1.ContainerCluster{
			containerCluster("cluster-a", "cluster-a-master-0"),
			containerCluster("cluster-b", "cluster-b-master-0"),
		},
	}
}

func sharedBMCServiceState() v1alpha1.State {
	return v1alpha1.State{
		Machines: []v1alpha1.Machine{
			libvirtHostMachine("libvirt-host", "10.0.0.6"),
			libvirtNodeMachine("cluster-a-master-0", "libvirt-provider"),
			libvirtNodeMachine("cluster-b-master-0", "libvirt-provider"),
		},
		InfraProviders: []v1alpha1.InfraProvider{libvirtProvider("libvirt-provider", "libvirt-host")},
		ContainerClusters: []v1alpha1.ContainerCluster{
			containerCluster("cluster-a", "cluster-a-master-0"),
			containerCluster("cluster-b", "cluster-b-master-0"),
		},
	}
}

func libvirtHostMachine(name, address string) v1alpha1.Machine {
	return v1alpha1.Machine{
		Metadata: v1alpha1.Metadata{Name: name},
		Spec: v1alpha1.MachineSpec{
			Capabilities: []string{v1alpha1.MachineCapabilityLibvirt},
			OS: v1alpha1.MachineOSSpec{
				Provided: v1alpha1.BoolPtr(true),
			},
			Addresses: []v1alpha1.MachineAddress{{Name: "ssh", Address: address}},
			Access: v1alpha1.MachineAccess{
				SSH: &v1alpha1.MachineSSHSpec{AddressRef: v1alpha1.LocalObjectReference{Name: "ssh"}},
			},
		},
	}
}

func libvirtNodeMachine(name, provider string) v1alpha1.Machine {
	return v1alpha1.Machine{
		Metadata: v1alpha1.Metadata{Name: name},
		Spec: v1alpha1.MachineSpec{
			Substrate: v1alpha1.MachineSubstrate{
				ProviderRef: v1alpha1.LocalObjectReference{Name: provider},
				ProfileRef:  v1alpha1.LocalObjectReference{Name: "libvirt-profile"},
			},
			OS: v1alpha1.MachineOSSpec{
				Provided: v1alpha1.BoolPtr(false),
			},
		},
	}
}

func libvirtProvider(name, machine string) v1alpha1.InfraProvider {
	return v1alpha1.InfraProvider{
		Metadata: v1alpha1.Metadata{Name: name},
		Spec: v1alpha1.InfraProviderSpec{
			Type: v1alpha1.ProvisionerLibvirt,
			Libvirt: &v1alpha1.InfraProviderLibvirt{
				MachineRef: v1alpha1.LocalObjectReference{Name: machine},
				URI:        "qemu:///system",
				BMCEmulationDefaults: &v1alpha1.BMCEmulationDefaults{
					Auth: &v1alpha1.BMCAuth{CredentialsRef: v1alpha1.SecretRef{Name: "bmc-credentials"}},
				},
				MachineProfiles: []v1alpha1.MachineProfile{{Name: "libvirt-profile"}},
			},
		},
	}
}

func networkConfig(name string) v1alpha1.NetworkConfig {
	return v1alpha1.NetworkConfig{
		Metadata: v1alpha1.Metadata{Name: name},
		Spec: v1alpha1.NetworkConfigSpec{
			DNSRefs: []string{"default"},
		},
	}
}

func serviceMachine() v1alpha1.Machine {
	return v1alpha1.Machine{
		Metadata: v1alpha1.Metadata{Name: "service-host"},
		Spec: v1alpha1.MachineSpec{
			Capabilities: []string{
				v1alpha1.MachineCapabilityContainerRuntime,
				v1alpha1.MachineCapabilityLoadBalancer,
				v1alpha1.MachineCapabilityNameResolution,
				v1alpha1.MachineCapabilityNTP,
				v1alpha1.MachineCapabilityProxy,
				v1alpha1.MachineCapabilityRegistry,
				v1alpha1.MachineCapabilityArtifactServer,
			},
			OS: v1alpha1.MachineOSSpec{
				Provided: v1alpha1.BoolPtr(true),
			},
			Addresses: []v1alpha1.MachineAddress{{Name: "ssh", Address: "10.0.0.5"}},
			Access: v1alpha1.MachineAccess{
				SSH: &v1alpha1.MachineSSHSpec{AddressRef: v1alpha1.LocalObjectReference{Name: "ssh"}},
			},
		},
	}
}

func rawMachine(name, provider, networkName string) v1alpha1.Machine {
	return v1alpha1.Machine{
		Metadata: v1alpha1.Metadata{Name: name},
		Spec: v1alpha1.MachineSpec{
			Substrate: v1alpha1.MachineSubstrate{
				ProviderRef: v1alpha1.LocalObjectReference{Name: provider},
			},
			Hardware: v1alpha1.MachineHardware{
				NICs: []v1alpha1.MachineNIC{{Name: "primary", MACAddress: "52:54:00:00:00:01"}},
				Boot: v1alpha1.MachineHardwareBoot{NICRef: v1alpha1.LocalObjectReference{Name: "primary"}},
			},
			OS: v1alpha1.MachineOSSpec{
				Provided: v1alpha1.BoolPtr(false),
			},
			Network: v1alpha1.MachineNetwork{
				Config: v1alpha1.MachineNetworkConfig{
					NetworkConfigRef: v1alpha1.LocalObjectReference{Name: networkName},
				},
			},
		},
	}
}

func bareMetalProvider(name string) v1alpha1.InfraProvider {
	return v1alpha1.InfraProvider{
		Metadata: v1alpha1.Metadata{Name: name},
		Spec: v1alpha1.InfraProviderSpec{
			Type:      v1alpha1.ProvisionerBareMetal,
			BareMetal: &v1alpha1.InfraProviderBareMetal{},
		},
	}
}

func containerCluster(name, machineName string) v1alpha1.ContainerCluster {
	return v1alpha1.ContainerCluster{
		Metadata: v1alpha1.Metadata{Name: name},
		Spec: v1alpha1.ContainerClusterSpec{
			Install: v1alpha1.OCPInstallSpec{
				Mode: v1alpha1.InstallModeDisconnected,
				ArtifactAccess: v1alpha1.ClusterArtifactAccess{
					ServerRef: v1alpha1.LocalObjectReference{Name: "default"},
					ContainerClusterInstall: v1alpha1.ClusterArtifactEndpointRef{
						EndpointRef: v1alpha1.LocalObjectReference{Name: "cluster"},
					},
				},
				Endpoints: map[string]v1alpha1.Endpoint{
					v1alpha1.EndpointAPI: {
						Source: v1alpha1.EndpointSource{
							Type:         v1alpha1.EndpointSourceInfraComponent,
							ComponentRef: v1alpha1.LocalObjectReference{Name: "load-balancer"},
							BindAddress:  "api",
						},
					},
				},
			},
			Nodes: []v1alpha1.OCPNodeSpec{{
				Hostname:   "master-0",
				Role:       "master",
				MachineRef: v1alpha1.LocalObjectReference{Name: machineName},
			}},
		},
	}
}

func loadBalancerComponent() v1alpha1.InfraComponent {
	return v1alpha1.InfraComponent{
		Metadata: v1alpha1.Metadata{Name: "load-balancer"},
		Spec: v1alpha1.InfraComponentSpec{
			Type: v1alpha1.ComponentSlotLoadBalancer, LoadBalancer: &v1alpha1.LoadBalancerComponent{
				Implementation: v1alpha1.InfraComponentTypeHAProxy,
				MachineRef:     v1alpha1.LocalObjectReference{Name: "service-host"},
				BindAddresses: []v1alpha1.LoadBalancerBindAddress{{
					Name: "api",
					IP:   "10.0.0.10",
				}},
			}},
	}
}

func nameResolutionComponent() v1alpha1.InfraComponent {
	return v1alpha1.InfraComponent{
		Metadata: v1alpha1.Metadata{Name: "name-resolution"},
		Spec: v1alpha1.InfraComponentSpec{
			Type: v1alpha1.ComponentSlotNameResolution, NameResolution: &v1alpha1.NameResolutionComponent{
				Implementation: v1alpha1.InfraComponentTypeDnsmasq,
				MachineRef:     v1alpha1.LocalObjectReference{Name: "service-host"},
				BindAddress:    "10.0.0.5",
				Port:           v1alpha1.DefaultDNSPort,
			}},
	}
}

func ntpComponent() v1alpha1.InfraComponent {
	return v1alpha1.InfraComponent{
		Metadata: v1alpha1.Metadata{Name: "ntp-server"},
		Spec: v1alpha1.InfraComponentSpec{
			Type: v1alpha1.ComponentSlotNTP, NTP: &v1alpha1.NTPComponent{
				Implementation: v1alpha1.InfraComponentTypeChrony,
				MachineRef:     v1alpha1.LocalObjectReference{Name: "service-host"},
				BindAddress:    "10.0.0.5",
				Port:           v1alpha1.DefaultNTPPort,
			}},
	}
}

func proxyComponent() v1alpha1.InfraComponent {
	return v1alpha1.InfraComponent{
		Metadata: v1alpha1.Metadata{Name: "proxy"},
		Spec: v1alpha1.InfraComponentSpec{
			Type: v1alpha1.ComponentSlotProxy, Proxy: &v1alpha1.ProxyComponent{
				Implementation: v1alpha1.InfraComponentTypeSquid,
				MachineRef:     v1alpha1.LocalObjectReference{Name: "service-host"},
				Port:           v1alpha1.DefaultSquidPort,
			}},
	}
}

func registryComponent() v1alpha1.InfraComponent {
	return v1alpha1.InfraComponent{
		Metadata: v1alpha1.Metadata{Name: "registry"},
		Spec: v1alpha1.InfraComponentSpec{
			Type: v1alpha1.ComponentSlotRegistry, Registry: &v1alpha1.RegistryComponent{
				Implementation: v1alpha1.InfraComponentTypeMirrorRegistry,
				MachineRef:     v1alpha1.LocalObjectReference{Name: "service-host"},
				Port:           v1alpha1.DefaultMirrorRegistryPort,
			}},
	}
}

func artifactServerComponent() v1alpha1.InfraComponent {
	return v1alpha1.InfraComponent{
		Metadata: v1alpha1.Metadata{Name: "artifact-server"},
		Spec: v1alpha1.InfraComponentSpec{
			Type: v1alpha1.ComponentSlotArtifactServer, ArtifactServer: &v1alpha1.ArtifactServerComponent{
				MachineRef: v1alpha1.LocalObjectReference{Name: "service-host"},
				Listeners: []v1alpha1.ArtifactServerListener{{
					Name:     "https",
					Protocol: v1alpha1.ArtifactServerProtocolHTTPS,
					Port:     v1alpha1.DefaultArtifactsHTTPPort,
				}},
				Endpoints: []v1alpha1.ArtifactServerEndpoint{{
					Name:           "cluster",
					Listener:       "https",
					MachineAddress: "ssh",
				}},
			}},
	}
}

func namesOfClusters(clusters []v1alpha1.ContainerCluster) []string {
	out := make([]string, 0, len(clusters))
	for _, cluster := range clusters {
		out = append(out, cluster.Metadata.Name)
	}
	return out
}

func namesOfMachines(machines []v1alpha1.Machine) []string {
	out := make([]string, 0, len(machines))
	for _, machine := range machines {
		out = append(out, machine.Metadata.Name)
	}
	return out
}

func namesOfProviders(providers []v1alpha1.InfraProvider) []string {
	out := make([]string, 0, len(providers))
	for _, provider := range providers {
		out = append(out, provider.Metadata.Name)
	}
	return out
}

func namesOfProfiles(sets []v1alpha1.ClusterAddonProfile) []string {
	out := make([]string, 0, len(sets))
	for _, set := range sets {
		out = append(out, set.Metadata.Name)
	}
	return out
}

func namesOfAddons(addons []v1alpha1.ClusterAddon) []string {
	out := make([]string, 0, len(addons))
	for _, extension := range addons {
		out = append(out, extension.Metadata.Name)
	}
	return out
}
