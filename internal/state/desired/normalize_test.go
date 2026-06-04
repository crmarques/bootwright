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
	wantSSHKey := v1alpha1.ClusterAdminSSHKeyName("cluster-a")
	if got := install.NodeSSH.KeyPairRef.Name; got != wantSSHKey {
		t.Fatalf("NodeSSH.KeyPairRef.Name = %q, want %q", got, wantSSHKey)
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

func TestNormalizeDefaultsHostSSHUser(t *testing.T) {
	previous := currentHostSSHUser
	currentHostSSHUser = func() string { return "operator" }
	t.Cleanup(func() { currentHostSSHUser = previous })

	state := v1alpha1.State{
		Machines: []v1alpha1.Machine{
			{
				Metadata: v1alpha1.Metadata{Name: "defaulted"},
				Spec: v1alpha1.MachineSpec{
					OS: v1alpha1.MachineOSSpec{
						Mode: v1alpha1.MachineOSModeExternal,
						SSH:  &v1alpha1.MachineSSHSpec{AddressName: "ssh"},
					},
				},
			},
			{
				Metadata: v1alpha1.Metadata{Name: "explicit"},
				Spec: v1alpha1.MachineSpec{
					OS: v1alpha1.MachineOSSpec{
						Mode: v1alpha1.MachineOSModeExternal,
						SSH:  &v1alpha1.MachineSSHSpec{AddressName: "ssh", User: "root"},
					},
				},
			},
			{
				Metadata: v1alpha1.Metadata{Name: "no-ssh"},
			},
		},
	}

	Normalize(&state)

	if got := state.Machines[0].Spec.OS.SSH.User; got != "operator" {
		t.Fatalf("defaulted Machine SSH user = %q, want operator", got)
	}
	if got := state.Machines[1].Spec.OS.SSH.User; got != "root" {
		t.Fatalf("explicit Machine SSH user = %q, want root", got)
	}
}

func TestNormalizeUsesEnvironmentArtifactAccessDefaultsForConnectedBareMetal(t *testing.T) {
	state := artifactAccessDefaultState()
	state.InfraProviders = []v1alpha1.InfraProvider{bareMetalProvider("rack")}
	state.Machines = []v1alpha1.Machine{bareMetalMachine("server-0", "rack")}
	state.ContainerClusters = []v1alpha1.ContainerCluster{containerClusterWithMachine("cluster", "server-0")}

	Normalize(&state)

	access := state.ContainerClusters[0].Spec.Install.ArtifactAccess
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
	state.Machines = []v1alpha1.Machine{profiledMachine("server-0", "libvirt", v1alpha1.ProvisionerLibvirt)}
	cluster := containerClusterWithMachine("cluster", "server-0")
	cluster.Spec.Install.Mode = v1alpha1.InstallModeDisconnected
	state.ContainerClusters = []v1alpha1.ContainerCluster{cluster}

	Normalize(&state)

	access := state.ContainerClusters[0].Spec.Install.ArtifactAccess
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
	state.InfraProviders = []v1alpha1.InfraProvider{bareMetalProvider("rack")}
	state.Machines = []v1alpha1.Machine{bareMetalMachine("server-0", "rack")}
	cluster := containerClusterWithMachine("cluster", "server-0")
	cluster.Spec.Install.ArtifactAccess = v1alpha1.ClusterArtifactAccess{
		ServerRef: v1alpha1.LocalObjectReference{Name: "site"},
		RedfishVirtualMedia: v1alpha1.ClusterArtifactEndpointRef{
			EndpointRef: v1alpha1.LocalObjectReference{Name: "oob"},
		},
	}
	state.ContainerClusters = []v1alpha1.ContainerCluster{cluster}

	Normalize(&state)

	access := state.ContainerClusters[0].Spec.Install.ArtifactAccess
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
					"cluster-a-cluster-admin-ssh-key": {
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
	if got := env.Spec.Secrets["cluster-a-cluster-admin-ssh-key"].Generated.SSHKeyPair.Type; got != v1alpha1.SSHKeyPairTypeEd25519 {
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

func bareMetalProvider(name string) v1alpha1.InfraProvider {
	return v1alpha1.InfraProvider{
		Metadata: v1alpha1.Metadata{Name: name},
		Spec: v1alpha1.InfraProviderSpec{
			Type:      v1alpha1.ProvisionerBareMetal,
			BareMetal: &v1alpha1.InfraProviderBareMetal{},
		},
	}
}

func bareMetalMachine(name, provider string) v1alpha1.Machine {
	return v1alpha1.Machine{
		Metadata: v1alpha1.Metadata{Name: name},
		Spec: v1alpha1.MachineSpec{
			Substrate: v1alpha1.MachineSubstrate{
				ProviderRef: v1alpha1.LocalObjectReference{Name: provider},
				BareMetal:   &v1alpha1.MachineBareMetalSubstrate{},
			},
			OS: v1alpha1.MachineOSSpec{Mode: v1alpha1.MachineOSModeRaw},
		},
	}
}

func profiledMachine(name, provider, providerType string) v1alpha1.Machine {
	machine := v1alpha1.Machine{
		Metadata: v1alpha1.Metadata{Name: name},
		Spec: v1alpha1.MachineSpec{
			Substrate: v1alpha1.MachineSubstrate{
				ProviderRef: v1alpha1.LocalObjectReference{Name: provider},
			},
			OS: v1alpha1.MachineOSSpec{Mode: v1alpha1.MachineOSModeRaw},
		},
	}
	if providerType == v1alpha1.ProvisionerLibvirt {
		machine.Spec.Substrate.Libvirt = &v1alpha1.MachineProfiledSubstrate{
			ProfileRef: v1alpha1.LocalObjectReference{Name: "worker"},
		}
	}
	return machine
}

func containerClusterWithMachine(name, machine string) v1alpha1.ContainerCluster {
	return v1alpha1.ContainerCluster{
		Metadata: v1alpha1.Metadata{Name: name},
		Spec: v1alpha1.ContainerClusterSpec{
			Nodes: []v1alpha1.OCPNodeSpec{{
				Hostname:   "master-0",
				Role:       "master",
				MachineRef: v1alpha1.LocalObjectReference{Name: machine},
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
					Type:       v1alpha1.InfraComponentTypeSquid,
					MachineRef: v1alpha1.LocalObjectReference{Name: "services-host"},
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
					MachineRef: v1alpha1.LocalObjectReference{Name: "services-host"},
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
				Type: v1alpha1.ProvisionerLibvirt,
				Libvirt: &v1alpha1.InfraProviderLibvirt{
					BMCEmulationDefaults: &v1alpha1.BMCEmulationDefaults{},
				},
			},
		}},
	}

	Normalize(&state)

	d := state.InfraProviders[0].Spec.Libvirt.BMCEmulationDefaults
	if got := d.Port; got != v1alpha1.DefaultBMCEmulationStartPort {
		t.Fatalf("BMC emulator port = %d, want %d", got, v1alpha1.DefaultBMCEmulationStartPort)
	}
	if got := d.VMediaPort; got != v1alpha1.DefaultBMCEmulationStartPort+1 {
		t.Fatalf("BMC emulator vmediaPort = %d, want %d", got, v1alpha1.DefaultBMCEmulationStartPort+1)
	}
}
