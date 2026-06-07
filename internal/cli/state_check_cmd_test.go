package cli

import (
	"strings"
	"testing"
)

func TestStateCheckRejectsOverride(t *testing.T) {
	stdout, stderr, code := runCLI(t, "state-check", "--override")
	if code != 2 {
		t.Fatalf("state-check --override exit = %d, want 2 (stderr=%q)", code, stderr)
	}
	if !strings.Contains(stdout+stderr, "--override is not valid for state-check") {
		t.Fatalf("expected override rejection, stdout=%q stderr=%q", stdout, stderr)
	}
}

func TestStateCheckRejectsUnknownOutput(t *testing.T) {
	stdout, stderr, code := runCLI(t, "state-check", "--output", "yaml")
	if code != 2 {
		t.Fatalf("state-check --output yaml exit = %d, want 2", code)
	}
	if !strings.Contains(stdout+stderr, "--output must be") {
		t.Fatalf("expected output-format rejection, stdout=%q stderr=%q", stdout, stderr)
	}
}
