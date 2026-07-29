package inventory

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infra/locality"
)

func nodeAccessStateWithClusterKey(t *testing.T) (v1alpha1.State, v1alpha1.StorageCluster) {
	t.Helper()
	state, cluster := nodeAccessState("cephadm", v1alpha1.MachineRootLoginRevoke)
	cluster.Spec.Ceph.Cephadm.ClusterSSH.KeyRef = v1alpha1.LocalObjectReference{Name: "ceph-cephadm-ssh-key"}
	state.StorageClusters[0] = cluster
	return state, cluster
}

func nodeAccessVarsWithClusterKey(t *testing.T) map[string]any {
	t.Helper()
	state, cluster := nodeAccessStateWithClusterKey(t)
	node := cluster.Spec.Ceph.Topology.Nodes[0]
	entry := storageNodeInventoryEntry(state, cluster, node, nil, PathOptions{SecretsDir: "/ctx/secrets"}, locality.Policy{})
	access, ok := entry["bootwright_node_access"].(map[string]any)
	if !ok {
		t.Fatal("bootwright_node_access is absent")
	}
	return access
}

func TestNodeAccessOffersThePrivateHalfOfTheKeyItAuthorizes(t *testing.T) {
	access := nodeAccessVarsWithClusterKey(t)
	public, _ := access["accountPublicKeyPath"].(string)
	private, _ := access["accountPrivateKeyPath"].(string)
	if public == "" {
		t.Fatal("accountPublicKeyPath is absent, so authorize.yml has no key to authorize for the orchestration account")
	}
	if private == "" {
		t.Fatal("accountPrivateKeyPath is absent: authorize.yml authorizes the cluster key for the orchestration account, so every later proof connects as that account with a credential it does not hold and sshd refuses the login (B-040)")
	}
	if strings.TrimSuffix(public, ".pub") != private {
		t.Fatalf("the account is authorized with %q but Bootwright would connect with %q; the two halves must come from the one Secret spec.ceph.cephadm.clusterSSH.keyRef names, or the pairing silently drifts", public, private)
	}
}

func TestNodeAccessKeepsTheClusterKeyDistinctFromTheMachineKey(t *testing.T) {
	access := nodeAccessVarsWithClusterKey(t)
	if account, install := access["accountPrivateKeyPath"], access["installPrivateKeyPath"]; account == install {
		t.Fatalf("the orchestration account key and the machine install key resolved to the same path (%v); the machine key is the install-window credential and must not stand in for the cluster key cephadm orchestrates with", account)
	}
}
