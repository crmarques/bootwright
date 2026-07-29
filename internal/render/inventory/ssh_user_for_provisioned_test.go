package inventory

import (
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infra/locality"
)

func provisionedMachine() (v1alpha1.State, v1alpha1.Machine) {
	state, machine := sshUserOverrideMachine(v1alpha1.BootwrightSSHUser)
	machine.Spec.OS = v1alpha1.MachineOSSpec{Provided: v1alpha1.BoolPtr(false), InstallProfileRef: v1alpha1.LocalObjectReference{Name: "rhel"}}
	machine.Spec.Access.SSH.Auth = v1alpha1.MachineSSHAuth{Provision: &v1alpha1.MachineSSHProvision{KeyRef: v1alpha1.SecretRef{Name: "bootwright-machine-key"}}}
	state.Machines[0] = machine
	return state, machine
}

func TestSSHUserForProvisionedMovesTheInstalledLogin(t *testing.T) {
	state, machine := provisionedMachine()
	paths := PathOptions{SSHUser: "carmj", SSHUserForProvisioned: true}
	entry := machineInventoryEntry(state, machine, nil, paths, locality.Policy{})
	if entry["ansible_user"] != "carmj" {
		t.Fatalf("ansible_user = %v, want the override; --ssh-user-for-provisioned widens it to the machines Bootwright installed", entry["ansible_user"])
	}
	if entry["bootwright_declared_ssh_user"] != v1alpha1.BootwrightSSHUser {
		t.Fatalf("bootwright_declared_ssh_user = %v, want the installed account preserved so the ownership record does not bake in a per-invocation account", entry["bootwright_declared_ssh_user"])
	}
}

func TestSSHUserForProvisionedMovesADeclaredSecretLogin(t *testing.T) {
	state, machine := sshUserOverrideMachine("svcadmin")
	machine.Spec.Access.SSH.Auth = v1alpha1.MachineSSHAuth{PrivateKeyRef: v1alpha1.SecretRef{Name: "lab-key"}}
	state.Machines[0] = machine
	paths := PathOptions{SSHUser: "carmj", SSHUserForProvisioned: true}
	entry := machineInventoryEntry(state, machine, nil, paths, locality.Policy{})
	if entry["ansible_user"] != "carmj" {
		t.Fatalf("ansible_user = %v, want the override on every machine in the run", entry["ansible_user"])
	}
}

func TestSSHUserForProvisionedIsInertWithoutTheOverride(t *testing.T) {
	state, machine := provisionedMachine()
	entry := machineInventoryEntry(state, machine, nil, PathOptions{SSHUserForProvisioned: true}, locality.Policy{})
	if entry["ansible_user"] != v1alpha1.BootwrightSSHUser {
		t.Fatalf("ansible_user = %v, want the installed account; there is no account to widen without --ssh-user", entry["ansible_user"])
	}
}

func TestSSHUserForProvisionedCarriesTheSudoPasswordToInstalledMachines(t *testing.T) {
	state, machine := provisionedMachine()
	paths := PathOptions{SSHUser: "carmj", SSHUserForProvisioned: true, AskSSHSudoPassword: true}
	entry := machineInventoryEntry(state, machine, nil, paths, locality.Policy{})
	if _, ok := entry["ansible_become_password"]; !ok {
		t.Fatal("ansible_become_password is absent; once the run logs in as the operator everywhere, the operator's sudo password has to travel with it")
	}
}

func TestSSHUserForProvisionedMovesTheStorageNodeInstallIdentity(t *testing.T) {
	state, cluster := nodeAccessState("cephadm", v1alpha1.MachineRootLoginRevoke)
	node := cluster.Spec.Ceph.Topology.Nodes[0]
	paths := PathOptions{SecretsDir: "/ctx/secrets", SSHUser: "carmj", SSHUserForProvisioned: true}
	entry := storageNodeInventoryEntry(state, cluster, node, nil, paths, locality.Policy{})
	access, ok := entry["bootwright_node_access"].(map[string]any)
	if !ok {
		t.Fatal("bootwright_node_access is absent")
	}
	if access["installUser"] != "carmj" {
		t.Fatalf("installUser = %v, want the widened override; the node-access channel borrows that identity to create the orchestration account", access["installUser"])
	}
	if access["user"] != "cephadm" {
		t.Fatalf("node access user = %v, want the cluster orchestration account untouched; the flag moves the borrowed login, never the created one", access["user"])
	}
	if access["connectionOverride"] != true {
		t.Fatalf("connectionOverride = %v, want true so teardown does not fall back to the cluster account", access["connectionOverride"])
	}
}
