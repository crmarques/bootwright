package inventory

import secret "github.com/crmarques/bootwright/internal/secrets"

type PathOptions struct {
	SecretsDir      string
	TrustSecretsDir string
	// SecretIndex resolves secret material paths. It is set from loaded state at
	// the inventory/vars entry points; the zero value resolves every reference to
	// its per-context store path (the common context-local case), so manually
	// constructed PathOptions in tests need not set it unless a fixture declares
	// file-sourced or generated secrets.
	SecretIndex secret.Index
}

func (p PathOptions) trustSecretsDir() string {
	if p.TrustSecretsDir != "" {
		return p.TrustSecretsDir
	}
	return p.SecretsDir
}
