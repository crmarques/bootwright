package preflight

import (
	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infra/locality"
)

// SecretScope restricts secret-material and storage-tool readiness to the
// objects a scoped run actually acts on. A scoped --clusters apply pulls a
// managed StorageCluster (and its nodes) into the plan state only as a render
// reference for a container cluster's data-foundation attachment; that
// reference must not require its own bootstrap secrets, ceph entitlement, or
// cephadm SSH/scp tooling. Machines and StorageClusters hold the names that are
// genuine work targets. A nil *SecretScope means no narrowing — every declared
// object's secrets are required (standalone preflight/check and unscoped apply).
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

// secretRefOwner identifies the work object a secret requirement belongs to, so
// a scoped run can drop secrets owned by render-reference pull-ins. At most one
// field is set; an empty owner is always in scope (environment-global material,
// container-cluster install secrets, and the data-foundation attachment SSH the
// consuming cluster, not the storage cluster, drives).
type secretRefOwner struct {
	machine        string
	storageCluster string
}

func secretRefChecks(state v1alpha1.State, secretsDir string, selected []Phase, deps Deps) []Check {
	return secretRefChecksWithLocalityPolicy(state, secretsDir, selected, deps, locality.DefaultPolicy, nil)
}

// secretRefChecksScoped is the apply-path entry that narrows secret-material
// checks to the run's work objects; CollectChecks threads the SecretScope here.
func secretRefChecksScoped(state v1alpha1.State, secretsDir string, selected []Phase, deps Deps, secretScope *SecretScope) []Check {
	return secretRefChecksWithLocalityPolicy(state, secretsDir, selected, deps, locality.DefaultPolicy, secretScope)
}

// stateHasManagedStorageClustersInScope reports whether the run will provision a
// managed StorageCluster. secretScope, when non-nil, excludes clusters present
// only as render references so a container-cluster apply that merely attaches to
// external Ceph does not pull in cephadm SSH/scp tooling.
func stateHasManagedStorageClustersInScope(state v1alpha1.State, secretScope *SecretScope) bool {
	for _, cluster := range state.StorageClusters {
		if v1alpha1.StorageClusterManaged(cluster) && secretScope.allowsStorageCluster(cluster.Metadata.Name) {
			return true
		}
	}
	return false
}
