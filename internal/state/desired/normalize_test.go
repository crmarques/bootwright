package desiredstate

import (
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func TestNormalizeDefaultsClusterInstallSecretRefs(t *testing.T) {
	state := v1alpha1.State{
		Environments: []v1alpha1.Environment{{
			Metadata: v1alpha1.Metadata{Name: "env"},
			Spec: v1alpha1.EnvironmentSpec{
				BaseDomain: "example.test",
			},
		}},
		ContainerClusters: []v1alpha1.ContainerCluster{{
			Metadata: v1alpha1.Metadata{Name: "cluster-a"},
		}},
	}

	Normalize(&state)

	install := state.ContainerClusters[0].Spec.Install
	if got := install.PullSecretRef.Name; got != v1alpha1.DefaultPullSecretName {
		t.Fatalf("PullSecretRef.Name = %q, want %q", got, v1alpha1.DefaultPullSecretName)
	}
	if got := install.NodeSSH.KeyPairRef.Name; got != v1alpha1.DefaultNodeSSHKeyName {
		t.Fatalf("NodeSSH.KeyPairRef.Name = %q, want %q", got, v1alpha1.DefaultNodeSSHKeyName)
	}
}

func TestNormalizeUsesEnvironmentInstallDefaults(t *testing.T) {
	state := v1alpha1.State{
		Environments: []v1alpha1.Environment{{
			Metadata: v1alpha1.Metadata{Name: "env"},
			Spec: v1alpha1.EnvironmentSpec{
				BaseDomain: "example.test",
				Defaults: v1alpha1.EnvironmentDefaultsSpec{
					Install: v1alpha1.EnvironmentInstallDefaultsSpec{
						PullSecretRef: v1alpha1.SecretRef{Name: "custom-pull"},
						NodeSSH: v1alpha1.NodeSSHSpec{
							PublicKeyRef:  v1alpha1.SecretRef{Name: "cluster-public"},
							PrivateKeyRef: v1alpha1.SecretRef{Name: "cluster-private"},
						},
					},
				},
			},
		}},
		ContainerClusters: []v1alpha1.ContainerCluster{{
			Metadata: v1alpha1.Metadata{Name: "cluster-a"},
		}},
	}

	Normalize(&state)

	install := state.ContainerClusters[0].Spec.Install
	if got := install.PullSecretRef.Name; got != "custom-pull" {
		t.Fatalf("PullSecretRef.Name = %q, want custom-pull", got)
	}
	if got := install.NodeSSH.PublicKeyRef.Name; got != "cluster-public" {
		t.Fatalf("NodeSSH.PublicKeyRef.Name = %q, want cluster-public", got)
	}
	if got := install.NodeSSH.PrivateKeyRef.Name; got != "cluster-private" {
		t.Fatalf("NodeSSH.PrivateKeyRef.Name = %q, want cluster-private", got)
	}
}

func TestNormalizeUsesEnvironmentArtifactAccessDefaultsForConnectedBareMetal(t *testing.T) {
	state := artifactAccessDefaultState()
	state.InfraProviders = []v1alpha1.InfraProvider{bareMetalProvider("rack", "server-0")}
	state.ClusterInfras = []v1alpha1.ClusterInfra{{
		Metadata: v1alpha1.Metadata{Name: "infra"},
		Spec: v1alpha1.ClusterInfraSpec{Components: v1alpha1.ClusterComponents{
			Machines: []v1alpha1.ClusterMachineComponent{{
				Name: "master-0",
				From: v1alpha1.From{Provider: "rack", Name: "server-0"},
			}},
		}},
	}}
	state.ContainerClusters = []v1alpha1.ContainerCluster{containerClusterWithInfra("cluster", "infra")}

	Normalize(&state)

	access := state.ClusterInfras[0].Spec.ArtifactAccess
	if got := access.ServerRef.Name; got != "default" {
		t.Fatalf("serverRef.name = %q, want default", got)
	}
	if got := access.RedfishVirtualMedia.EndpointRef.Name; got != "bmc" {
		t.Fatalf("redfishVirtualMedia.endpointRef.name = %q, want bmc", got)
	}
	if got := access.ContainerClusterInstall.EndpointRef.Name; got != "" {
		t.Fatalf("containerClusterInstall.endpointRef.name = %q, want empty", got)
	}
}

func TestNormalizeUsesEnvironmentArtifactAccessDefaultsForDisconnectedInstall(t *testing.T) {
	state := artifactAccessDefaultState()
	state.ClusterInfras = []v1alpha1.ClusterInfra{{
		Metadata: v1alpha1.Metadata{Name: "infra"},
		Spec: v1alpha1.ClusterInfraSpec{Components: v1alpha1.ClusterComponents{
			Machines: []v1alpha1.ClusterMachineComponent{{
				Name: "master-0",
				From: v1alpha1.From{Provider: "libvirt", Profile: "worker"},
			}},
		}},
	}}
	cluster := containerClusterWithInfra("cluster", "infra")
	cluster.Spec.Install.Mode = v1alpha1.InstallModeDisconnected
	state.ContainerClusters = []v1alpha1.ContainerCluster{cluster}

	Normalize(&state)

	access := state.ClusterInfras[0].Spec.ArtifactAccess
	if got := access.ServerRef.Name; got != "default" {
		t.Fatalf("serverRef.name = %q, want default", got)
	}
	if got := access.ContainerClusterInstall.EndpointRef.Name; got != "cluster" {
		t.Fatalf("containerClusterInstall.endpointRef.name = %q, want cluster", got)
	}
	if got := access.RedfishVirtualMedia.EndpointRef.Name; got != "" {
		t.Fatalf("redfishVirtualMedia.endpointRef.name = %q, want empty", got)
	}
}

func TestNormalizeEnvironmentArtifactAccessDefaultsKeepExplicitValues(t *testing.T) {
	state := artifactAccessDefaultState()
	state.InfraProviders = []v1alpha1.InfraProvider{bareMetalProvider("rack", "server-0")}
	state.ClusterInfras = []v1alpha1.ClusterInfra{{
		Metadata: v1alpha1.Metadata{Name: "infra"},
		Spec: v1alpha1.ClusterInfraSpec{
			ArtifactAccess: v1alpha1.ClusterArtifactAccess{
				ServerRef: v1alpha1.LocalObjectReference{Name: "site"},
				RedfishVirtualMedia: v1alpha1.ClusterArtifactEndpointRef{
					EndpointRef: v1alpha1.LocalObjectReference{Name: "oob"},
				},
			},
			Components: v1alpha1.ClusterComponents{
				Machines: []v1alpha1.ClusterMachineComponent{{
					Name: "master-0",
					From: v1alpha1.From{Provider: "rack", Name: "server-0"},
				}},
			},
		},
	}}
	state.ContainerClusters = []v1alpha1.ContainerCluster{containerClusterWithInfra("cluster", "infra")}

	Normalize(&state)

	access := state.ClusterInfras[0].Spec.ArtifactAccess
	if got := access.ServerRef.Name; got != "site" {
		t.Fatalf("serverRef.name = %q, want site", got)
	}
	if got := access.RedfishVirtualMedia.EndpointRef.Name; got != "oob" {
		t.Fatalf("redfishVirtualMedia.endpointRef.name = %q, want oob", got)
	}
	if got := access.ContainerClusterInstall.EndpointRef.Name; got != "" {
		t.Fatalf("containerClusterInstall.endpointRef.name = %q, want empty", got)
	}
}

func TestNormalizeDefaultsSecretStorageAndSSHKeyPairType(t *testing.T) {
	state := v1alpha1.State{
		Environments: []v1alpha1.Environment{{
			Metadata: v1alpha1.Metadata{Name: "env"},
			Spec: v1alpha1.EnvironmentSpec{
				BaseDomain: "example.test",
				Secrets: map[string]v1alpha1.EnvironmentSecretSpec{
					"cluster-admin-ssh-key": {
						Generated: &v1alpha1.EnvironmentSecretGenerated{
							SSHKeyPair: &v1alpha1.GeneratedSSHKeyPairSpec{},
						},
					},
				},
			},
		}},
	}

	Normalize(&state)

	env := state.Environments[0]
	if got := env.Spec.SecretStorage.Mode; got != v1alpha1.SecretStorageModeSource {
		t.Fatalf("SecretStorage.Mode = %q, want %q", got, v1alpha1.SecretStorageModeSource)
	}
	if got := env.Spec.Secrets["cluster-admin-ssh-key"].Generated.SSHKeyPair.Type; got != v1alpha1.SSHKeyPairTypeEd25519 {
		t.Fatalf("SSHKeyPair.Type = %q, want %q", got, v1alpha1.SSHKeyPairTypeEd25519)
	}
}

func artifactAccessDefaultState() v1alpha1.State {
	return v1alpha1.State{
		Environments: []v1alpha1.Environment{{
			Metadata: v1alpha1.Metadata{Name: "env"},
			Spec: v1alpha1.EnvironmentSpec{
				BaseDomain: "example.test",
				Defaults: v1alpha1.EnvironmentDefaultsSpec{
					ArtifactAccess: v1alpha1.ClusterArtifactAccess{
						ServerRef: v1alpha1.LocalObjectReference{Name: "default"},
						RedfishVirtualMedia: v1alpha1.ClusterArtifactEndpointRef{
							EndpointRef: v1alpha1.LocalObjectReference{Name: "bmc"},
						},
						ContainerClusterInstall: v1alpha1.ClusterArtifactEndpointRef{
							EndpointRef: v1alpha1.LocalObjectReference{Name: "cluster"},
						},
					},
				},
			},
		}},
	}
}

func bareMetalProvider(name, machine string) v1alpha1.InfraProvider {
	return v1alpha1.InfraProvider{
		Metadata: v1alpha1.Metadata{Name: name},
		Spec: v1alpha1.InfraProviderSpec{
			Machines: []v1alpha1.MachineCapability{{
				Name:      machine,
				BareMetal: &v1alpha1.MachineBareMetalCapability{},
			}},
		},
	}
}

func containerClusterWithInfra(name, infra string) v1alpha1.ContainerCluster {
	return v1alpha1.ContainerCluster{
		Metadata: v1alpha1.Metadata{Name: name},
		Spec: v1alpha1.ContainerClusterSpec{
			Nodes: []v1alpha1.OCPNodeSpec{{
				Hostname: "master-0",
				MachineRef: v1alpha1.NodeMachineRef{
					ClusterInfra: infra,
					Name:         "master-0",
				},
			}},
		},
	}
}

func TestNormalizeDefaultsInfraComponentProxy(t *testing.T) {
	state := v1alpha1.State{
		InfraComponents: []v1alpha1.InfraComponent{{
			Metadata: v1alpha1.Metadata{Name: "proxy"},
			Spec: v1alpha1.InfraComponentSpec{
				Proxy: &v1alpha1.ProxyComponent{
					Type:    v1alpha1.InfraComponentTypeSquid,
					HostRef: v1alpha1.LocalObjectReference{Name: "services-host"},
				},
			},
		}},
	}

	Normalize(&state)

	proxy := state.InfraComponents[0].Spec.Proxy
	if got := proxy.Port; got != v1alpha1.DefaultSquidPort {
		t.Fatalf("proxy port = %d, want %d", got, v1alpha1.DefaultSquidPort)
	}
	if got := proxy.BindAddress; got != v1alpha1.DefaultServiceBindAddress {
		t.Fatalf("proxy bindAddress = %q, want %q", got, v1alpha1.DefaultServiceBindAddress)
	}
}

func TestNormalizeDefaultsArtifactServerListener(t *testing.T) {
	state := v1alpha1.State{
		InfraComponents: []v1alpha1.InfraComponent{{
			Spec: v1alpha1.InfraComponentSpec{
				ArtifactServer: &v1alpha1.ArtifactServerComponent{
					HostRef: v1alpha1.LocalObjectReference{Name: "services-host"},
				},
			},
		}},
	}

	Normalize(&state)

	listeners := state.InfraComponents[0].Spec.ArtifactServer.Listeners
	if len(listeners) != 1 {
		t.Fatalf("artifact server listeners = %d, want 1", len(listeners))
	}
	if got := listeners[0].Protocol; got != v1alpha1.ArtifactServerProtocolHTTPS {
		t.Fatalf("artifact server listener protocol = %q, want %q", got, v1alpha1.ArtifactServerProtocolHTTPS)
	}
	if got := listeners[0].Port; got != v1alpha1.DefaultArtifactsHTTPPort {
		t.Fatalf("artifact server listener port = %d, want %d", got, v1alpha1.DefaultArtifactsHTTPPort)
	}
}

func TestNormalizeDefaultsBMCEmulationPorts(t *testing.T) {
	state := v1alpha1.State{
		InfraProviders: []v1alpha1.InfraProvider{{
			Spec: v1alpha1.InfraProviderSpec{
				MachineProfiles: []v1alpha1.MachineProfileCapability{{
					Name: "sno",
					Libvirt: &v1alpha1.MachineProfileLibvirtProvisioner{
						BMCEmulationDefaults: &v1alpha1.BMCEmulationDefaults{},
					},
				}},
			},
		}},
	}

	Normalize(&state)

	d := state.InfraProviders[0].Spec.MachineProfiles[0].Libvirt.BMCEmulationDefaults
	if got := d.Port; got != v1alpha1.DefaultBMCEmulationStartPort {
		t.Fatalf("BMC emulator port = %d, want %d", got, v1alpha1.DefaultBMCEmulationStartPort)
	}
	if got := d.VMediaPort; got != v1alpha1.DefaultBMCEmulationStartPort+1 {
		t.Fatalf("BMC emulator vmediaPort = %d, want %d", got, v1alpha1.DefaultBMCEmulationStartPort+1)
	}
}
