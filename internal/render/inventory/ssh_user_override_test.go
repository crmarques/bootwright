package inventory

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infra/locality"
)

func sshUserOverrideMachine(user string) (v1alpha1.State, v1alpha1.Machine) {
	machine := v1alpha1.Machine{
		Metadata: v1alpha1.Metadata{Name: "bastion"},
		Spec: v1alpha1.MachineSpec{
			OS:        v1alpha1.MachineOSSpec{Provided: v1alpha1.BoolPtr(true)},
			Addresses: []v1alpha1.MachineAddress{{Name: "ssh", Address: "192.0.2.10"}},
			Access: v1alpha1.MachineAccess{SSH: &v1alpha1.MachineSSHSpec{
				AddressRef: v1alpha1.LocalObjectReference{Name: "ssh"},
				User:       user,
				Auth:       v1alpha1.MachineSSHAuth{ControllerIdentity: &v1alpha1.MachineSSHControllerIdentity{}},
			}},
		},
	}
	return v1alpha1.State{Machines: []v1alpha1.Machine{machine}}, machine
}

func TestSSHUserOverridesTheDeclaredMachineLogin(t *testing.T) {
	state, machine := sshUserOverrideMachine("svcadmin")
	entry := machineInventoryEntry(state, machine, nil, PathOptions{SSHUser: "operator"}, locality.Policy{})
	if entry["ansible_user"] != "operator" {
		t.Fatalf("ansible_user = %v, want the override", entry["ansible_user"])
	}
	if entry["bootwright_declared_ssh_user"] != "svcadmin" {
		t.Fatalf("bootwright_declared_ssh_user = %v, want the declared account preserved for the ownership record", entry["bootwright_declared_ssh_user"])
	}
}

func TestSSHUserNamesTheAccountForAnUndeclaredLogin(t *testing.T) {
	state, machine := sshUserOverrideMachine("")
	entry := machineInventoryEntry(state, machine, nil, PathOptions{SSHUser: "operator"}, locality.Policy{})
	if entry["ansible_user"] != "operator" {
		t.Fatalf("ansible_user = %v, want the override for a controllerIdentity machine", entry["ansible_user"])
	}
	if entry["bootwright_declared_ssh_user"] != "" {
		t.Fatalf("bootwright_declared_ssh_user = %v, want empty so the record keeps declaring no account", entry["bootwright_declared_ssh_user"])
	}
}

func TestWithoutOverrideNoDeclaredVarsAreEmitted(t *testing.T) {
	state, machine := sshUserOverrideMachine("svcadmin")
	entry := machineInventoryEntry(state, machine, nil, PathOptions{}, locality.Policy{})
	if entry["ansible_user"] != "svcadmin" {
		t.Fatalf("ansible_user = %v, want the declared account", entry["ansible_user"])
	}
	for _, key := range []string{"bootwright_declared_ssh_user", "bootwright_declared_ssh_common_args"} {
		if _, ok := entry[key]; ok {
			t.Fatalf("%s must stay out of the inventory when no per-invocation preference is set", key)
		}
	}
}

func TestPreferredIdentityKeepsTheDeclaredCommonArgsForTheRecord(t *testing.T) {
	state, machine := sshUserOverrideMachine("svcadmin")
	paths := PathOptions{SecretsDir: "/ctx/secrets", PreferredIdentityFile: "/home/op/.ssh/id_ed25519"}
	entry := machineInventoryEntry(state, machine, nil, paths, locality.Policy{})
	effective, _ := entry["ansible_ssh_common_args"].(string)
	declared, ok := entry["bootwright_declared_ssh_common_args"].(string)
	if !ok {
		t.Fatal("bootwright_declared_ssh_common_args is absent, so the ownership record would bake in the preferred key")
	}
	if !strings.Contains(effective, "IdentityFile=/home/op/.ssh/id_ed25519") {
		t.Fatalf("ansible_ssh_common_args = %q, want the preferred identity offered", effective)
	}
	if strings.Contains(declared, "IdentityFile=") {
		t.Fatalf("bootwright_declared_ssh_common_args = %q, want no per-invocation identity", declared)
	}
}

func TestSSHUserMovesTheStorageNodeConnectionNotTheClusterAccount(t *testing.T) {
	state, cluster := nodeAccessState("cephadm", v1alpha1.MachineRootLoginRevoke)
	node := cluster.Spec.Ceph.Topology.Nodes[0]
	paths := PathOptions{SecretsDir: "/ctx/secrets", SSHUser: "operator"}
	entry := storageNodeInventoryEntry(state, cluster, node, nil, paths, locality.Policy{})
	if entry["ansible_user"] != "operator" {
		t.Fatalf("ansible_user = %v, want the override even though root login is revoked", entry["ansible_user"])
	}
	access, ok := entry["bootwright_node_access"].(map[string]any)
	if !ok {
		t.Fatal("bootwright_node_access is absent")
	}
	if access["user"] != "cephadm" {
		t.Fatalf("node access user = %v, want the cluster orchestration account untouched by --ssh-user", access["user"])
	}
	if access["installUser"] != "operator" {
		t.Fatalf("installUser = %v, want the connection identity to follow the override", access["installUser"])
	}
	if access["connectionOverride"] != true {
		t.Fatalf("connectionOverride = %v, want true so teardown does not fall back to the cluster account", access["connectionOverride"])
	}
	if access["installIdentity"] != false {
		t.Fatalf("installIdentity = %v, want false; the override is not the cluster account", access["installIdentity"])
	}
}

func TestSSHUserReachesAStorageNodeWithNoDeclaredSSHAccess(t *testing.T) {
	state, cluster := nodeAccessState("cephadm", v1alpha1.MachineRootLoginKeep)
	for i := range state.Machines {
		state.Machines[i].Spec.Access.SSH = nil
	}
	node := cluster.Spec.Ceph.Topology.Nodes[0]
	entry := storageNodeInventoryEntry(state, cluster, node, nil, PathOptions{SSHUser: "operator"}, locality.Policy{})
	if entry["ansible_user"] != "operator" {
		t.Fatalf("ansible_user = %v, want the override; without it Ansible falls back to the account running bootwright, which is root under the local-root gate", entry["ansible_user"])
	}
}

func TestStorageNodeAccessCarriesThePreferredIdentity(t *testing.T) {
	state, cluster := nodeAccessState("cephadm", v1alpha1.MachineRootLoginKeep)
	node := cluster.Spec.Ceph.Topology.Nodes[0]
	paths := PathOptions{SecretsDir: "/ctx/secrets", PreferredIdentityFile: "/home/op/.ssh/id_ed25519"}
	entry := storageNodeInventoryEntry(state, cluster, node, nil, paths, locality.Policy{})
	access, ok := entry["bootwright_node_access"].(map[string]any)
	if !ok {
		t.Fatal("bootwright_node_access is absent")
	}
	if access["preferredIdentityPath"] != "/home/op/.ssh/id_ed25519" {
		t.Fatalf("preferredIdentityPath = %v, want the operator key the node-access probes must offer", access["preferredIdentityPath"])
	}
	plain := storageNodeInventoryEntry(state, cluster, node, nil, PathOptions{SecretsDir: "/ctx/secrets"}, locality.Policy{})
	plainAccess := plain["bootwright_node_access"].(map[string]any)
	if _, ok := plainAccess["preferredIdentityPath"]; ok {
		t.Fatal("preferredIdentityPath must be absent when no key is preferred")
	}
}
