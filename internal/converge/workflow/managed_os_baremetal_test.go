package workflow

import (
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/render"
)

func bareMetalManagedOSState() v1alpha1.State {
	node := func(name, address string) v1alpha1.Machine {
		return v1alpha1.Machine{
			Metadata: v1alpha1.Metadata{Name: name},
			Spec: v1alpha1.MachineSpec{
				Capabilities: []string{v1alpha1.MachineCapabilityCephNode},
				Substrate:    v1alpha1.MachineSubstrate{ProviderRef: v1alpha1.LocalObjectReference{Name: "bare-metal"}},
				OS: v1alpha1.MachineOSSpec{
					Provided:          v1alpha1.BoolPtr(false),
					InstallProfileRef: v1alpha1.LocalObjectReference{Name: "rhel-9-ceph-node"},
				},
				Addresses: []v1alpha1.MachineAddress{{Name: "ssh", Address: address}},
				Access: v1alpha1.MachineAccess{
					SSH: &v1alpha1.MachineSSHSpec{
						AddressRef: v1alpha1.LocalObjectReference{Name: "ssh"},
						Auth:     v1alpha1.MachineSSHAuth{PrivateKeyRef: v1alpha1.SecretRef{Name: "ceph-node-ssh"}},
					},
				},
			},
		}
	}
	return v1alpha1.State{
		Environments: []v1alpha1.Environment{{Metadata: v1alpha1.Metadata{Name: "lab"}}},
		InfraProviders: []v1alpha1.InfraProvider{{
			Metadata: v1alpha1.Metadata{Name: "bare-metal"},
			Spec:     v1alpha1.InfraProviderSpec{Type: v1alpha1.ProvisionerBareMetal},
		}},
		Machines: []v1alpha1.Machine{node("ceph-0", "10.10.10.10"), node("ceph-1", "10.10.10.11"), node("ceph-2", "10.10.10.12")},
		StorageClusters: []v1alpha1.StorageCluster{{
			Metadata: v1alpha1.Metadata{Name: "ceph-bm"},
			Spec: v1alpha1.StorageClusterSpec{
				Type:       v1alpha1.StorageClusterTypeCeph,
				Management: v1alpha1.StorageClusterManagementManaged,
				Ceph: &v1alpha1.StorageClusterCephSpec{
					Cephadm: v1alpha1.StorageCephadmSpec{
						AddressRef: v1alpha1.LocalObjectReference{Name: "ssh"},
						Bootstrap:  v1alpha1.StorageCephadmBootstrap{Node: "ceph-0"},
					},
					Topology: v1alpha1.StorageCephTopology{
						Nodes: []v1alpha1.StorageCephNode{
							{Name: "ceph-0", MachineRef: v1alpha1.LocalObjectReference{Name: "ceph-0"}, Site: "dc1", Roles: []string{v1alpha1.StorageCephRoleMON, v1alpha1.StorageCephRoleOSD}},
							{Name: "ceph-1", MachineRef: v1alpha1.LocalObjectReference{Name: "ceph-1"}, Site: "dc1", Roles: []string{v1alpha1.StorageCephRoleMON, v1alpha1.StorageCephRoleOSD}},
							{Name: "ceph-2", MachineRef: v1alpha1.LocalObjectReference{Name: "ceph-2"}, Site: "dc1", Roles: []string{v1alpha1.StorageCephRoleMON, v1alpha1.StorageCephRoleOSD}},
						},
					},
				},
			},
		}},
	}
}

func TestMachinesPhasePlansReachableManagedOSTaskForBareMetal(t *testing.T) {
	state := bareMetalManagedOSState()
	target := ApplyTarget{Name: "machines", PhaseNames: []string{ApplyPhaseMachines}}

	tasks, err := PlanApplyTasksChecked(target, state)
	if err != nil {
		t.Fatalf("plan machines: %v", err)
	}

	var osTask *ApplyTask
	for i := range tasks {
		if tasks[i].Entry.Kind == ApplyTaskKindManagedMachineOS && tasks[i].Entry.Cluster == "ceph-bm" {
			osTask = &tasks[i]
			break
		}
	}
	if osTask == nil {
		t.Fatalf("machines plan did not schedule a managed-OS install task: %v", taskKinds(tasks))
	}

	counts := render.HostGroupCountsWithOwnershipRecords(state, nil)
	if got := counts[osTask.Limit]; got != 3 {
		t.Fatalf("managed-OS task limit %q resolves to %d hosts, want 3 (task would be skipped)", osTask.Limit, got)
	}
}
