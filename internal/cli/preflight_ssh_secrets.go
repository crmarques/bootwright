package cli

import (
	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/runtime/secrets"
)

func sshKeyPairFileChecks(req secretRefRequirement, env *v1alpha1.Environment, secretsDir string, deps preflightDeps) []preflightCheck {
	if req.source == secretRefSourceGenerated && req.generatedKind == "sshKeyPair" {
		return generatedSSHKeyPairChecks(req, env, secretsDir, deps)
	}
	contextBacked := req.source == secretRefSourceContext || req.source == secretRefSourceGenerated
	privatePath := secret.ResolveSSHPrivateKeyPath(req.refName, env, secretsDir)
	publicPath := secret.ResolveSSHPublicKeyPath(req.refName, env, secretsDir)
	return []preflightCheck{
		secretFileCheck(req.refName, privatePath, req.label+" private", false, contextBacked, secret.MaterialPathUsesExternalSource(req.refName, env, secret.MaterialSSHPrivate), deps),
		secretFileCheck(req.refName, publicPath, req.label+" public", true, contextBacked, secret.MaterialPathUsesExternalSource(req.refName, env, secret.MaterialSSHPublic), deps),
	}
}

func generatedSSHKeyPairChecks(req secretRefRequirement, env *v1alpha1.Environment, secretsDir string, deps preflightDeps) []preflightCheck {
	privatePath := secret.ResolveSSHPrivateKeyPath(req.refName, env, secretsDir)
	publicPath := secret.ResolveSSHPublicKeyPath(req.refName, env, secretsDir)
	return []preflightCheck{
		generatedSecretCheck(req.refName, privatePath, req.label+" private", "sshKeyPair", deps),
		generatedSecretCheck(req.refName, publicPath, req.label+" public", "sshKeyPair", deps),
	}
}
