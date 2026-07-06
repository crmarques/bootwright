package desiredstate

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Excluded clusters skip Validate entirely, so the ghost objects below stay
// deliberately minimal: selection must drop them before validation would
// reject them.
const ghostStorageClusterYAML = `apiVersion: bootwright.io/v1alpha1
kind: StorageCluster
metadata:
  name: ghost-ceph
spec:
  type: ceph
`

const ghostContainerClusterYAML = `apiVersion: bootwright.io/v1alpha1
kind: ContainerCluster
metadata:
  name: ghost-ocp
spec: {}
`

func TestLoadReportsClustersExcludedByEnvironmentSelection(t *testing.T) {
	source := copyClusterSelectionFixture(t)
	appendToEnvironmentSpec(t, source, "  containerClusters:\n    - sno-libvirt\n")
	writeClusterSelectionFile(t, source, "ghost-storage-cluster.yaml", ghostStorageClusterYAML)
	writeClusterSelectionFile(t, source, "ghost-container-cluster.yaml", ghostContainerClusterYAML)

	state, exclusions, err := LoadNormalizeValidateWithExclusions([]string{source})
	if err != nil {
		t.Fatalf("LoadNormalizeValidateWithExclusions: %v", err)
	}
	if got, want := exclusions.ContainerClusters, []string{"ghost-ocp"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("excluded container clusters = %v, want %v", got, want)
	}
	if got, want := exclusions.StorageClusters, []string{"ghost-ceph"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("excluded storage clusters = %v, want %v", got, want)
	}
	if exclusions.Empty() {
		t.Fatal("exclusions with excluded clusters report Empty")
	}
	if len(state.ContainerClusters) != 1 || state.ContainerClusters[0].Metadata.Name != "sno-libvirt" {
		t.Fatalf("selected state container clusters = %+v, want only sno-libvirt", state.ContainerClusters)
	}
	if len(state.StorageClusters) != 0 {
		t.Fatalf("selected state keeps %d storage cluster(s), want none", len(state.StorageClusters))
	}
}

func TestLoadReportsNoExclusionsWhenSelectionCoversEveryCluster(t *testing.T) {
	source := copyClusterSelectionFixture(t)
	appendToEnvironmentSpec(t, source, "  containerClusters:\n    - sno-libvirt\n")

	_, exclusions, err := LoadNormalizeValidateWithExclusions([]string{source})
	if err != nil {
		t.Fatalf("LoadNormalizeValidateWithExclusions: %v", err)
	}
	if !exclusions.Empty() {
		t.Fatalf("exclusions = %+v, want none", exclusions)
	}
}

func TestLoadReportsNoExclusionsWithoutEnvironmentSelection(t *testing.T) {
	source := copyClusterSelectionFixture(t)

	_, exclusions, err := LoadNormalizeValidateWithExclusions([]string{source})
	if err != nil {
		t.Fatalf("LoadNormalizeValidateWithExclusions: %v", err)
	}
	if !exclusions.Empty() {
		t.Fatalf("exclusions = %+v, want none", exclusions)
	}
}

// TestEnvironmentResourceDirectorySkipsAnsibleContent covers mc#2: a selected
// resource directory must skip operator Ansible content (playbooks/roles/
// collections) exactly like plain discovery does, so the same tree does not
// validate under discovery yet die under spec.resources selection on a
// playbook's top-level sequence.
func TestEnvironmentResourceDirectorySkipsAnsibleContent(t *testing.T) {
	dir := t.TempDir()
	files := newBaselineFiles()
	delete(files, "environment.yaml")
	nested := map[string]string{
		"environment.yaml": newEnvironmentYAMLWithResources("infra"),
		// A playbook is a top-level YAML sequence with no apiVersion; strict
		// decode would reject it, so the walk must skip the ansible-content dir.
		"infra/playbooks/site.yml": "- hosts: all\n  tasks: []\n",
	}
	for name, content := range files {
		nested["infra/"+name] = content
	}
	writeFiles(t, dir, nested)
	if _, err := LoadNormalizeValidate([]string{dir}); err != nil {
		t.Fatalf("resource dir with ansible content should validate: %v", err)
	}
}

// TestKnownResourceKindCoversRegistry covers go-architecture#2: knownResourceKind
// must accept every authored kind (it is derived from AuthoredKindAccessors),
// including the two the old hand-enumerated switch had already dropped.
func TestKnownResourceKindCoversRegistry(t *testing.T) {
	for _, kind := range supportedKinds() {
		if !knownResourceKind(kind) {
			t.Fatalf("knownResourceKind(%q) = false, want true", kind)
		}
	}
	if knownResourceKind("Bogus") {
		t.Fatal("knownResourceKind(\"Bogus\") = true, want false")
	}
}

// TestUnsupportedKindErrorListsSupported covers api-author-ux#5: a kind typo
// error must name the accepted kinds so a casing/plural mistake is visible.
func TestUnsupportedKindErrorListsSupported(t *testing.T) {
	dir := t.TempDir()
	writeClusterSelectionFile(t, dir, "bad.yaml", "apiVersion: bootwright.io/v1alpha1\nkind: Machin\nmetadata: { name: x }\nspec: {}\n")
	_, err := Load([]string{dir})
	if err == nil {
		t.Fatal("expected unsupported kind error, got nil")
	}
	for _, want := range []string{`unsupported kind "Machin"`, "supported kinds:", "Machine"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not contain %q", err, want)
		}
	}
}

func copyClusterSelectionFixture(t *testing.T) string {
	t.Helper()
	source := filepath.Join("..", "..", "..", "test", "e2e", "001-sno-libvirt")
	target := t.TempDir()
	entries, err := os.ReadDir(source)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(source, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		writeClusterSelectionFile(t, target, entry.Name(), string(data))
	}
	return target
}

func appendToEnvironmentSpec(t *testing.T, dir, fragment string) {
	t.Helper()
	path := filepath.Join(dir, "environment.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, []byte(fragment)...), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeClusterSelectionFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
