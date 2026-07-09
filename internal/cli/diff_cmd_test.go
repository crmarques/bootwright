package cli

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/converge"
)

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
