package sshtrust

import (
	"github.com/crmarques/bootwright/api/v1alpha1"
	secret "github.com/crmarques/bootwright/internal/secrets"
)

// MachineKnownHostsPath resolves the SSH host-key trust file a Machine verifies
// against: its explicit per-Machine spec.access.ssh.knownHostsRef secret when
// set, otherwise the managed trust-store known_hosts the caller located for its
// own execution context. render (portable/live inventory vars) and cli (a live
// context) locate that store through different roots, so the managed fallback
// path is a parameter rather than resolved here; only the ref-vs-managed
// decision is shared, which is the part that must not drift between them.
func MachineKnownHostsPath(machine v1alpha1.Machine, idx secret.Index, secretsDir, managedKnownHostsPath string) string {
	if machine.Spec.Access.SSH == nil {
		return ""
	}
	if machine.Spec.Access.SSH.KnownHostsRef.Name != "" {
		return secret.ResolvePath(machine.Spec.Access.SSH.KnownHostsRef.Name, idx, secretsDir)
	}
	return managedKnownHostsPath
}
