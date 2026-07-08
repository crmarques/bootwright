package preflight

import (
	secret "github.com/crmarques/bootwright/internal/secrets"
)

func sshKeyPairFileChecks(req secretRefRequirement, idx secret.Index, secretsDir string, deps Deps) []Check {
	if req.source == secretRefSourceGenerated && req.generatedKind == "sshKeyPair" {
		return generatedSSHKeyPairChecks(req, idx, secretsDir, deps)
	}
	contextBacked := req.source == secretRefSourceContext || req.source == secretRefSourceGenerated
	privatePath := secret.ResolveSSHPrivateKeyPath(req.refName, idx, secretsDir)
	publicPath := secret.ResolveSSHPublicKeyPath(req.refName, idx, secretsDir)
	return []Check{
		secretFileCheck(req.refName, privatePath, req.label+" private", false, contextBacked, secret.MaterialPathUsesExternalSource(req.refName, idx, secret.MaterialSSHPrivate), deps),
		secretFileCheck(req.refName, publicPath, req.label+" public", true, contextBacked, secret.MaterialPathUsesExternalSource(req.refName, idx, secret.MaterialSSHPublic), deps),
	}
}

func generatedSSHKeyPairChecks(req secretRefRequirement, idx secret.Index, secretsDir string, deps Deps) []Check {
	privatePath := secret.ResolveSSHPrivateKeyPath(req.refName, idx, secretsDir)
	publicPath := secret.ResolveSSHPublicKeyPath(req.refName, idx, secretsDir)
	return []Check{
		generatedSecretCheck(req.refName, privatePath, req.label+" private", "sshKeyPair", deps),
		generatedSecretCheck(req.refName, publicPath, req.label+" public", "sshKeyPair", deps),
	}
}
