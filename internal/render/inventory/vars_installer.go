package inventory

import (
	"github.com/crmarques/bootwright/api/v1alpha1"
	secret "github.com/crmarques/bootwright/internal/secrets"
	stateview "github.com/crmarques/bootwright/internal/state/view"
)

// nodeSSHPrivateKeyPath resolves the installer's local post-install probe key
// for a cluster's node SSH secret, or "" when no private half is locally
// available. Generated key pairs always mint both halves into the store and
// file sources carry both; a context-local secret may hold only the public
// half, so no private-key path is emitted for it.
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
