package workflow

import (
	"encoding/json"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func TestManagedMachineOSStructuralHashIgnoresNonOSEdits(t *testing.T) {
	proj := func(s v1alpha1.State) string {
		b, err := json.Marshal(managedMachineOSStructuralHashVars(s, "ceph-bm"))
		if err != nil {
			t.Fatalf("marshal projection: %v", err)
		}
		return string(b)
	}
	want := proj(bareMetalManagedOSState())

	t.Run("OSD device add is not a reinstall", func(t *testing.T) {
		s := bareMetalManagedOSState()
		s.StorageClusters[0].Spec.Ceph.Topology.Hosts[0].Devices = []string{"/dev/sdb"}
		if proj(s) != want {
			t.Fatal("an OSD-device add must not move the managed-OS structural hash")
		}
	})

	t.Run("machine substrate/BMC edit is not a reinstall", func(t *testing.T) {
		s := bareMetalManagedOSState()
		s.Machines[0].Spec.Substrate.ProviderRef.Name = "bare-metal-relocated"
		if proj(s) != want {
			t.Fatal("a machine substrate/BMC edit must not move the managed-OS structural hash")
		}
	})

	t.Run("pool add is not a reinstall", func(t *testing.T) {
		s := bareMetalManagedOSState()
		s.StoragePools = []v1alpha1.StoragePool{{Metadata: v1alpha1.Metadata{Name: "rbd"}}}
		if proj(s) != want {
			t.Fatal("adding a pool must not move the managed-OS structural hash")
		}
	})

	t.Run("OS install-profile change IS a reinstall", func(t *testing.T) {
		s := bareMetalManagedOSState()
		s.Machines[0].Spec.OS.InstallProfileRef.Name = "rhel-9-hardened"
		if proj(s) == want {
			t.Fatal("an OS install-profile change MUST move the managed-OS structural hash")
		}
	})
}
