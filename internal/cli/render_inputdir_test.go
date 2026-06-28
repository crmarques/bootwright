package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRenderInputDirRendersPortableBundleWithoutContext exercises the
// context-free render end to end: no `context init`/`use` runs first, yet
// `render --input-dir <dir> --output-dir <dir>` produces the full tool-input
// bundle with {{ secret }} placeholders and no inlined material.
func TestRenderInputDirRendersPortableBundleWithoutContext(t *testing.T) {
	setTestHomeAndRoot(t)
	outputDir := filepath.Join(t.TempDir(), "rendered")

	stdout, stderr, code := runCLI(t, "render", "--input-dir", fixturePath("001-sno-libvirt"), "--output-dir", outputDir)
	if code != 0 {
		t.Fatalf("render --input-dir exited %d, stderr=%q\nstdout:\n%s", code, stderr, stdout)
	}

	installConfig := filepath.Join(outputDir, "openshift-install", "sno-libvirt", "install-config.yaml")
	for _, path := range []string{
		filepath.Join(outputDir, "effective-state.yaml"),
		filepath.Join(outputDir, "bootwright.lock.yaml"),
		filepath.Join(outputDir, "ansible", "inventory.yaml"),
		filepath.Join(outputDir, "ansible", "vars.yaml"),
		installConfig,
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected rendered file %s: %v\nstdout:\n%s", path, err, stdout)
		}
	}

	data, err := os.ReadFile(installConfig)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "{{ secret ") {
		t.Fatalf("portable install-config carries no {{ secret }} placeholder:\n%s", data)
	}
	if strings.Contains(string(data), "<bootwright-secret-ref:") || strings.Contains(string(data), "bootwright-secret-ref:") {
		t.Fatalf("portable install-config leaked the context placeholder dialect:\n%s", data)
	}
	if !strings.Contains(stdout, "portable render") || !strings.Contains(stdout, "substitute every {{ secret <name> }} token") {
		t.Fatalf("stdout missing portable render summary:\n%s", stdout)
	}
}

func TestRenderInputDirRequiresOutputDir(t *testing.T) {
	setTestHomeAndRoot(t)
	_, stderr, code := runCLI(t, "render", "--input-dir", fixturePath("001-sno-libvirt"))
	if code == 0 {
		t.Fatal("render --input-dir without --output-dir should fail")
	}
	if !strings.Contains(stderr, "requires --output-dir") {
		t.Fatalf("stderr missing --output-dir guidance:\n%s", stderr)
	}
}

func TestRenderInputDirRejectsSensitive(t *testing.T) {
	setTestHomeAndRoot(t)
	outputDir := filepath.Join(t.TempDir(), "rendered")
	_, stderr, code := runCLI(t, "render", "--input-dir", fixturePath("001-sno-libvirt"), "--output-dir", outputDir, "--sensitive")
	if code == 0 {
		t.Fatal("render --input-dir --sensitive should fail")
	}
	if !strings.Contains(stderr, "not applicable") {
		t.Fatalf("stderr missing --sensitive rejection:\n%s", stderr)
	}
}

// TestRenderInputDirStaysRootless pins the root-gate carve-out: a context-free
// render reads/writes user-owned dirs and must never escalate, even though it
// carries --output-dir (which is an execution target in the context mode).
func TestRenderInputDirStaysRootless(t *testing.T) {
	cases := [][]string{
		{"render", "--input-dir", "/in", "--output-dir", "/out"},
		{"render", "--input-dir=/in", "--output-dir=/out"},
		{"--context", "lab", "render", "--input-dir", "/in", "--output-dir", "/out"},
	}
	for _, args := range cases {
		if argsNeedLocalRoot(stripLeadingGlobalFlags(args)) {
			t.Errorf("%v should stay rootless", args)
		}
	}
	// The context-bound --output-dir path still escalates.
	if !argsNeedLocalRoot([]string{"render", "--output-dir", "/out"}) {
		t.Fatal("context render --output-dir should be rootful")
	}
}
