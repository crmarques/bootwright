package runconfig

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
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

func TestNewRunSpecAppendsVendoredRolesAndCollections(t *testing.T) {
	root := t.TempDir()
	bundleDir := filepath.Join(root, "bundle")
	absPlaybook := filepath.Join(root, "input", "playbooks", "harden.yml")
	spec, err := NewRunSpec(RunSpecConfig{
		BundleDir:          bundleDir,
		RenderedDir:        filepath.Join(root, "rendered"),
		ClustersDir:        filepath.Join(root, "clusters"),
		RunsDir:            filepath.Join(root, "runs"),
		SecretsDir:         filepath.Join(root, "secrets"),
		ManagedServicesDir: filepath.Join(root, "managed-services"),
		ProviderStateDir:   filepath.Join(root, "provider-state"),
		OwnershipDir:       filepath.Join(root, "ownership"),
		InventoryPath:      filepath.Join(root, "inventory.yaml"),
		VarsPath:           filepath.Join(root, "vars.yaml"),
		Playbook:           absPlaybook,
		RolesPath:          filepath.Join(root, "input", "roles"),
		CollectionsPath:    filepath.Join(root, "input", "collections"),
		ArtifactsDir:       filepath.Join(root, "artifacts"),
	})
	if err != nil {
		t.Fatalf("NewRunSpec: %v", err)
	}
	if spec.Playbook != absPlaybook {
		t.Fatalf("Playbook = %q, want absolute passthrough %q", spec.Playbook, absPlaybook)
	}
	bundleCollections := filepath.Join(bundleDir, "collections")
	wantCollections := bundleCollections + string(os.PathListSeparator) + filepath.Join(root, "input", "collections")
	if spec.CollectionsPath != wantCollections {
		t.Fatalf("CollectionsPath = %q, want %q", spec.CollectionsPath, wantCollections)
	}
	if !strings.HasPrefix(spec.CollectionsPath, bundleCollections) {
		t.Fatalf("CollectionsPath must start with the bundle collections: %q", spec.CollectionsPath)
	}
	if spec.RolesPath != filepath.Join(root, "input", "roles") {
		t.Fatalf("RolesPath = %q", spec.RolesPath)
	}
}
