package status

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/converge/workflow"
)

// TestStateCheckStageAddonsRejectsStorageClusterLikeApply pins M11: state-check
// threads the resolved stage target (e.g. "add-ons") into clusteraccess.Resolve
// instead of a hardcoded "all". add-ons is a container-cluster-scoped target, so
// a StorageCluster name is not a valid --clusters value there — apply rejects it
// and state-check must reject it identically. Under the hardcoded "all" the same
// storage name was wrongly accepted.
func TestStateCheckStageAddonsRejectsStorageClusterLikeApply(t *testing.T) {
	state := v1alpha1.State{
		StorageClusters: []v1alpha1.StorageCluster{{
			Metadata: v1alpha1.Metadata{Name: "ceph1"},
		}},
	}
	runsDir := t.TempDir()
	ownershipDir := t.TempDir()

	_, err := StateCheck(state, "ceph1", "add-ons", workflow.ApplyTarget{PhaseNames: []string{"add-ons"}}, runsDir, ownershipDir, "ctx")
	if err == nil {
		t.Fatalf("state-check --stage add-ons --clusters ceph1 must be rejected like apply")
	}
	if !strings.Contains(err.Error(), "unknown cluster") {
		t.Fatalf("rejection should name the unknown container cluster, got %v", err)
	}
}

// TestStateCheckClusterRootTargetAcceptsStorageCluster is the accept side of the
// same parity: a cluster-root target ("all") does accept a StorageCluster name,
// so the resolved-target threading is what differentiates the two — the storage
// name is never rejected as unknown under a root-scoped target.
func TestStateCheckClusterRootTargetAcceptsStorageCluster(t *testing.T) {
	state := v1alpha1.State{
		StorageClusters: []v1alpha1.StorageCluster{{
			Metadata: v1alpha1.Metadata{Name: "ceph1"},
		}},
	}
	runsDir := t.TempDir()
	ownershipDir := t.TempDir()

	_, err := StateCheck(state, "ceph1", "all", workflow.ApplyTarget{PhaseNames: []string{"fabric", "machines", "deps", "base", "add-ons"}}, runsDir, ownershipDir, "ctx")
	if err != nil && strings.Contains(err.Error(), "unknown cluster") {
		t.Fatalf("a cluster-root target must accept the StorageCluster name, got %v", err)
	}
}
