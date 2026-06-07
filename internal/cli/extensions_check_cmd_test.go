package cli

import (
	"strings"
	"testing"
)

func TestCheckAddonsRejectsUnknownOutput(t *testing.T) {
	stdout, stderr, code := runCLI(t, "check", "addons", "--output", "yaml")
	if code != 2 {
		t.Fatalf("check addons --output yaml exit = %d, want 2 (stderr=%q)", code, stderr)
	}
	if !strings.Contains(stdout+stderr, "--output must be") {
		t.Fatalf("expected output-format rejection, stdout=%q stderr=%q", stdout, stderr)
	}
}
