package sshtrust

import (
	"github.com/crmarques/bootwright/api/v1alpha1"
	secret "github.com/crmarques/bootwright/internal/secrets"
)

func MachineKnownHostsPath(machine v1alpha1.Machine, idx secret.Index, secretsDir, managedKnownHostsPath string) string {
	if machine.Spec.Access.SSH == nil {
		return ""
	}
	if machine.Spec.Access.SSH.KnownHostsRef.Name != "" {
		return secret.ResolvePath(machine.Spec.Access.SSH.KnownHostsRef.Name, idx, secretsDir)
	}
	return managedKnownHostsPath
}
