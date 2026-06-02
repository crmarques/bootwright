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
		v1alpha1.ComponentSlotArtifacts,
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

func TestProviderServiceGraphIncludesSharedBMCServices(t *testing.T) {
	state := sharedBMCServiceState()
	graph := ResolveProviderServices(state)

	var bmc *ProviderService
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
	if bmc.HostRef != "libvirt-host" {
		t.Fatalf("BMC hostRef = %q, want libvirt-host", bmc.HostRef)
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

func TestProviderServiceGraphKeepsBMCServicesPerHost(t *testing.T) {
	state := sharedBMCServiceState()
	state.Hosts = append(state.Hosts, v1alpha1.Host{
		Metadata: v1alpha1.Metadata{Name: "libvirt-host-b"},
		Spec: v1alpha1.HostSpec{
			Addresses:    []v1alpha1.HostAddress{{Name: "ssh", Address: "10.0.0.7"}},
			SSH:          &v1alpha1.HostSSHSpec{AddressName: "ssh"},
			Capabilities: []string{v1alpha1.HostCapabilityLibvirt},
		},
	})
	state.InfraProviders[0].Spec.MachineProfiles = append(state.InfraProviders[0].Spec.MachineProfiles, v1alpha1.MachineProfileCapability{
		Name: "libvirt-profile-b",
		Libvirt: &v1alpha1.MachineProfileLibvirtProvisioner{
			HostRef: v1alpha1.LocalObjectReference{Name: "libvirt-host-b"},
			URI:     "qemu:///system",
			BMCEmulationDefaults: &v1alpha1.BMCEmulationDefaults{
				Auth: &v1alpha1.BMCAuth{CredentialRef: v1alpha1.SecretRef{Name: "bmc-credentials"}},
			},
		},
	})
	state.ClusterInfras[1].Spec.Components.Machines[0].From.Profile = "libvirt-profile-b"

	var hostRefs []string
	for _, service := range ResolveProviderServices(state).Services {
		if service.Identity.Kind == v1alpha1.ProviderServiceKindBMC {
			hostRefs = append(hostRefs, service.HostRef)
		}
	}
	want := []string{"libvirt-host", "libvirt-host-b"}
	if !reflect.DeepEqual(hostRefs, want) {
		t.Fatalf("BMC hostRefs = %v, want %v", hostRefs, want)
	}
}

func TestProviderServiceGraphMergesAdditionalIngressHosts(t *testing.T) {
	state := sharedManagedServiceState()
	state.Environments[0].Spec.InfraComponents.NameResolution[0].AdditionalIngressHosts = []string{"env.example.test"}
	state.InfraComponents[1].Spec.NameResolution.AdditionalIngressHosts = []string{"component.example.test"}

	got := ResolveProviderServices(state).MergedStringField(ProviderServiceIdentity{
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
	groups := ResolveProviderServices(sharedManagedServiceState()).SharedServices()
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
	if got := namesOfInfras(filtered.ClusterInfras); !reflect.DeepEqual(got, []string{"infra-a"}) {
		t.Fatalf("infras = %#v", got)
	}
	if got := namesOfProviders(filtered.InfraProviders); !reflect.DeepEqual(got, []string{"machines-a"}) {
		t.Fatalf("providers = %#v", got)
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
						Routes: v1alpha1.EnvironmentArtifactRoutes{
							ContainerClusterInstall: v1alpha1.EnvironmentArtifactRoute{Endpoint: "cluster"},
						},
					}},
					Registries: []v1alpha1.EnvironmentRegistryComponent{{
						Name:         "default",
						Type:         v1alpha1.EnvironmentComponentManaged,
						ComponentRef: v1alpha1.LocalObjectReference{Name: "registry"},
					}},
				},
			},
		}},
		Hosts: []v1alpha1.Host{{
			Metadata: v1alpha1.Metadata{Name: "service-host"},
			Spec: v1alpha1.HostSpec{
				Addresses:    []v1alpha1.HostAddress{{Name: "ssh", Address: "10.0.0.5"}},
				SSH:          &v1alpha1.HostSSHSpec{AddressName: "ssh"},
				Capabilities: []string{v1alpha1.HostCapabilityContainerRuntime},
			},
		}},
		NetworkConfigs: []v1alpha1.NetworkConfig{networkConfig("net-a"), networkConfig("net-b")},
		InfraProviders: []v1alpha1.InfraProvider{
			{Metadata: v1alpha1.Metadata{Name: "machines-a"}},
			{Metadata: v1alpha1.Metadata{Name: "machines-b"}},
		},
		InfraComponents: []v1alpha1.InfraComponent{
			loadBalancerComponent(),
			nameResolutionComponent(),
			ntpComponent(),
			proxyComponent(),
			registryComponent(),
			artifactServerComponent(),
		},
		ClusterInfras: []v1alpha1.ClusterInfra{
			clusterInfra("infra-a", "machines-a", "net-a"),
			clusterInfra("infra-b", "machines-b", "net-b"),
		},
		ContainerClusters: []v1alpha1.ContainerCluster{
			containerCluster("cluster-a", "infra-a"),
			containerCluster("cluster-b", "infra-b"),
		},
	}
}

func sharedBMCServiceState() v1alpha1.State {
	return v1alpha1.State{
		Hosts: []v1alpha1.Host{{
			Metadata: v1alpha1.Metadata{Name: "libvirt-host"},
			Spec: v1alpha1.HostSpec{
				Addresses:    []v1alpha1.HostAddress{{Name: "ssh", Address: "10.0.0.6"}},
				SSH:          &v1alpha1.HostSSHSpec{AddressName: "ssh"},
				Capabilities: []string{v1alpha1.HostCapabilityLibvirt},
			},
		}},
		InfraProviders: []v1alpha1.InfraProvider{{
			Metadata: v1alpha1.Metadata{Name: "libvirt-provider"},
			Spec: v1alpha1.InfraProviderSpec{MachineProfiles: []v1alpha1.MachineProfileCapability{{
				Name: "libvirt-profile",
				Libvirt: &v1alpha1.MachineProfileLibvirtProvisioner{
					HostRef: v1alpha1.LocalObjectReference{Name: "libvirt-host"},
					URI:     "qemu:///system",
					BMCEmulationDefaults: &v1alpha1.BMCEmulationDefaults{
						Auth: &v1alpha1.BMCAuth{CredentialRef: v1alpha1.SecretRef{Name: "bmc-credentials"}},
					},
				},
			}}},
		}},
		ClusterInfras: []v1alpha1.ClusterInfra{
			bmcClusterInfra("infra-a"),
			bmcClusterInfra("infra-b"),
		},
		ContainerClusters: []v1alpha1.ContainerCluster{
			containerCluster("cluster-a", "infra-a"),
			containerCluster("cluster-b", "infra-b"),
		},
	}
}

func bmcClusterInfra(name string) v1alpha1.ClusterInfra {
	return v1alpha1.ClusterInfra{
		Metadata: v1alpha1.Metadata{Name: name},
		Spec: v1alpha1.ClusterInfraSpec{
			Components: v1alpha1.ClusterComponents{Machines: []v1alpha1.ClusterMachineComponent{{
				Name: "master-0",
				From: v1alpha1.From{Provider: "libvirt-provider", Profile: "libvirt-profile"},
			}}},
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

func clusterInfra(name, machineProvider, networkName string) v1alpha1.ClusterInfra {
	return v1alpha1.ClusterInfra{
		Metadata: v1alpha1.Metadata{Name: name},
		Spec: v1alpha1.ClusterInfraSpec{
			Endpoints: map[string]v1alpha1.Endpoint{
				v1alpha1.EndpointAPI: {
					Source: v1alpha1.EndpointSource{
						Type:         v1alpha1.EndpointSourceInfraComponent,
						ComponentRef: v1alpha1.LocalObjectReference{Name: "load-balancer"},
						BindAddress:  "api",
					},
				},
			},
			Components: v1alpha1.ClusterComponents{Machines: []v1alpha1.ClusterMachineComponent{{
				Name: "master-0",
				From: v1alpha1.From{Provider: machineProvider, Name: "node-0"},
				NetworkConfig: v1alpha1.ClusterMachineNetworkConfig{
					Ref: v1alpha1.LocalObjectReference{Name: networkName},
				},
			}}},
		},
	}
}

func containerCluster(name, infraName string) v1alpha1.ContainerCluster {
	return v1alpha1.ContainerCluster{
		Metadata: v1alpha1.Metadata{Name: name},
		Spec: v1alpha1.ContainerClusterSpec{
			Install: v1alpha1.OCPInstallSpec{
				Mode: v1alpha1.InstallModeDisconnected,
				EndpointRefs: v1alpha1.ContainerEndpointRefs{
					API: v1alpha1.EndpointRef{Name: v1alpha1.EndpointAPI},
				},
			},
			Nodes: []v1alpha1.OCPNodeSpec{{
				Hostname: "master-0",
				Role:     "master",
				MachineRef: v1alpha1.NodeMachineRef{
					ClusterInfra: infraName,
					Name:         "master-0",
				},
			}},
		},
	}
}

func loadBalancerComponent() v1alpha1.InfraComponent {
	return v1alpha1.InfraComponent{
		Metadata: v1alpha1.Metadata{Name: "load-balancer"},
		Spec: v1alpha1.InfraComponentSpec{LoadBalancer: &v1alpha1.LoadBalancerComponent{
			Type:    v1alpha1.InfraComponentTypeHAProxy,
			HostRef: v1alpha1.LocalObjectReference{Name: "service-host"},
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
		Spec: v1alpha1.InfraComponentSpec{NameResolution: &v1alpha1.NameResolutionComponent{
			Type:        v1alpha1.InfraComponentTypeDnsmasq,
			HostRef:     v1alpha1.LocalObjectReference{Name: "service-host"},
			BindAddress: "10.0.0.5",
			Port:        v1alpha1.DefaultDNSPort,
		}},
	}
}

func ntpComponent() v1alpha1.InfraComponent {
	return v1alpha1.InfraComponent{
		Metadata: v1alpha1.Metadata{Name: "ntp-server"},
		Spec: v1alpha1.InfraComponentSpec{NTP: &v1alpha1.NTPComponent{
			Type:        v1alpha1.InfraComponentTypeChrony,
			HostRef:     v1alpha1.LocalObjectReference{Name: "service-host"},
			BindAddress: "10.0.0.5",
			Port:        v1alpha1.DefaultNTPPort,
		}},
	}
}

func proxyComponent() v1alpha1.InfraComponent {
	return v1alpha1.InfraComponent{
		Metadata: v1alpha1.Metadata{Name: "proxy"},
		Spec: v1alpha1.InfraComponentSpec{Proxy: &v1alpha1.ProxyComponent{
			Type:    v1alpha1.InfraComponentTypeSquid,
			HostRef: v1alpha1.LocalObjectReference{Name: "service-host"},
			Port:    v1alpha1.DefaultSquidPort,
		}},
	}
}

func registryComponent() v1alpha1.InfraComponent {
	return v1alpha1.InfraComponent{
		Metadata: v1alpha1.Metadata{Name: "registry"},
		Spec: v1alpha1.InfraComponentSpec{Registry: &v1alpha1.RegistryComponent{
			Type:    v1alpha1.InfraComponentTypeMirrorRegistry,
			HostRef: v1alpha1.LocalObjectReference{Name: "service-host"},
			Port:    v1alpha1.DefaultMirrorRegistryPort,
		}},
	}
}

func artifactServerComponent() v1alpha1.InfraComponent {
	return v1alpha1.InfraComponent{
		Metadata: v1alpha1.Metadata{Name: "artifact-server"},
		Spec: v1alpha1.InfraComponentSpec{ArtifactServer: &v1alpha1.ArtifactServerComponent{
			HostRef: v1alpha1.LocalObjectReference{Name: "service-host"},
			Listeners: []v1alpha1.ArtifactServerListener{{
				Name:     "https",
				Protocol: v1alpha1.ArtifactServerProtocolHTTPS,
				Port:     v1alpha1.DefaultArtifactsHTTPPort,
			}},
			Endpoints: []v1alpha1.ArtifactServerEndpoint{{
				Name:        "cluster",
				Listener:    "https",
				HostAddress: "ssh",
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

func namesOfInfras(infras []v1alpha1.ClusterInfra) []string {
	out := make([]string, 0, len(infras))
	for _, infra := range infras {
		out = append(out, infra.Metadata.Name)
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
