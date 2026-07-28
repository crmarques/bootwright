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
				Auth:       v1alpha1.MachineSSHAuth{OperatorIdentity: &v1alpha1.MachineSSHOperatorIdentity{}},
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
		t.Fatalf("ansible_user = %v, want the override for an operatorIdentity machine", entry["ansible_user"])
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

func TestSSHUserLeavesADeclaredLoginAlone(t *testing.T) {
	state, machine := sshUserOverrideMachine("svcadmin")
	machine.Spec.Access.SSH.Auth = v1alpha1.MachineSSHAuth{PrivateKeyRef: v1alpha1.SecretRef{Name: "lab-key"}}
	state.Machines[0] = machine
	entry := machineInventoryEntry(state, machine, nil, PathOptions{SSHUser: "operator"}, locality.Policy{})
	if entry["ansible_user"] != "svcadmin" {
		t.Fatalf("ansible_user = %v, want the declared account; --ssh-user names the operator's own identity, not a login a Secret already names", entry["ansible_user"])
	}
	if _, ok := entry["bootwright_declared_ssh_user"]; ok {
		t.Fatal("bootwright_declared_ssh_user must stay out of the inventory when the override does not apply")
	}
}

func TestSSHUserLeavesAProvisionedLoginAlone(t *testing.T) {
	state, machine := sshUserOverrideMachine(v1alpha1.BootwrightSSHUser)
	machine.Spec.OS = v1alpha1.MachineOSSpec{Provided: v1alpha1.BoolPtr(false), InstallProfileRef: v1alpha1.LocalObjectReference{Name: "rhel"}}
	machine.Spec.Access.SSH.Auth = v1alpha1.MachineSSHAuth{Provision: &v1alpha1.MachineSSHProvision{KeyRef: v1alpha1.SecretRef{Name: "bootwright-machine-key"}}}
	state.Machines[0] = machine
	entry := machineInventoryEntry(state, machine, nil, PathOptions{SSHUser: "operator"}, locality.Policy{})
	if entry["ansible_user"] != v1alpha1.BootwrightSSHUser {
		t.Fatalf("ansible_user = %v, want the service account Bootwright installed; moving it would fail the ownership probe closed", entry["ansible_user"])
	}
}

func TestSSHUserLeavesTheStorageNodeConnectionAlone(t *testing.T) {
	state, cluster := nodeAccessState("cephadm", v1alpha1.MachineRootLoginRevoke)
	node := cluster.Spec.Ceph.Topology.Nodes[0]
	paths := PathOptions{SecretsDir: "/ctx/secrets", SSHUser: "operator"}
	entry := storageNodeInventoryEntry(state, cluster, node, nil, paths, locality.Policy{})
	if entry["ansible_user"] != "cephadm" {
		t.Fatalf("ansible_user = %v, want the cluster orchestration account; the node login is declared, so --ssh-user does not apply", entry["ansible_user"])
	}
	access, ok := entry["bootwright_node_access"].(map[string]any)
	if !ok {
		t.Fatal("bootwright_node_access is absent")
	}
	if access["user"] != "cephadm" {
		t.Fatalf("node access user = %v, want the cluster orchestration account untouched by --ssh-user", access["user"])
	}
	if access["installUser"] != "root" {
		t.Fatalf("installUser = %v, want the declared install identity, not the override", access["installUser"])
	}
	if access["connectionOverride"] != false {
		t.Fatalf("connectionOverride = %v, want false; the override does not reach a declared login", access["connectionOverride"])
	}
}

func TestSSHUserMovesAnOperatorIdentityStorageNode(t *testing.T) {
	state, cluster := nodeAccessState("cephadm", v1alpha1.MachineRootLoginKeep)
	for i := range state.Machines {
		state.Machines[i].Spec.Access.SSH.User = ""
		state.Machines[i].Spec.Access.SSH.Auth = v1alpha1.MachineSSHAuth{OperatorIdentity: &v1alpha1.MachineSSHOperatorIdentity{}}
	}
	node := cluster.Spec.Ceph.Topology.Nodes[0]
	entry := storageNodeInventoryEntry(state, cluster, node, nil, PathOptions{SSHUser: "operator"}, locality.Policy{})
	if entry["ansible_user"] != "operator" {
		t.Fatalf("ansible_user = %v, want the override on a node whose login is the operator's own", entry["ansible_user"])
	}
	access := entry["bootwright_node_access"].(map[string]any)
	if access["user"] != "cephadm" {
		t.Fatalf("node access user = %v, want the cluster orchestration account untouched by --ssh-user", access["user"])
	}
	if access["connectionOverride"] != true {
		t.Fatalf("connectionOverride = %v, want true so teardown does not fall back to the cluster account", access["connectionOverride"])
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
