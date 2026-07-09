package inventory

import (
	"github.com/crmarques/bootwright/api/v1alpha1"
	secret "github.com/crmarques/bootwright/internal/secrets"
	stateview "github.com/crmarques/bootwright/internal/state/view"
)

func nodeSSHPrivateKeyPath(state v1alpha1.State, idx secret.Index, ocp v1alpha1.ContainerCluster, secretsDir string) string {
	ref := ocp.Spec.Install.NodeSSH.PrivateMaterialRef().Name
	if ref == "" {
		return ""
	}
	s, ok := stateview.Secret(state, ref)
	if !ok {
		return ""
	}
	switch {
	case s.Spec.Source.Generated != nil:
		return secret.ResolveSSHPrivateKeyPath(ref, idx, secretsDir)
	case s.Spec.Source.File != nil:
		privatePath := secret.ResolveSSHPrivateKeyPath(ref, idx, secretsDir)
		publicPath := secret.ResolveSSHPublicKeyPath(ref, idx, secretsDir)
		if privatePath == publicPath {
			return ""
		}
		return privatePath
	default:
		return ""
	}
}
