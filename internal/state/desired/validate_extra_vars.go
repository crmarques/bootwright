package desiredstate

import (
	"fmt"
	"sort"
)

var reservedConnectionExtraVars = map[string]bool{
	"ansible_become":               true,
	"ansible_become_method":        true,
	"ansible_become_pass":          true,
	"ansible_become_password":      true,
	"ansible_become_user":          true,
	"ansible_connection":           true,
	"ansible_host":                 true,
	"ansible_password":             true,
	"ansible_port":                 true,
	"ansible_private_key_file":     true,
	"ansible_ssh_common_args":      true,
	"ansible_ssh_extra_args":       true,
	"ansible_ssh_host":             true,
	"ansible_ssh_pass":             true,
	"ansible_ssh_port":             true,
	"ansible_ssh_private_key_file": true,
	"ansible_ssh_user":             true,
	"ansible_user":                 true,
}

var reservedEscalationExtraVars = map[string]bool{
	"ansible_pipelining":     true,
	"ansible_ssh_pipelining": true,
	"ansible_ssh_use_tty":    true,
}

const reservedConnectionRefusal = "%s sets %q, which is not supported. Ansible ranks an extra var above every inventory value, so it silently repoints the identity Bootwright connects and escalates with — the bootwright service account on a machine it installed, the account from Machine spec.access.ssh.user, a storage cluster's spec.ceph.cephadm.clusterSSH.user, their keys, and the recorded host-key trust — and it does so for every host in the run, not only the ones this targets. Declare the login in desired state, or on a machine that declares spec.access.ssh.auth.operatorIdentity name your own account for one invocation with --ssh-user"

const reservedEscalationRefusal = "%s sets %q, which is not supported. Bootwright pins [ssh_connection] pipelining = False in ansible/ansible.cfg and leaves use_tty at its default so that ansible-core allocates a controlling terminal for every escalated task. A node whose sudoers sets requiretty refuses sudo without one, so disabling either makes every become task in the run fail on such a node — for every host in the run, not only the ones this targets. Bootwright decides terminal allocation per node from a probe; leave it to Bootwright"

func validateReservedExtraVars(owner string, extraVars map[string]any) []string {
	errs := reservedExtraVarErrors(owner, extraVars, reservedConnectionExtraVars, reservedConnectionRefusal)
	return append(errs, reservedExtraVarErrors(owner, extraVars, reservedEscalationExtraVars, reservedEscalationRefusal)...)
}

func reservedExtraVarErrors(owner string, extraVars map[string]any, reserved map[string]bool, refusal string) []string {
	var keys []string
	for key := range extraVars {
		if reserved[key] {
			keys = append(keys, key)
		}
	}
	if len(keys) == 0 {
		return nil
	}
	sort.Strings(keys)
	errs := make([]string, 0, len(keys))
	for _, key := range keys {
		errs = append(errs, fmt.Sprintf(refusal, owner, key))
	}
	return errs
}
