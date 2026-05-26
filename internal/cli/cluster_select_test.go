package cli

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func TestValidateScopedApplySharedServicesFailsForInfraScope(t *testing.T) {
	state := cliStateWithSharedDNS()

	err := validateScopedApplySharedServices(state, "infra", "cluster-a")
	if err == nil {
		t.Fatal("expected shared service conflict, got nil")
	}
	if !strings.Contains(err.Error(), "--scope would narrow shared provider service") {
		t.Fatalf("error %q does not explain scoped apply conflict", err)
	}
	if !strings.Contains(err.Error(), "unscoped {cluster-b}") {
		t.Fatalf("error %q does not list unscoped consumer", err)
	}
}

func TestValidateScopedApplySharedServicesFailsForInfraAndAllSharedKinds(t *testing.T) {
	state := cliStateWithAllSharedProviderServices()
	for _, target := range []string{"infra", "all"} {
		t.Run(target, func(t *testing.T) {
			err := validateScopedApplySharedServices(state, target, "cluster-a")
			if err == nil {
				t.Fatal("expected shared service conflict, got nil")
			}
			for _, fragment := range []string{
				"artifacts InfraComponent/artifact-server",
				"loadBalancer InfraComponent/load-balancer",
				"nameResolution InfraComponent/name-resolution",
				"proxy InfraComponent/proxy",
				"registry InfraComponent/registry",
			} {
				if !strings.Contains(err.Error(), fragment) {
					t.Fatalf("%s error %q missing %q", target, err, fragment)
				}
			}
		})
	}
}

func TestValidateScopedApplySharedServicesAllowsClusterScope(t *testing.T) {
	if err := validateScopedApplySharedServices(cliStateWithSharedDNS(), "cluster", "cluster-a"); err != nil {
		t.Fatalf("cluster apply scope should not validate provider services: %v", err)
	}
}

func TestValidateScopedApplySharedServicesAllowsAllConsumers(t *testing.T) {
	if err := validateScopedApplySharedServices(cliStateWithSharedDNS(), "infra", "cluster-a,cluster-b"); err != nil {
		t.Fatalf("infra apply with all consumers should validate: %v", err)
	}
}

func cliStateWithSharedDNS() v1alpha1.State {
	return v1alpha1.State{
		Environments: []v1alpha1.Environment{{
			Metadata: v1alpha1.Metadata{Name: "env"},
			Spec: v1alpha1.EnvironmentSpec{
				BaseDomain: "example.test",
				InfraComponents: v1alpha1.EnvironmentInfraComponentsSpec{
					NameResolution: []v1alpha1.EnvironmentNameResolutionComponent{{
						Name:         "default",
						Type:         v1alpha1.EnvironmentComponentManaged,
						ComponentRef: v1alpha1.LocalObjectReference{Name: "name-resolution"},
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
		NetworkConfigs: []v1alpha1.NetworkConfig{
			cliNetworkConfig("net-a"),
			cliNetworkConfig("net-b"),
		},
		InfraProviders: []v1alpha1.InfraProvider{{
			Metadata: v1alpha1.Metadata{Name: "machines-a"},
		}, {
			Metadata: v1alpha1.Metadata{Name: "machines-b"},
		}},
		InfraComponents: []v1alpha1.InfraComponent{cliNameResolutionComponent()},
		ClusterInfras: []v1alpha1.ClusterInfra{
			cliClusterInfra("infra-a", "machines-a", "net-a", false),
			cliClusterInfra("infra-b", "machines-b", "net-b", false),
		},
		ContainerClusters: []v1alpha1.ContainerCluster{
			cliContainerCluster("cluster-a", "infra-a"),
			cliContainerCluster("cluster-b", "infra-b"),
		},
	}
}

func cliStateWithAllSharedProviderServices() v1alpha1.State {
	state := cliStateWithSharedDNS()
	state.Environments[0].Spec.ProxyFor = v1alpha1.EnvironmentProxyForSpec{
		Bootwright:     "default",
		ClusterInstall: "default",
	}
	state.Environments[0].Spec.InfraComponents.Proxies = []v1alpha1.EnvironmentProxyComponent{{
		Name:         "default",
		Type:         v1alpha1.EnvironmentComponentManaged,
		ComponentRef: v1alpha1.LocalObjectReference{Name: "proxy"},
	}}
	state.Environments[0].Spec.InfraComponents.ArtifactServers = []v1alpha1.EnvironmentArtifactServerComponent{{
		Name:         "default",
		Type:         v1alpha1.EnvironmentComponentManaged,
		ComponentRef: v1alpha1.LocalObjectReference{Name: "artifact-server"},
		Routes: v1alpha1.EnvironmentArtifactRoutes{
			ClusterInstall: v1alpha1.EnvironmentArtifactRoute{Endpoint: "cluster"},
		},
	}}
	state.Environments[0].Spec.InfraComponents.Registries = []v1alpha1.EnvironmentRegistryComponent{{
		Name:         "default",
		Type:         v1alpha1.EnvironmentComponentManaged,
		ComponentRef: v1alpha1.LocalObjectReference{Name: "registry"},
	}}
	state.InfraComponents = append(state.InfraComponents,
		cliLoadBalancerComponent(),
		cliProxyComponent(),
		cliRegistryComponent(),
		cliArtifactServerComponent(),
	)
	for i := range state.ClusterInfras {
		state.ClusterInfras[i].Spec.Endpoints = map[string]v1alpha1.Endpoint{
			v1alpha1.EndpointAPI: {
				ProvidedBy: &v1alpha1.EndpointProvidedBy{
					ComponentRef: v1alpha1.LocalObjectReference{Name: "load-balancer"},
					Address:      "api",
				},
			},
		}
	}
	for i := range state.ContainerClusters {
		state.ContainerClusters[i].Spec.Install.Mode = v1alpha1.InstallModeDisconnected
	}
	return state
}

func cliNetworkConfig(name string) v1alpha1.NetworkConfig {
	return v1alpha1.NetworkConfig{
		Metadata: v1alpha1.Metadata{Name: name},
		Spec: v1alpha1.NetworkConfigSpec{
			DNSRefs: []string{"default"},
		},
	}
}

func cliArtifactServerComponent() v1alpha1.InfraComponent {
	return v1alpha1.InfraComponent{
		Metadata: v1alpha1.Metadata{Name: "artifact-server"},
		Spec: v1alpha1.InfraComponentSpec{
			ArtifactServer: &v1alpha1.ArtifactServerComponent{
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
			},
		},
	}
}

func cliLoadBalancerComponent() v1alpha1.InfraComponent {
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

func cliNameResolutionComponent() v1alpha1.InfraComponent {
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

func cliProxyComponent() v1alpha1.InfraComponent {
	return v1alpha1.InfraComponent{
		Metadata: v1alpha1.Metadata{Name: "proxy"},
		Spec: v1alpha1.InfraComponentSpec{Proxy: &v1alpha1.ProxyComponent{
			Type:    v1alpha1.InfraComponentTypeSquid,
			HostRef: v1alpha1.LocalObjectReference{Name: "service-host"},
			Port:    v1alpha1.DefaultSquidPort,
		}},
	}
}

func cliRegistryComponent() v1alpha1.InfraComponent {
	return v1alpha1.InfraComponent{
		Metadata: v1alpha1.Metadata{Name: "registry"},
		Spec: v1alpha1.InfraComponentSpec{Registry: &v1alpha1.RegistryComponent{
			Type:    v1alpha1.InfraComponentTypeMirrorRegistry,
			HostRef: v1alpha1.LocalObjectReference{Name: "service-host"},
			Port:    v1alpha1.DefaultMirrorRegistryPort,
		}},
	}
}

func cliClusterInfra(name, machineProvider, networkName string, withEndpoints bool) v1alpha1.ClusterInfra {
	endpoints := map[string]v1alpha1.Endpoint{}
	if withEndpoints {
		endpoints[v1alpha1.EndpointAPI] = v1alpha1.Endpoint{
			ProvidedBy: &v1alpha1.EndpointProvidedBy{
				ComponentRef: v1alpha1.LocalObjectReference{Name: "load-balancer"},
				Address:      "api",
			},
		}
	}
	return v1alpha1.ClusterInfra{
		Metadata: v1alpha1.Metadata{Name: name},
		Spec: v1alpha1.ClusterInfraSpec{
			Endpoints: endpoints,
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

func cliContainerCluster(name, infraName string) v1alpha1.ContainerCluster {
	return v1alpha1.ContainerCluster{
		Metadata: v1alpha1.Metadata{Name: name},
		Spec: v1alpha1.ContainerClusterSpec{Nodes: []v1alpha1.OCPNodeSpec{{
			Hostname: "master-0",
			Role:     "master",
			MachineRef: v1alpha1.NodeMachineRef{
				ClusterInfra: infraName,
				Name:         "master-0",
			},
		}}},
	}
}
