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
	if got := install.SSHKeyRef.Name; got != "cluster-admin-pub-key" {
		t.Fatalf("SSHKeyRef.Name = %q, want cluster-admin-pub-key", got)
	}
}

func TestNormalizeDefaultsEnvironmentProxyUseFor(t *testing.T) {
	state := v1alpha1.State{
		Environments: []v1alpha1.Environment{{
			Metadata: v1alpha1.Metadata{Name: "env"},
			Spec: v1alpha1.EnvironmentSpec{
				Proxy: &v1alpha1.EnvironmentProxySpec{
					HTTPProxy: "http://proxy.example.test:3128",
				},
			},
		}},
	}

	Normalize(&state)

	useFor := state.Environments[0].Spec.Proxy.UseFor
	if useFor.Bootwright == nil || !*useFor.Bootwright {
		t.Fatalf("UseFor.Bootwright = %v, want true", useFor.Bootwright)
	}
	if useFor.ClusterInstall == nil || !*useFor.ClusterInstall {
		t.Fatalf("UseFor.ClusterInstall = %v, want true", useFor.ClusterInstall)
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
