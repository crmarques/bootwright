package inventory

import (
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infra/locality"
)

func nodeAccessState(clusterUser, rootLogin string) (v1alpha1.State, v1alpha1.StorageCluster) {
	machine := v1alpha1.Machine{
		Metadata: v1alpha1.Metadata{Name: "ceph-1"},
		Spec: v1alpha1.MachineSpec{
			Addresses: []v1alpha1.MachineAddress{{Name: "storage", Address: "10.20.0.11"}},
			Access: v1alpha1.MachineAccess{
				RootLogin: rootLogin,
				SSH: &v1alpha1.MachineSSHSpec{
					AddressRef: v1alpha1.LocalObjectReference{Name: "storage"},
					User:       "root",
					KeyRef:     v1alpha1.SecretRef{Name: "ceph-node-ssh"},
				},
			},
		},
	}
	cluster := v1alpha1.StorageCluster{
		Metadata: v1alpha1.Metadata{Name: "ceph"},
		Spec: v1alpha1.StorageClusterSpec{
			Type: v1alpha1.StorageClusterTypeCeph,
			Ceph: &v1alpha1.StorageClusterCephSpec{
				Cephadm: v1alpha1.StorageCephadmSpec{
					ClusterSSH: v1alpha1.StorageCephadmSSHSpec{User: clusterUser},
					Bootstrap:  v1alpha1.StorageCephadmBootstrap{Node: "node01"},
				},
				Topology: v1alpha1.StorageCephTopology{
					Nodes: []v1alpha1.StorageCephNode{{
						Name:       "node01",
						MachineRef: v1alpha1.LocalObjectReference{Name: "ceph-1"},
					}},
				},
			},
		},
	}
	return v1alpha1.State{
		Machines:        []v1alpha1.Machine{machine},
		StorageClusters: []v1alpha1.StorageCluster{cluster},
	}, cluster
}

func TestClusterSSHUserAlwaysRendered(t *testing.T) {
	state, cluster := nodeAccessState("root", v1alpha1.MachineRootLoginKeep)
	got := storageClusterSSHVars(state, cluster, nil, PathOptions{SecretsDir: "/ctx/secrets"})
	if got["user"] != "root" {
		t.Fatalf("clusterSSH.user = %v, want root emitted unconditionally so the role needs no Jinja fallback", got["user"])
	}
}

func TestStorageNodeEntryConnectsAsProvisionedAccount(t *testing.T) {
	state, cluster := nodeAccessState("cephadm", v1alpha1.MachineRootLoginRevoke)
	node := cluster.Spec.Ceph.Topology.Nodes[0]
	entry := storageNodeInventoryEntry(state, cluster, node, nil, PathOptions{SecretsDir: "/ctx/secrets"}, locality.Policy{})
	if entry["ansible_user"] != "cephadm" {
		t.Fatalf("ansible_user = %v, want the provisioned account once root is revoked", entry["ansible_user"])
	}
	access, ok := entry["bootwright_node_access"].(map[string]any)
	if !ok {
		t.Fatal("bootwright_node_access is absent; the node access role has no contract to act on")
	}
	if access["installUser"] != "root" {
		t.Fatalf("installUser = %v, want the untouched install-window identity", access["installUser"])
	}
	if access["rootLogin"] != v1alpha1.MachineRootLoginRevoke {
		t.Fatalf("rootLogin = %v, want the machine posture carried to the role", access["rootLogin"])
	}
	if access["sudoersPath"] != "/etc/sudoers.d/60-bootwright-cephadm" {
		t.Fatalf("sudoersPath = %v, want a per-user drop-in", access["sudoersPath"])
	}
}

func TestStorageNodeEntryKeepsMachineUserWhenRootKept(t *testing.T) {
	state, cluster := nodeAccessState("root", v1alpha1.MachineRootLoginKeep)
	node := cluster.Spec.Ceph.Topology.Nodes[0]
	entry := storageNodeInventoryEntry(state, cluster, node, nil, PathOptions{SecretsDir: "/ctx/secrets"}, locality.Policy{})
	if _, ok := entry["bootwright_node_access"]; ok {
		t.Fatal("bootwright_node_access must be absent when no account is managed, so existing state renders unchanged")
	}
	if entry["ansible_user"] != "root" {
		t.Fatalf("ansible_user = %v, want the unchanged machine access identity", entry["ansible_user"])
	}
}
