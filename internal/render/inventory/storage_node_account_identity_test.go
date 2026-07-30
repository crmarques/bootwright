package inventory

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/infra/locality"
)

func TestStorageNodeEntryOffersTheKeyOfTheAccountItConnectsAs(t *testing.T) {
	state, cluster := nodeAccessStateWithClusterKey(t)
	node := cluster.Spec.Ceph.Topology.Nodes[0]
	paths := PathOptions{SecretsDir: "/ctx/secrets", PreferredIdentityFile: "/home/op/.ssh/id_bootwright"}
	entry := storageNodeInventoryEntry(state, cluster, node, nil, paths, locality.Policy{})
	access, ok := entry["bootwright_node_access"].(map[string]any)
	if !ok {
		t.Fatal("bootwright_node_access is absent")
	}
	private, _ := access["accountPrivateKeyPath"].(string)
	if private == "" {
		t.Fatal("accountPrivateKeyPath is absent, so there is no credential to offer")
	}
	if entry["ansible_user"] != access["user"] {
		t.Fatalf("ansible_user = %v, want the orchestration account once root is revoked", entry["ansible_user"])
	}
	common, _ := entry["ansible_ssh_common_args"].(string)
	if !strings.Contains(common, "IdentityFile="+private) {
		t.Fatalf("the play connects as %v but offers %q: authorize.yml authorizes only the cluster key for that account, so sshd refuses the login with Permission denied (publickey)", entry["ansible_user"], common)
	}
	declared, ok := entry["bootwright_declared_ssh_common_args"].(string)
	if !ok || !strings.Contains(declared, "IdentityFile="+private) {
		t.Fatalf("the declared args drop the account key (%v), so the ownership record is written over a connection the account refuses", entry["bootwright_declared_ssh_common_args"])
	}
	if got := entry["ansible_ssh_private_key_file"]; got == private {
		t.Fatalf("ansible_ssh_private_key_file = %v; the machine key must stay offered for the install identity the teardown may fall back to", got)
	}
}
