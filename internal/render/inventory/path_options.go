package inventory

import secret "github.com/crmarques/bootwright/internal/secrets"

type PathOptions struct {
	SecretsDir                  string
	TrustSecretsDir             string
	KubeVirtHostKubeconfigPaths map[string]string
	SecretIndex                 secret.Index
}

func (p PathOptions) trustSecretsDir() string {
	if p.TrustSecretsDir != "" {
		return p.TrustSecretsDir
	}
	return p.SecretsDir
}
