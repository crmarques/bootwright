package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/converge"
)

func TestWorkflowSummaryOmitsPasswordPromptExplanations(t *testing.T) {
	oldCurrentEUID := currentEUID
	t.Cleanup(func() { currentEUID = oldCurrentEUID })
	rootPhase := []converge.Phase{{Name: "base", NeedsRoot: true}}

	t.Run("running as root prints no Root phases note", func(t *testing.T) {
		currentEUID = func() int { return 0 }
		var out bytes.Buffer
		printWorkflowSummary(&out, "Apply plan", rootPhase, false, false, false)
		if got := out.String(); strings.Contains(got, "Root phases") {
			t.Fatalf("running as root must not annotate Root phases:\n%s", got)
		}
	})

	t.Run("prompted run does not explain the password prompt", func(t *testing.T) {
		currentEUID = func() int { return 1000 }
		var out bytes.Buffer
		printWorkflowSummary(&out, "Apply plan", rootPhase, true, false, false)
		if got := out.String(); strings.Contains(got, "Root phases") {
			t.Fatalf("prompted run must not explain the password prompt:\n%s", got)
		}
	})

	t.Run("ask-become-pass=false keeps the requirement warning", func(t *testing.T) {
		currentEUID = func() int { return 1000 }
		var out bytes.Buffer
		printWorkflowSummary(&out, "Apply plan", rootPhase, false, false, false)
		if got := out.String(); !strings.Contains(got, "[WARN] Root phases: --ask-become-pass=false requires passwordless sudo") {
			t.Fatalf("ask-become-pass=false must keep the requirement warning:\n%s", got)
		}
	})

	t.Run("dry run keeps the no-execution notice", func(t *testing.T) {
		currentEUID = func() int { return 1000 }
		var out bytes.Buffer
		printWorkflowSummary(&out, "Apply plan", rootPhase, true, true, false)
		if got := out.String(); !strings.Contains(got, "[WARN] Root phases: sudo escalation is required; this is a dry run") {
			t.Fatalf("dry run must keep the no-execution notice:\n%s", got)
		}
	})
}
