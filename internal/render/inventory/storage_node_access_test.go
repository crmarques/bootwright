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
					Auth:     v1alpha1.MachineSSHAuth{PrivateKeyRef: v1alpha1.SecretRef{Name: "ceph-node-ssh"}},
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
	if access["connectionAddress"] != "10.20.0.11" {
		t.Fatalf("connectionAddress = %v, want the canonical Machine SSH connection address", access["connectionAddress"])
	}
	if access["knownHostsManaged"] != true {
		t.Fatalf("knownHostsManaged = %v, want true when Machine SSH trust has no explicit Secret", access["knownHostsManaged"])
	}
	if access["sudoersPath"] != "/etc/sudoers.d/60-bootwright-cephadm" {
		t.Fatalf("sudoersPath = %v, want a per-user drop-in", access["sudoersPath"])
	}
}

func TestStorageNodeEntryFlagsADistinctOrchestrationAccount(t *testing.T) {
	state, cluster := nodeAccessState("cephadm", v1alpha1.MachineRootLoginRevoke)
	node := cluster.Spec.Ceph.Topology.Nodes[0]
	entry := storageNodeInventoryEntry(state, cluster, node, nil, PathOptions{SecretsDir: "/ctx/secrets"}, locality.Policy{})
	access := entry["bootwright_node_access"].(map[string]any)
	if access["installIdentity"] != false {
		t.Fatalf("installIdentity = %v, want false when the account is provisioned alongside a separate install-window identity", access["installIdentity"])
	}
}

func TestStorageNodeEntryFlagsTheInstallIdentityAsTheAccount(t *testing.T) {
	state, cluster := nodeAccessState("cephadm", v1alpha1.MachineRootLoginRevoke)
	state.Machines[0].Spec.Access.SSH.User = "cephadm"
	node := cluster.Spec.Ceph.Topology.Nodes[0]
	entry := storageNodeInventoryEntry(state, cluster, node, nil, PathOptions{SecretsDir: "/ctx/secrets"}, locality.Policy{})
	access := entry["bootwright_node_access"].(map[string]any)
	if access["installIdentity"] != true {
		t.Fatalf("installIdentity = %v, want true so the role reconciles the account instead of reporting a second identity it could fall back to", access["installIdentity"])
	}
	if access["installUser"] != "cephadm" {
		t.Fatalf("installUser = %v, want the install-window identity even when it is the orchestration account", access["installUser"])
	}
	if entry["ansible_user"] != "cephadm" {
		t.Fatalf("ansible_user = %v, want the single account every flow connects as", entry["ansible_user"])
	}
}

func TestStorageNodeEntryDoesNotManageExplicitSSHTrust(t *testing.T) {
	state, cluster := nodeAccessState("cephadm", v1alpha1.MachineRootLoginRevoke)
	state.Machines[0].Spec.Access.SSH.KnownHostsRef = v1alpha1.SecretRef{Name: "ceph-known-hosts"}
	node := cluster.Spec.Ceph.Topology.Nodes[0]
	entry := storageNodeInventoryEntry(state, cluster, node, nil, PathOptions{SecretsDir: "/ctx/secrets"}, locality.Policy{})
	access, ok := entry["bootwright_node_access"].(map[string]any)
	if !ok {
		t.Fatal("bootwright_node_access is absent")
	}
	if access["knownHostsManaged"] != false {
		t.Fatalf("knownHostsManaged = %v, want false for an explicit knownHostsRef", access["knownHostsManaged"])
	}
}

func TestStorageNodeEntryRendersCanonicalFQDNForTeardown(t *testing.T) {
	state, cluster := nodeAccessState("cephadm", v1alpha1.MachineRootLoginRevoke)
	state.Machines[0].Spec.Addresses = append(state.Machines[0].Spec.Addresses, v1alpha1.MachineAddress{
		Name:    v1alpha1.MachineAddressFQDN,
		Address: "ceph-1.example.test",
	})
	state.Machines[0].Spec.Network.Config.Spec = &v1alpha1.NetworkConfigSpec{
		NameResolutionRefs: []v1alpha1.LocalObjectReference{{Name: "external-dns"}},
	}
	node := cluster.Spec.Ceph.Topology.Nodes[0]
	entry := storageNodeInventoryEntry(state, cluster, node, nil, PathOptions{SecretsDir: "/ctx/secrets"}, locality.Policy{})
	access, ok := entry["bootwright_node_access"].(map[string]any)
	if !ok {
		t.Fatal("bootwright_node_access is absent")
	}
	if access["address"] != "10.20.0.11" {
		t.Fatalf("address = %v, want the raw SSH address retained as the trusted alias source", access["address"])
	}
	if access["connectionAddress"] != "ceph-1.example.test" {
		t.Fatalf("connectionAddress = %v, want the canonical FQDN used by Ansible", access["connectionAddress"])
	}
}

func TestStorageNodeEntryKeepsInstallIdentityUntilRootIsRevoked(t *testing.T) {
	state, cluster := nodeAccessState("cephadm", v1alpha1.MachineRootLoginKeep)
	node := cluster.Spec.Ceph.Topology.Nodes[0]
	entry := storageNodeInventoryEntry(state, cluster, node, nil, PathOptions{SecretsDir: "/ctx/secrets"}, locality.Policy{})
	if entry["ansible_user"] != "root" {
		t.Fatalf("ansible_user = %v, want the install identity while root login is kept; switching before the account is provisioned breaks every flow that does not run the node access task first (destroy, scoped applies)", entry["ansible_user"])
	}
	if _, ok := entry["bootwright_node_access"]; !ok {
		t.Fatal("bootwright_node_access must still be emitted so the role can provision the cephadm account")
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
