package workflow

import (
	"encoding/json"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func TestCephScaleOutIsStructurallyReconcilable(t *testing.T) {
	proj := func(s v1alpha1.State) string {
		b, err := json.Marshal(storageClusterStructuralHashVars(s, "ceph-bm"))
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		return string(b)
	}
	base := bareMetalManagedOSState()
	base.StorageClusters[0].Spec.Ceph.Networks = v1alpha1.StorageCephNetworks{PublicCIDRs: []string{"10.10.10.0/24"}}
	want := proj(base)

	t.Run("add an OSD host is reconcilable", func(t *testing.T) {
		s := bareMetalManagedOSState()
		s.StorageClusters[0].Spec.Ceph.Networks = v1alpha1.StorageCephNetworks{PublicCIDRs: []string{"10.10.10.0/24"}}
		s.StorageClusters[0].Spec.Ceph.Topology.Hosts = append(s.StorageClusters[0].Spec.Ceph.Topology.Hosts,
			v1alpha1.StorageCephHost{Hostname: "ceph-3", MachineRef: v1alpha1.LocalObjectReference{Name: "ceph-3"}, Site: "dc1", Roles: []string{v1alpha1.StorageCephRoleOSD}})
		if proj(s) != want {
			t.Fatal("adding an OSD host must not move the structural hash (reconciled via ceph orch)")
		}
	})

	t.Run("rebalance mon/mgr roles is reconcilable", func(t *testing.T) {
		s := bareMetalManagedOSState()
		s.StorageClusters[0].Spec.Ceph.Networks = v1alpha1.StorageCephNetworks{PublicCIDRs: []string{"10.10.10.0/24"}}
		s.StorageClusters[0].Spec.Ceph.Topology.Hosts[2].Roles = []string{v1alpha1.StorageCephRoleOSD}
		if proj(s) != want {
			t.Fatal("editing host roles (daemon rebalance) must not move the structural hash")
		}
	})

	t.Run("cluster public network change IS structural", func(t *testing.T) {
		s := bareMetalManagedOSState()
		s.StorageClusters[0].Spec.Ceph.Networks = v1alpha1.StorageCephNetworks{PublicCIDRs: []string{"10.20.0.0/24"}}
		if proj(s) == want {
			t.Fatal("a cluster public-network change MUST move the structural hash (destructive rebuild)")
		}
	})
}
