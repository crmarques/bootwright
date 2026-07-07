package render_test

import (
	"os"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/render"
)

// TestResolveInstallerFailsBeforeWriteOnUnresolvedEndpointBind proves the
// installer entry point — the one that inlines secret material into the
// install-config openshift-install consumes — is guarded by the same
// second-enforcement-line check as the other render entry points. Without the
// guard its VIPs degrade to empty on an unresolved endpoint bind; with it the
// call fails before writing anything to the cluster work dir.
func TestResolveInstallerFailsBeforeWriteOnUnresolvedEndpointBind(t *testing.T) {
	state := v1alpha1.State{
		InfraComponents: []v1alpha1.InfraComponent{{
			Metadata: v1alpha1.Metadata{Name: "lb"},
			Spec: v1alpha1.InfraComponentSpec{
				Type: v1alpha1.ComponentSlotLoadBalancer,
				LoadBalancer: &v1alpha1.LoadBalancerComponent{
					Implementation: v1alpha1.InfraComponentTypeHAProxy,
					MachineRef:     v1alpha1.LocalObjectReference{Name: "controller"},
					BindAddresses: []v1alpha1.LoadBalancerBindAddress{{
						Name:    "vip-a",
						Address: "192.168.133.10",
					}},
				},
			},
		}},
		ContainerClusters: []v1alpha1.ContainerCluster{{
			Metadata: v1alpha1.Metadata{Name: "cluster"},
			Spec: v1alpha1.ContainerClusterSpec{
				Install: v1alpha1.OCPInstallSpec{
					Endpoints: map[string]v1alpha1.Endpoint{
						v1alpha1.EndpointAPI: {Source: v1alpha1.EndpointSource{
							Type:           v1alpha1.EndpointSourceInfraComponent,
							ComponentRef:   v1alpha1.LocalObjectReference{Name: "lb"},
							BindAddressRef: v1alpha1.LocalObjectReference{Name: "vip-b"},
						}},
					},
				},
			},
		}},
	}

	want := `ContainerCluster/cluster spec.install.endpoints.api.source (componentRef "lb", bindAddressRef "vip-b") does not resolve to a loadBalancer bind address`
	clustersDir := t.TempDir()
	if _, err := render.ResolveInstallerForContext("test", clustersDir, t.TempDir(), state); err == nil {
		t.Fatal("render.ResolveInstallerForContext succeeded, want unresolved bindAddress error")
	} else if !strings.Contains(err.Error(), want) {
		t.Fatalf("render.ResolveInstallerForContext error = %q, want it to contain %q", err, want)
	}
	if entries, err := os.ReadDir(clustersDir); err != nil {
		t.Fatalf("read clusters dir: %v", err)
	} else if len(entries) != 0 {
		t.Fatalf("render.ResolveInstallerForContext wrote %d entries before failing, want 0", len(entries))
	}
}

// TestRenderFailsBeforeWriteOnUnresolvableOSInstallImage proves render.All
// refuses to render when a machine slated for a managed-OS install carries a
// MachineImage whose ISO URL fails media.Resolve. Without the check
// machineOSInstallVars returns nil for it and the machine is silently dropped
// from the managed-OS install group; with it the render fails before writing
// anything, naming the unresolvable reference.
func TestRenderFailsBeforeWriteOnUnresolvableOSInstallImage(t *testing.T) {
	state := v1alpha1.State{
		Machines: []v1alpha1.Machine{{
			Metadata: v1alpha1.Metadata{Name: "ceph-0"},
			Spec: v1alpha1.MachineSpec{
				OS: v1alpha1.MachineOSSpec{
					Provided:          v1alpha1.BoolPtr(false),
					InstallProfileRef: v1alpha1.LocalObjectReference{Name: "rhel"},
				},
				Addresses: []v1alpha1.MachineAddress{{Name: "ssh", Address: "192.168.133.30"}},
				Access: v1alpha1.MachineAccess{
					SSH: &v1alpha1.MachineSSHSpec{AddressRef: v1alpha1.LocalObjectReference{Name: "ssh"}},
				},
			},
		}},
		MachineImages: []v1alpha1.MachineImage{{
			Metadata: v1alpha1.Metadata{Name: "rhel-iso"},
			Spec: v1alpha1.MachineImageSpec{
				BootMedia: "nfs://server/export/rhel.iso",
			},
		}},
		MachineInstallProfiles: []v1alpha1.MachineInstallProfile{{
			Metadata: v1alpha1.Metadata{Name: "rhel"},
			Spec: v1alpha1.MachineInstallProfileSpec{
				Installer: v1alpha1.MachineInstallProfileInstaller{
					Anaconda: &v1alpha1.MachineInstallAnaconda{
						ImageRef: v1alpha1.LocalObjectReference{Name: "rhel-iso"},
					},
				},
			},
		}},
		StorageClusters: []v1alpha1.StorageCluster{{
			Metadata: v1alpha1.Metadata{Name: "ceph"},
			Spec: v1alpha1.StorageClusterSpec{
				Type:       v1alpha1.StorageClusterTypeCeph,
				Management: v1alpha1.StorageClusterManagementManaged,
				Ceph: &v1alpha1.StorageClusterCephSpec{
					Topology: v1alpha1.StorageCephTopology{
						Hosts: []v1alpha1.StorageCephHost{{
							Hostname:   "ceph-0",
							MachineRef: v1alpha1.LocalObjectReference{Name: "ceph-0"},
							Site:       "dc1",
							Roles:      []string{v1alpha1.StorageCephRoleMON},
						}},
					},
				},
			},
		}},
	}

	want := `StorageCluster/ceph spec.ceph.topology.hosts[0].machineRef "ceph-0" MachineImage "rhel-iso" spec.bootMedia "nfs://server/export/rhel.iso" does not resolve to installable media:`
	renderedDir := t.TempDir()
	if _, err := render.All(renderedDir, t.TempDir(), t.TempDir(), state); err == nil {
		t.Fatal("render.All succeeded, want unresolvable OS-install image error")
	} else if !strings.Contains(err.Error(), want) {
		t.Fatalf("render.All error = %q, want it to contain %q", err, want)
	}
	if entries, err := os.ReadDir(renderedDir); err != nil {
		t.Fatalf("read rendered dir: %v", err)
	} else if len(entries) != 0 {
		t.Fatalf("render.All wrote %d entries before failing, want 0", len(entries))
	}

	// A resolvable ISO URL clears this specific check: the same machine no
	// longer contributes an unresolved-media event.
	state.MachineImages[0].Spec.BootMedia = "https://example.com/rhel-boot.iso"
	if _, err := render.All(t.TempDir(), t.TempDir(), t.TempDir(), state); err != nil && strings.Contains(err.Error(), "does not resolve to installable media") {
		t.Fatalf("render.All still reports unresolvable media for a resolvable URL: %v", err)
	}
}
