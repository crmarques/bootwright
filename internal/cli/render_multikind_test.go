package cli

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// TestRenderOutputDirClustersAcceptsStorageCluster is the regression guard for
// the multi-kind scope fix: the top-level tool-input render (here the portable
// --input-dir variant) resolves --clusters against BOTH the ContainerCluster and
// StorageCluster name spaces, matching its flag help and `apply`. Before the fix
// it hard-wired the container-cluster target, so naming a StorageCluster failed
// with "unknown cluster(s)" even though the bundle covers storage.
func TestRenderOutputDirClustersAcceptsStorageCluster(t *testing.T) {
	setTestHomeAndRoot(t)
	outputDir := filepath.Join(t.TempDir(), "rendered")

	stdout, stderr, code := runCLI(t, "render",
		"--input-dir", fixturePath("006-ceph-3nodes-libvirt-managed-os"),
		"--output-dir", outputDir,
		"--clusters", "ceph-libvirt",
		"--output", "json",
	)
	if code != 0 {
		t.Fatalf("render --clusters ceph-libvirt exited %d, stderr=%q\nstdout:\n%s", code, stderr, stdout)
	}

	var report renderToolInputsReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("decode render report: %v\nstdout:\n%s", err, stdout)
	}
	if got := storageClusterNames(report.Storage); len(got) != 1 || got[0] != "ceph-libvirt" {
		t.Fatalf("storage-scoped render must render only ceph-libvirt; got %v", got)
	}
	if len(report.Installer) != 0 {
		t.Fatalf("storage-scoped render must render no installer inputs; got %v", report.Installer)
	}
	if report.InventoryPath == "" || report.EffectiveStatePath == "" {
		t.Fatalf("tool-input JSON missing shared artifact paths: %+v", report)
	}
}

// TestRenderOutputDirClustersRejectsUnknown confirms the widened scope still
// rejects a name that is neither a ContainerCluster nor a StorageCluster, and now
// lists the storage roots as available.
func TestRenderOutputDirClustersRejectsUnknown(t *testing.T) {
	setTestHomeAndRoot(t)
	outputDir := filepath.Join(t.TempDir(), "rendered")

	_, stderr, code := runCLI(t, "render",
		"--input-dir", fixturePath("006-ceph-3nodes-libvirt-managed-os"),
		"--output-dir", outputDir,
		"--clusters", "no-such",
	)
	if code == 0 {
		t.Fatal("render --clusters no-such should fail")
	}
	if !strings.Contains(stderr, "unknown cluster(s): no-such") {
		t.Fatalf("stderr missing unknown-cluster diagnostic:\n%s", stderr)
	}
	if !strings.Contains(stderr, "ceph-libvirt") {
		t.Fatalf("unknown-cluster hint must list the available storage root ceph-libvirt:\n%s", stderr)
	}
}

// TestRenderToolInputsInstallerJSON confirms the tool-input JSON projects the
// per-ContainerCluster installer inputs for a container-only bundle.
func TestRenderToolInputsInstallerJSON(t *testing.T) {
	setTestHomeAndRoot(t)
	outputDir := filepath.Join(t.TempDir(), "rendered")

	stdout, stderr, code := runCLI(t, "render",
		"--input-dir", fixturePath("001-sno-libvirt"),
		"--output-dir", outputDir,
		"--output", "json",
	)
	if code != 0 {
		t.Fatalf("render --output json exited %d, stderr=%q\nstdout:\n%s", code, stderr, stdout)
	}
	var report renderToolInputsReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("decode render report: %v\nstdout:\n%s", err, stdout)
	}
	if len(report.Installer) != 1 || report.Installer[0].Name != "sno-libvirt" {
		t.Fatalf("expected installer[sno-libvirt]; got %+v", report.Installer)
	}
	if report.Installer[0].InstallConfigPath == "" || report.Installer[0].AgentConfigPath == "" {
		t.Fatalf("installer JSON missing config paths: %+v", report.Installer[0])
	}
	if len(report.Storage) != 0 {
		t.Fatalf("container-only bundle must carry no storage inputs; got %v", storageClusterNames(report.Storage))
	}
}

// TestRenderStorageJSON exercises `render storage --output json`, the parity gap
// this change closes (its sibling `render installer`/`render effective` already
// had --output json), and with it the shared StorageRenderState scoping.
func TestRenderStorageJSON(t *testing.T) {
	setTestHomeAndRoot(t)
	if _, stderr, code := runCLI(t, "context", "init", "--name", "st", "-f", fixturePath("006-ceph-3nodes-libvirt-managed-os")); code != 0 {
		t.Fatalf("context init exited %d, stderr=%q", code, stderr)
	}

	stdout, stderr, code := runCLI(t, "render", "storage", "--output", "json")
	if code != 0 {
		t.Fatalf("render storage --output json exited %d, stderr=%q\nstdout:\n%s", code, stderr, stdout)
	}
	var report renderStorageReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("decode storage report: %v\nstdout:\n%s", err, stdout)
	}
	if got := storageClusterNames(report.Clusters); len(got) != 1 || got[0] != "ceph-libvirt" {
		t.Fatalf("expected storage[ceph-libvirt]; got %v", got)
	}
	if report.Clusters[0].ApplyScriptPath == "" || report.Clusters[0].BootstrapSpecPath == "" {
		t.Fatalf("storage JSON missing rendered paths: %+v", report.Clusters[0])
	}
}

func storageClusterNames(clusters []renderStorageCluster) []string {
	names := make([]string, 0, len(clusters))
	for _, c := range clusters {
		names = append(names, c.Name)
	}
	return names
}
