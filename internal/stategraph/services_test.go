package stategraph

import (
	"reflect"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
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

func sharedManagedServiceState() v1alpha1.State {
	return v1alpha1.State{
		Environments: []v1alpha1.Environment{{
			Metadata: v1alpha1.Metadata{Name: "env"},
			Spec: v1alpha1.EnvironmentSpec{
				BaseDomain: "example.test",
				ProxyFor: v1alpha1.EnvironmentProxyForSpec{
					Bootwright:     "default",
					ClusterInstall: "default",
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
					ArtifactServers: []v1alpha1.EnvironmentArtifactServerComponent{{
						Name:         "default",
						Type:         v1alpha1.EnvironmentComponentManaged,
						ComponentRef: v1alpha1.LocalObjectReference{Name: "artifact-server"},
						Routes: v1alpha1.EnvironmentArtifactRoutes{
							ClusterInstall: v1alpha1.EnvironmentArtifactRoute{Endpoint: "cluster"},
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

func networkConfig(name string) v1alpha1.NetworkConfig {
	return v1alpha1.NetworkConfig{
		Metadata: v1alpha1.Metadata{Name: name},
		Spec: v1alpha1.NetworkConfigSpec{
			Template: v1alpha1.NetworkConfigTemplate{DNSRefs: []string{"default"}},
		},
	}
}

func clusterInfra(name, machineProvider, networkName string) v1alpha1.ClusterInfra {
	return v1alpha1.ClusterInfra{
		Metadata: v1alpha1.Metadata{Name: name},
		Spec: v1alpha1.ClusterInfraSpec{
			Endpoints: map[string]v1alpha1.Endpoint{
				v1alpha1.EndpointAPI: {
					ProvidedBy: &v1alpha1.EndpointProvidedBy{ComponentRef: v1alpha1.LocalObjectReference{Name: "load-balancer"}, Address: "api"},
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
			Install: v1alpha1.OCPInstallSpec{Mode: v1alpha1.InstallModeDisconnected},
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
				AddressName: "ssh",
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
