package cli

import (
	"strings"
	"testing"
)

func TestApplyMachinesAndClustersMutuallyExclusive(t *testing.T) {
	_, stderr, code := runCLI(t, "apply", "--machines", "master-0", "--clusters", "3-nodes-ocp-libvirt", "--dry-run")
	if code != 2 {
		t.Fatalf("apply --machines+--clusters exited %d, want 2; stderr=%q", code, stderr)
	}
	if !strings.Contains(stderr, "--machines and --clusters are mutually exclusive") {
		t.Fatalf("apply --machines+--clusters stderr = %q, want mutual-exclusion message", stderr)
	}
}

func TestApplyMachinesRejectsClusterStage(t *testing.T) {
	_, stderr, code := runCLI(t, "apply", "--machines", "master-0", "--stage", "clusters", "--dry-run")
	if code != 2 {
		t.Fatalf("apply --machines --stage clusters exited %d, want 2; stderr=%q", code, stderr)
	}
	if !strings.Contains(stderr, "only the fabric and machines phases") {
		t.Fatalf("apply --machines --stage clusters stderr = %q, want machine-layer message", stderr)
	}
}

func TestPlanMachinesUnknownMachine(t *testing.T) {
	initTestContext(t, "003-3nodes-libvirt")
	_, stderr, code := runCLI(t, "plan", "--machines", "not-a-machine")
	if code != 1 {
		t.Fatalf("plan --machines not-a-machine exited %d, want 1; stderr=%q", code, stderr)
	}
	if !strings.Contains(stderr, "unknown machine(s): not-a-machine") || !strings.Contains(stderr, "available:") {
		t.Fatalf("plan --machines not-a-machine stderr = %q, want unknown-machine hint", stderr)
	}
}

func TestPlanMachinesSelectsSingleNode(t *testing.T) {
	initTestContext(t, "003-3nodes-libvirt")
	stdout, stderr, code := runCLI(t, "plan", "--machines", "master-0")
	if code != 0 {
		t.Fatalf("plan --machines master-0 exited %d, want 0; stderr=%q", code, stderr)
	}
	if !strings.Contains(stdout, "master-0") {
		t.Fatalf("plan --machines master-0 stdout did not mention master-0:\n%s", stdout)
	}
	if strings.Contains(stdout, "master-1") || strings.Contains(stdout, "master-2") {
		t.Fatalf("plan --machines master-0 should not provision sibling nodes:\n%s", stdout)
	}
}
