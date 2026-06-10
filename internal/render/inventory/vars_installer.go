package inventory

import (
	"github.com/crmarques/bootwright/api/v1alpha1"
	secret "github.com/crmarques/bootwright/internal/secrets"
)

func nodeSSHPrivateKeyPath(env *v1alpha1.Environment, ocp v1alpha1.ContainerCluster, secretsDir string) string {
	if env == nil {
		return ""
	}
	ref := ocp.Spec.Install.NodeSSH.PrivateMaterialRef().Name
	if ref == "" {
		return ""
	}
	spec, ok := env.Spec.Secrets[ref]
	if !ok {
		return ""
	}
	if spec.Generated != nil && spec.Generated.SSHKeyPair != nil {
		return secret.ResolveSSHPrivateKeyPath(ref, env, secretsDir)
	}
	if env.Spec.SecretStorage.Mode == v1alpha1.SecretStorageModeContext && spec.File != "" {
		return secret.ResolveSSHPrivateKeyPath(ref, env, secretsDir)
	}
	if spec.File == "" {
		return ""
	}
	privatePath := secret.ResolveSSHPrivateKeyPath(ref, env, secretsDir)
	publicPath := secret.ResolveSSHPublicKeyPath(ref, env, secretsDir)
	if privatePath == publicPath {
		return ""
	}
	return privatePath
}
