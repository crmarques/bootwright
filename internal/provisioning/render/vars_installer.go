package render

import (
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/secret"
)

func clusterAdminSSHPrivateKeyPath(env *v1alpha1.Environment, ocp v1alpha1.ContainerCluster, secretsDir string) string {
	if env == nil {
		return ""
	}
	ref := ocp.Spec.Install.SSHKeyRef.Name
	if ref == "" {
		return ""
	}
	spec, ok := env.Spec.Secrets[ref]
	if !ok || spec.File == "" {
		return ""
	}
	pubPath := secret.ResolvePath(ref, env, secretsDir)
	if !strings.HasSuffix(pubPath, ".pub") {
		return ""
	}
	return strings.TrimSuffix(pubPath, ".pub")
}
