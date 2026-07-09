package preflight

import (
	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infra/locality"
)

type SecretScope struct {
	Machines        map[string]bool
	StorageClusters map[string]bool
}

func (s *SecretScope) allowsMachine(name string) bool {
	return s == nil || name == "" || s.Machines[name]
}

func (s *SecretScope) allowsStorageCluster(name string) bool {
	return s == nil || name == "" || s.StorageClusters[name]
}

type secretRefOwner struct {
	machine        string
	storageCluster string
}

func secretRefChecks(state v1alpha1.State, secretsDir string, selected []Phase, deps Deps) []Check {
	return secretRefChecksWithLocalityPolicy(state, secretsDir, selected, deps, locality.DefaultPolicy, nil)
}

func secretRefChecksScoped(state v1alpha1.State, secretsDir string, selected []Phase, deps Deps, secretScope *SecretScope) []Check {
	return secretRefChecksWithLocalityPolicy(state, secretsDir, selected, deps, locality.DefaultPolicy, secretScope)
}

func stateHasManagedStorageClustersInScope(state v1alpha1.State, secretScope *SecretScope) bool {
	for _, cluster := range state.StorageClusters {
		if v1alpha1.StorageClusterManaged(cluster) && secretScope.allowsStorageCluster(cluster.Metadata.Name) {
			return true
		}
	}
	return false
}
