package runconfig

import (
	"path/filepath"
	"slices"
	"testing"
)

func TestNewRunSpecAddsArtifactsDirExtraVar(t *testing.T) {
	root := t.TempDir()
	artifactsDir := filepath.Join(root, "artifacts")
	spec, err := NewRunSpec(RunSpecConfig{
		BundleDir:          filepath.Join(root, "bundle"),
		RenderedDir:        filepath.Join(root, "rendered"),
		ClustersDir:        filepath.Join(root, "clusters"),
		RunsDir:            filepath.Join(root, "runs"),
		SecretsDir:         filepath.Join(root, "secrets"),
		ManagedServicesDir: filepath.Join(root, "managed-services"),
		ProviderStateDir:   filepath.Join(root, "provider-state"),
		OwnershipDir:       filepath.Join(root, "ownership"),
		InventoryPath:      filepath.Join(root, "inventory.yaml"),
		VarsPath:           filepath.Join(root, "vars.yaml"),
		Playbook:           "bootwright.core.task_storage_cluster_apply",
		ArtifactsDir:       artifactsDir,
	})
	if err != nil {
		t.Fatalf("NewRunSpec: %v", err)
	}
	artifactsDirAbs, err := filepath.Abs(artifactsDir)
	if err != nil {
		t.Fatalf("Abs: %v", err)
	}
	if spec.ArtifactsDir != artifactsDirAbs {
		t.Fatalf("ArtifactsDir = %q, want %q", spec.ArtifactsDir, artifactsDirAbs)
	}
	if !slices.Contains(spec.ExtraVarPairs, "bootwright_ansible_artifacts_dir="+artifactsDirAbs) {
		t.Fatalf("ExtraVarPairs missing artifacts dir: %v", spec.ExtraVarPairs)
	}
	ownershipDirAbs, err := filepath.Abs(filepath.Join(root, "ownership"))
	if err != nil {
		t.Fatalf("Abs: %v", err)
	}
	if !slices.Contains(spec.ExtraVarPairs, "bootwright_ownership_dir="+ownershipDirAbs) {
		t.Fatalf("ExtraVarPairs missing ownership dir: %v", spec.ExtraVarPairs)
	}
}
