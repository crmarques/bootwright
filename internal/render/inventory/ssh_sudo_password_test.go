package inventory

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infra/locality"
)

func TestAskSSHSudoPasswordReachesAnOperatorIdentityMachineByEnvironment(t *testing.T) {
	state, machine := sshUserOverrideMachine("svcadmin")
	entry := machineInventoryEntry(state, machine, nil, PathOptions{AskSSHSudoPassword: true}, locality.Policy{})
	password, ok := entry["ansible_become_password"].(string)
	if !ok {
		t.Fatal("ansible_become_password is absent, so become on a machine the operator administers cannot answer a sudo prompt")
	}
	if !strings.Contains(password, v1alpha1.SSHSudoPasswordEnv) {
		t.Fatalf("ansible_become_password = %q, want a lookup of %s", password, v1alpha1.SSHSudoPasswordEnv)
	}
	if !strings.Contains(password, "ansible.builtin.env") {
		t.Fatalf("ansible_become_password = %q, want an environment lookup; a literal would put the operator password in the rendered inventory on disk", password)
	}
}

func TestAskSSHSudoPasswordLeavesAProvisionedLoginAlone(t *testing.T) {
	state, machine := sshUserOverrideMachine(v1alpha1.BootwrightSSHUser)
	machine.Spec.OS = v1alpha1.MachineOSSpec{Provided: v1alpha1.BoolPtr(false), InstallProfileRef: v1alpha1.LocalObjectReference{Name: "rhel"}}
	machine.Spec.Access.SSH.Auth = v1alpha1.MachineSSHAuth{Provision: &v1alpha1.MachineSSHProvision{KeyRef: v1alpha1.SecretRef{Name: "bootwright-machine-key"}}}
	state.Machines[0] = machine
	entry := machineInventoryEntry(state, machine, nil, PathOptions{AskSSHSudoPassword: true}, locality.Policy{})
	if _, ok := entry["ansible_become_password"]; ok {
		t.Fatal("the prompted password must not reach a login Bootwright installed; that account holds a passwordless grant and the password would be offered to an account it does not belong to")
	}
}

func TestAskSSHSudoPasswordNamesTheEnvironmentForTheNodeAccessChannel(t *testing.T) {
	state, cluster := nodeAccessState("cephadm", v1alpha1.MachineRootLoginKeep)
	for i := range state.Machines {
		state.Machines[i].Spec.Access.SSH.User = ""
		state.Machines[i].Spec.Access.SSH.Auth = v1alpha1.MachineSSHAuth{OperatorIdentity: &v1alpha1.MachineSSHOperatorIdentity{}}
	}
	node := cluster.Spec.Ceph.Topology.Nodes[0]
	paths := PathOptions{SecretsDir: "/ctx/secrets", AskSSHSudoPassword: true}
	entry := storageNodeInventoryEntry(state, cluster, node, nil, paths, locality.Policy{})
	access, ok := entry["bootwright_node_access"].(map[string]any)
	if !ok {
		t.Fatal("bootwright_node_access is absent")
	}
	if access["sudoPasswordEnv"] != v1alpha1.SSHSudoPasswordEnv {
		t.Fatalf("sudoPasswordEnv = %v, want %s; the node-access channel builds its own ssh argv and never uses become, so it needs the name of the variable holding the password", access["sudoPasswordEnv"], v1alpha1.SSHSudoPasswordEnv)
	}
	for _, value := range access {
		if text, ok := value.(string); ok && strings.Contains(text, "lookup(") {
			t.Fatalf("node access carries a lookup expression %q; the channel must receive the variable name only, so the rendered inventory never holds the password", text)
		}
	}
}

func TestNodeAccessChannelOmitsTheEnvironmentForADeclaredLogin(t *testing.T) {
	state, cluster := nodeAccessState("cephadm", v1alpha1.MachineRootLoginRevoke)
	node := cluster.Spec.Ceph.Topology.Nodes[0]
	paths := PathOptions{SecretsDir: "/ctx/secrets", AskSSHSudoPassword: true}
	entry := storageNodeInventoryEntry(state, cluster, node, nil, paths, locality.Policy{})
	access := entry["bootwright_node_access"].(map[string]any)
	if _, ok := access["sudoPasswordEnv"]; ok {
		t.Fatal("the prompted password must not reach a node whose install login is declared; Bootwright reaches that account with the credential its Secret names")
	}
	if access["sudoPasswordOffered"] != true {
		t.Fatalf("sudoPasswordOffered = %v, want true; the node-access channel must be able to tell an operator who passed --ssh-ask-sudo-password that it does not reach this machine apart from one who never passed it, or its refusal names a flag that is already on the command line", access["sudoPasswordOffered"])
	}
}

func TestNodeAccessChannelMarksARunThatCollectedNoPassword(t *testing.T) {
	state, cluster := nodeAccessState("cephadm", v1alpha1.MachineRootLoginRevoke)
	node := cluster.Spec.Ceph.Topology.Nodes[0]
	entry := storageNodeInventoryEntry(state, cluster, node, nil, PathOptions{SecretsDir: "/ctx/secrets"}, locality.Policy{})
	access := entry["bootwright_node_access"].(map[string]any)
	if _, ok := access["sudoPasswordOffered"]; ok {
		t.Fatalf("sudoPasswordOffered is set on a run that never prompted, so the refusal would withhold the one remedy that applies, got %v", access["sudoPasswordOffered"])
	}
}
