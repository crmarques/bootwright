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

func TestNormalizeDefaultsArtifactHTTPPort(t *testing.T) {
	state := v1alpha1.State{
		InfraProviders: []v1alpha1.InfraProvider{{
			Spec: v1alpha1.InfraProviderSpec{
				ArtifactPublishers: []v1alpha1.ArtifactPublisherCapability{{
					Name: "default",
					HTTP: &v1alpha1.ArtifactHTTPCapability{
						HostRef: v1alpha1.LocalObjectReference{Name: "services-host"},
					},
				}},
			},
		}},
	}

	Normalize(&state)

	http := state.InfraProviders[0].Spec.ArtifactPublishers[0].HTTP
	if got := http.Port; got != v1alpha1.DefaultArtifactsHTTPPort {
		t.Fatalf("artifact publisher HTTP port = %d, want %d", got, v1alpha1.DefaultArtifactsHTTPPort)
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
