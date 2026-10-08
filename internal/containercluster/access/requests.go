package access

import "github.com/crmarques/bootwright/internal/secrets"

type KubeconfigRequest struct {
	ContextName string
	Name        string
}

// KubeconfigResult is the administrator kubeconfig of one cluster, as the
// context's custody holds it. Material is bounded memory the caller clears.
// Unproved says the copy was kept from an installation whose completion was
// never proved, so the access it grants was not proved either (D124).
type KubeconfigResult struct {
	Context  string
	Cluster  string
	Material secrets.Material
	Unproved bool
}
