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

func validateReservedExtraVars(owner string, extraVars map[string]any) []string {
	var reserved []string
	for key := range extraVars {
		if reservedConnectionExtraVars[key] {
			reserved = append(reserved, key)
		}
	}
	if len(reserved) == 0 {
		return nil
	}
	sort.Strings(reserved)
	errs := make([]string, 0, len(reserved))
	for _, key := range reserved {
		errs = append(errs, fmt.Sprintf("%s sets %q, which is not supported. Ansible ranks an extra var above every inventory value, so it silently repoints the identity Bootwright connects and escalates with — the bootwright service account on a machine it installed, the account from Machine spec.access.ssh.user, a storage cluster's spec.ceph.cephadm.clusterSSH.user, their keys, and the recorded host-key trust — and it does so for every host in the run, not only the ones this targets. Declare the login in desired state, or on a machine that declares spec.access.ssh.auth.operatorIdentity name your own account for one invocation with --ssh-user", owner, key))
	}
	return errs
}
