package inventory

type PathOptions struct {
	SecretsDir      string
	TrustSecretsDir string
}

func (p PathOptions) trustSecretsDir() string {
	if p.TrustSecretsDir != "" {
		return p.TrustSecretsDir
	}
	return p.SecretsDir
}
