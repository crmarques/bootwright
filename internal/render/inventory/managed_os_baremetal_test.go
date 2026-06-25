package inventory

import "testing"

import "github.com/crmarques/bootwright/api/v1alpha1"

// bareMetalManagedOSState is a managed Ceph cluster whose nodes install their OS
// (os.provided=false + installProfileRef) on a bare-metal substrate. Bare-metal
// machines carry no substrate profile, so they have no provider host: the
// managed-OS install is driven from the controller over the BMC (Redfish), the
// same controller-local model KubeVirt/vSphere use. The node must still land in
// the cluster's managed-OS inventory group, or the machines-phase install task
// is created (managedOSMachineNames sees installsOS=true) but skipped because
// its --limit group resolves to zero hosts.
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
						KeyRef:     v1alpha1.SecretRef{Name: "ceph-node-ssh"},
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
						Bootstrap:  v1alpha1.StorageCephadmBootstrap{Host: "ceph-0"},
					},
					Topology: v1alpha1.StorageCephTopology{
						Hosts: []v1alpha1.StorageCephHost{
							{Hostname: "ceph-0", MachineRef: v1alpha1.LocalObjectReference{Name: "ceph-0"}, Site: "dc1", Roles: []string{v1alpha1.StorageCephRoleMON, v1alpha1.StorageCephRoleOSD}},
							{Hostname: "ceph-1", MachineRef: v1alpha1.LocalObjectReference{Name: "ceph-1"}, Site: "dc1", Roles: []string{v1alpha1.StorageCephRoleMON, v1alpha1.StorageCephRoleOSD}},
							{Hostname: "ceph-2", MachineRef: v1alpha1.LocalObjectReference{Name: "ceph-2"}, Site: "dc1", Roles: []string{v1alpha1.StorageCephRoleMON, v1alpha1.StorageCephRoleOSD}},
						},
					},
				},
			},
		}},
	}
}

func TestManagedOSGroupIncludesBareMetalNodes(t *testing.T) {
	state := bareMetalManagedOSState()
	group := ManagedOSGroupName("ceph-bm")

	members := HostGroupMembers(state)[group]
	if len(members) != 3 {
		t.Fatalf("managed-OS group %q members = %v, want 3 bare-metal nodes", group, members)
	}

	counts := HostGroupCounts(state)
	if counts[group] != 3 {
		t.Fatalf("managed-OS group %q count = %d, want 3", group, counts[group])
	}

	// The machines-phase install task is limited to this group; an empty group
	// makes ansible abort with "no hosts to target" and the task is skipped.
	for _, host := range members {
		entry, ok := HostGroupMembersWithOwnershipRecords(state, nil)[group]
		if !ok || len(entry) == 0 {
			t.Fatalf("host %q missing from managed-OS group", host)
		}
	}
}
