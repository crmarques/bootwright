package cli

import (
	"bytes"
	"strings"
	"testing"

	cliout "github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/converge"
	"github.com/crmarques/bootwright/internal/converge/workflow"
)

func TestDiffOrphanRemedyScopedToSweepCoverage(t *testing.T) {
	var buf bytes.Buffer
	printStateCheckOrphans(cliout.New(&buf), []workflow.UndeclaredResource{
		{Kind: "libvirt-domain", Name: "vm-a"},
		{Kind: "kubevirt-machine", Name: "vm-b", Cluster: "hub1"},
	})
	out := buf.String()
	if !strings.Contains(out, "libvirt-domain/vm-a") || !strings.Contains(out, "a full `bootwright destroy` reclaims it") {
		t.Fatalf("sweep-reclaimable orphan should promise reclaim:\n%s", out)
	}
	if !strings.Contains(out, "kubevirt-machine/vm-b") || !strings.Contains(out, "does not reclaim this record") {
		t.Fatalf("non-sweep orphan must not be pointed at a full destroy that leaves it standing:\n%s", out)
	}
	if strings.Contains(out, "run `bootwright destroy` to reclaim them") {
		t.Fatalf("the blanket destroy remedy overpromises for non-sweep kinds and must be gone:\n%s", out)
	}
}

func TestDiffRejectsUnknownStage(t *testing.T) {
	_, stderr, code := runCLI(t, "diff", "--stage", "bogus")
	if code != 2 {
		t.Fatalf("diff --stage bogus exit = %d, want 2 (stderr=%q)", code, stderr)
	}
	if !strings.Contains(stderr, "--stage must be one of") {
		t.Fatalf("diff --stage bogus stderr = %q", stderr)
	}
}

func TestDiffAcceptsSubPhaseStages(t *testing.T) {
	setTestHomeAndRoot(t)
	for _, stage := range converge.SubPhaseStageNames() {
		_, stderr, _ := runCLI(t, "diff", "--stage", stage)
		if strings.Contains(stderr, "--stage must be one of") {
			t.Fatalf("diff --stage %s rejected a valid sub-phase: %q", stage, stderr)
		}
	}
}

func TestDiffRejectsOverride(t *testing.T) {
	stdout, stderr, code := runCLI(t, "diff", "--override")
	if code != 2 {
		t.Fatalf("diff --override exit = %d, want 2 (stderr=%q)", code, stderr)
	}
	if !strings.Contains(stdout+stderr, "unknown flag: --override") {
		t.Fatalf("expected unknown-flag rejection, stdout=%q stderr=%q", stdout, stderr)
	}
}

func TestDiffAdoptRejectsRecorded(t *testing.T) {
	_, stderr, code := runCLI(t, "diff", "--recorded", "--adopt")
	if code != 2 {
		t.Fatalf("diff --recorded --adopt exit = %d, want 2 (stderr=%q)", code, stderr)
	}
	if !strings.Contains(stderr, "--adopt requires live discovery") {
		t.Fatalf("stderr missing the adopt/recorded conflict: %q", stderr)
	}
}

func TestDiffRejectsUnknownOutput(t *testing.T) {
	stdout, stderr, code := runCLI(t, "diff", "--output", "yaml")
	if code != 2 {
		t.Fatalf("diff --output yaml exit = %d, want 2", code)
	}
	if !strings.Contains(stdout+stderr, "--output must be") {
		t.Fatalf("expected output-format rejection, stdout=%q stderr=%q", stdout, stderr)
	}
}
