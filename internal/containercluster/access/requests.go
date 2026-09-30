package access

import "github.com/crmarques/bootwright/internal/secrets"

type KubeconfigRequest struct {
	ContextName string
	Name        string
}

// KubeconfigResult is the administrator kubeconfig of one cluster, as the
// context's custody holds it. Material is bounded memory the caller clears.
type KubeconfigResult struct {
	Context  string
	Cluster  string
	Material secrets.Material
}
