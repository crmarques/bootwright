package workflow

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func dataLossFixtureState() v1alpha1.State {
	return v1alpha1.State{
		InfraProviders: []v1alpha1.InfraProvider{{
			Metadata: v1alpha1.Metadata{Name: "lab-libvirt"},
			Spec:     v1alpha1.InfraProviderSpec{Type: v1alpha1.ProvisionerLibvirt},
		}, {
			Metadata: v1alpha1.Metadata{Name: "lab-metal"},
			Spec:     v1alpha1.InfraProviderSpec{Type: v1alpha1.ProvisionerBareMetal},
		}},
		Machines: []v1alpha1.Machine{{
			Metadata: v1alpha1.Metadata{Name: "virt-osd"},
			Spec: v1alpha1.MachineSpec{
				Substrate: v1alpha1.MachineSubstrate{ProviderRef: v1alpha1.LocalObjectReference{Name: "lab-libvirt"}},
				OS:        v1alpha1.MachineOSSpec{Provided: v1alpha1.BoolPtr(false)},
			},
		}, {
			Metadata: v1alpha1.Metadata{Name: "metal-osd"},
			Spec: v1alpha1.MachineSpec{
				Substrate: v1alpha1.MachineSubstrate{ProviderRef: v1alpha1.LocalObjectReference{Name: "lab-metal"}},
				OS:        v1alpha1.MachineOSSpec{Provided: v1alpha1.BoolPtr(true)},
			},
		}},
		StorageClusters: []v1alpha1.StorageCluster{
			dataLossStorageCluster("virt-ceph", "virt-osd", v1alpha1.StorageClusterManagementManaged),
			dataLossStorageCluster("metal-ceph", "metal-osd", v1alpha1.StorageClusterManagementManaged),
		},
	}
}

func dataLossStorageCluster(name, machine, management string) v1alpha1.StorageCluster {
	return v1alpha1.StorageCluster{
		Metadata: v1alpha1.Metadata{Name: name},
		Spec: v1alpha1.StorageClusterSpec{
			Type:       v1alpha1.StorageClusterTypeCeph,
			Management: management,
			Ceph: &v1alpha1.StorageClusterCephSpec{
				Topology: v1alpha1.StorageCephTopology{Nodes: []v1alpha1.StorageCephNode{{
					Name:       "node-1",
					MachineRef: v1alpha1.LocalObjectReference{Name: machine},
					Roles:      []string{v1alpha1.StorageCephRoleOSD},
					OSD:        &v1alpha1.StorageCephNodeOSD{},
				}}},
			},
		},
	}
}

func TestDestroyDataLossCoversEveryScopeThatDestroysOSDData(t *testing.T) {
	state := dataLossFixtureState()
	all := []string{"metal-ceph", "virt-ceph"}
	cases := []struct {
		name         string
		scope        DestroySafetyScope
		names        []string
		wantZapped   []string
		wantReleased []string
	}{{
		name:       "cluster-layer teardown zaps every selected cluster",
		scope:      DestroySafetyScope{TearsClusters: true},
		names:      all,
		wantZapped: all,
	}, {
		name:         "machine-layer teardown loses the OSD data of provider-backed hosts only",
		scope:        DestroySafetyScope{TearsMachines: true},
		names:        all,
		wantReleased: []string{"virt-ceph"},
	}, {
		name:         "a full teardown reports both consequences",
		scope:        DestroySafetyScope{TearsClusters: true, TearsMachines: true},
		names:        all,
		wantZapped:   all,
		wantReleased: []string{"virt-ceph"},
	}, {
		name:  "a scope that tears neither layer plans no data loss",
		scope: DestroySafetyScope{},
		names: all,
	}, {
		name:         "a machine-layer teardown narrowed to the bare-metal cluster plans no data loss",
		scope:        DestroySafetyScope{TearsMachines: true},
		names:        []string{"metal-ceph"},
		wantReleased: nil,
	}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := EvaluateDestroyDataLoss(state, tc.names, tc.scope)
			if strings.Join(got.Zapped, ",") != strings.Join(tc.wantZapped, ",") {
				t.Errorf("Zapped = %v, want %v", got.Zapped, tc.wantZapped)
			}
			if strings.Join(got.Released, ",") != strings.Join(tc.wantReleased, ",") {
				t.Errorf("Released = %v, want %v", got.Released, tc.wantReleased)
			}
			if got.Planned() != (len(tc.wantZapped)+len(tc.wantReleased) > 0) {
				t.Errorf("Planned() = %v for %v", got.Planned(), got)
			}
			if got.Planned() && got.Consequence() == "" {
				t.Error("a planned data loss must be able to state its consequence in the refusal")
			}
			if got.Planned() && got.Warning() == "" {
				t.Error("a planned data loss must be able to state its warning before the confirmation")
			}
		})
	}
}

func TestDestroyDataLossIgnoresExternalStorageClusterMachines(t *testing.T) {
	state := dataLossFixtureState()
	state.StorageClusters[1] = dataLossStorageCluster("virt-external", "virt-osd", v1alpha1.StorageClusterManagementExternal)
	got := EvaluateDestroyDataLoss(state, []string{"virt-external"}, DestroySafetyScope{TearsMachines: true})
	if got.Planned() {
		t.Fatalf("an external storage cluster owns no substrate Bootwright deletes, got %v", got)
	}
}
