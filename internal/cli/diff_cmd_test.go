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
	// diff shares apply's stage vocabulary (sub-phase drift checks are
	// valid for a read-only command), so a sub-phase must pass stage validation
	// rather than be rejected like destroy rejects it. Isolate HOME so the run
	// fails (if at all) on a missing context, never on stage parsing.
	setTestHomeAndRoot(t)
	for _, stage := range converge.SubPhaseStageNames() {
		_, stderr, _ := runCLI(t, "diff", "--stage", stage)
		if strings.Contains(stderr, "--stage must be one of") {
			t.Fatalf("diff --stage %s rejected a valid sub-phase: %q", stage, stderr)
		}
	}
}

func TestDiffRejectsOverride(t *testing.T) {
	// diff never mutates state, so it carries no --override flag at all;
	// cobra rejects it as unknown.
	stdout, stderr, code := runCLI(t, "diff", "--override")
	if code != 2 {
		t.Fatalf("diff --override exit = %d, want 2 (stderr=%q)", code, stderr)
	}
	if !strings.Contains(stdout+stderr, "unknown flag: --override") {
		t.Fatalf("expected unknown-flag rejection, stdout=%q stderr=%q", stdout, stderr)
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
