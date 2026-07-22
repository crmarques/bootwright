package render_test

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/render"
)

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
	if _, err := render.All(t.TempDir(), t.TempDir(), t.TempDir(), state); err == nil {
		t.Fatal("render.All succeeded, want unresolved bindAddress error")
	} else if !strings.Contains(err.Error(), want) {
		t.Fatalf("render.All error = %q, want it to contain %q", err, want)
	}
	if _, err := render.Effective(t.TempDir(), state); err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("render.Effective error = %v, want unresolved bindAddress error", err)
	}
	if _, err := render.ToolInputsForContext("test", t.TempDir(), t.TempDir(), state); err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("render.ToolInputs error = %v, want unresolved bindAddress error", err)
	}
}

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
						Bootstrap: v1alpha1.StorageCephadmBootstrap{Node: "ceph-0"},
					},
					Topology: v1alpha1.StorageCephTopology{
						Nodes: []v1alpha1.StorageCephNode{{
							Name:       "ceph-0",
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
	wantHost := `StorageCluster/ceph spec.ceph.topology.nodes[0].machineRef "ceph-0" does not resolve to a machine address for node "ceph-0"`
	if !strings.Contains(err.Error(), wantHost) {
		t.Fatalf("render.All error = %q, want it to contain %q", err, wantHost)
	}
	wantBootstrap := `StorageCluster/ceph spec.ceph.cephadm.bootstrap.node "ceph-0" does not resolve to a machine address`
	if !strings.Contains(err.Error(), wantBootstrap) {
		t.Fatalf("render.All error = %q, want it to contain %q", err, wantBootstrap)
	}

	state.Machines[0].Spec.Addresses = []v1alpha1.MachineAddress{{Name: "ssh", Address: "192.168.133.30"}}
	state.Machines[0].Spec.Access = v1alpha1.MachineAccess{
		SSH: &v1alpha1.MachineSSHSpec{AddressRef: v1alpha1.LocalObjectReference{Name: "ssh"}},
	}
	if _, err := render.All(t.TempDir(), t.TempDir(), t.TempDir(), state); err != nil {
		t.Fatalf("render.All with resolvable address: %v", err)
	}
}
