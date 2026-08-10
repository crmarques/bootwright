package status

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/converge/workflow"
)

func TestStateCheckStageAddonsRejectsStorageClusterLikeApply(t *testing.T) {
	state := v1alpha1.State{
		StorageClusters: []v1alpha1.StorageCluster{{
			Metadata: v1alpha1.Metadata{Name: "ceph1"},
		}},
	}
	runsDir := t.TempDir()
	ownershipDir := t.TempDir()

	_, err := StateCheck(state, "ceph1", "add-ons", workflow.ApplyTarget{PhaseNames: []string{"add-ons"}}, t.TempDir(), runsDir, ownershipDir, "ctx")
	if err == nil {
		t.Fatalf("state-check --stage add-ons --clusters ceph1 must be rejected like apply")
	}
	if !strings.Contains(err.Error(), "unknown cluster") {
		t.Fatalf("rejection should name the unknown container cluster, got %v", err)
	}
}

func TestStateCheckClusterRootTargetAcceptsStorageCluster(t *testing.T) {
	state := v1alpha1.State{
		StorageClusters: []v1alpha1.StorageCluster{{
			Metadata: v1alpha1.Metadata{Name: "ceph1"},
		}},
	}
	runsDir := t.TempDir()
	ownershipDir := t.TempDir()

	_, err := StateCheck(state, "ceph1", "all", workflow.ApplyTarget{PhaseNames: []string{"fabric", "machines", "deps", "base", "add-ons"}}, t.TempDir(), runsDir, ownershipDir, "ctx")
	if err != nil && strings.Contains(err.Error(), "unknown cluster") {
		t.Fatalf("a cluster-root target must accept the StorageCluster name, got %v", err)
	}
}
