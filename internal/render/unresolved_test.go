package render_test

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/render"
)

// TestRenderFailsOnUnresolvedEndpointBindAddress constructs the resolution gap
// directly in Go (Validate rejects it since the bind-name resolution fix): an
// install endpoint sourced from a loadBalancer whose source.bindAddress
// matches no bindAddresses[].name. stateview.EndpointAddress degrades to ""
// for it and the installer platform guards with `if vip != ""`, so without the
// render-side check the install-config would ship with empty api/ingress VIPs.
func TestRenderFailsOnUnresolvedEndpointBindAddress(t *testing.T) {
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
							Type:         v1alpha1.EndpointSourceInfraComponent,
							ComponentRef: v1alpha1.LocalObjectReference{Name: "lb"},
							BindAddress:  "vip-b",
						}},
					},
				},
			},
		}},
	}

	want := `ContainerCluster/cluster spec.install.endpoints.api.source (componentRef "lb", bindAddress "vip-b") does not resolve to a loadBalancer bind address`
	if _, err := render.All(t.TempDir(), t.TempDir(), t.TempDir(), state); err == nil {
		t.Fatal("render.All succeeded, want unresolved bindAddress error")
	} else if !strings.Contains(err.Error(), want) {
		t.Fatalf("render.All error = %q, want it to contain %q", err, want)
	}
	if _, err := render.Effective(t.TempDir(), state); err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("render.Effective error = %v, want unresolved bindAddress error", err)
	}
	if _, err := render.ToolInputs(t.TempDir(), t.TempDir(), state); err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("render.ToolInputs error = %v, want unresolved bindAddress error", err)
	}
}

// TestRenderFailsOnUnresolvedStorageNodeAddress constructs the topology gap
// directly in Go: a managed Ceph cluster whose host machine carries no
// resolvable address. topology.NodeAddress returns "" and MonitorEndpoints
// silently drops the host from the monitor list, so without the render-side
// check the inventory ansible_host and the bootstrap monIP would render empty.
func TestRenderFailsOnUnresolvedStorageNodeAddress(t *testing.T) {
	state := v1alpha1.State{
		Machines: []v1alpha1.Machine{{
			Metadata: v1alpha1.Metadata{Name: "ceph-0"},
			Spec: v1alpha1.MachineSpec{
				OS: v1alpha1.MachineOSSpec{Provided: v1alpha1.BoolPtr(true)},
			},
		}},
		StorageClusters: []v1alpha1.StorageCluster{{
			Metadata: v1alpha1.Metadata{Name: "ceph"},
			Spec: v1alpha1.StorageClusterSpec{
				Type:       v1alpha1.StorageClusterTypeCeph,
				Management: v1alpha1.StorageClusterManagementManaged,
				Ceph: &v1alpha1.StorageClusterCephSpec{
					Cephadm: v1alpha1.StorageCephadmSpec{
						Bootstrap: v1alpha1.StorageCephadmBootstrap{Host: "ceph-0"},
					},
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

	_, err := render.All(t.TempDir(), t.TempDir(), t.TempDir(), state)
	if err == nil {
		t.Fatal("render.All succeeded, want unresolved storage node address error")
	}
	wantHost := `StorageCluster/ceph spec.ceph.topology.hosts[0].machineRef "ceph-0" does not resolve to a machine address for host "ceph-0"`
	if !strings.Contains(err.Error(), wantHost) {
		t.Fatalf("render.All error = %q, want it to contain %q", err, wantHost)
	}
	wantBootstrap := `StorageCluster/ceph spec.ceph.cephadm.bootstrap.host "ceph-0" does not resolve to a machine address`
	if !strings.Contains(err.Error(), wantBootstrap) {
		t.Fatalf("render.All error = %q, want it to contain %q", err, wantBootstrap)
	}

	// The same state with a resolvable machine address renders cleanly.
	state.Machines[0].Spec.Addresses = []v1alpha1.MachineAddress{{Name: "ssh", Address: "192.168.133.30"}}
	state.Machines[0].Spec.Access = v1alpha1.MachineAccess{
		SSH: &v1alpha1.MachineSSHSpec{AddressRef: v1alpha1.LocalObjectReference{Name: "ssh"}},
	}
	if _, err := render.All(t.TempDir(), t.TempDir(), t.TempDir(), state); err != nil {
		t.Fatalf("render.All with resolvable address: %v", err)
	}
}
