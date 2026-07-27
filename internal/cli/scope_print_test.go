package cli

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/converge"
)

func TestConfirmPreservesBufferedStdinAcrossPrompts(t *testing.T) {
	in := strings.NewReader("y\nyes\n")
	if !confirm(in, io.Discard, "first? ") {
		t.Fatal("first confirm should accept 'y'")
	}
	if !confirm(in, io.Discard, "second? ") {
		t.Fatal("second confirm should accept the still-buffered 'yes', not see a spurious EOF")
	}
}

func TestWorkflowSummaryOmitsPasswordPromptExplanations(t *testing.T) {
	oldCurrentEUID := currentEUID
	t.Cleanup(func() { currentEUID = oldCurrentEUID })
	rootPhase := []converge.Phase{{Name: "base", NeedsRoot: true}}

	t.Run("running as root prints no Root phases note", func(t *testing.T) {
		currentEUID = func() int { return 0 }
		var out bytes.Buffer
		printWorkflowSummary(&out, "Stages", rootPhase, false, false, false, true)
		if got := out.String(); strings.Contains(got, "root escalation") {
			t.Fatalf("running as root must not annotate root escalation:\n%s", got)
		}
	})

	t.Run("prompted run does not explain the password prompt", func(t *testing.T) {
		currentEUID = func() int { return 1000 }
		var out bytes.Buffer
		printWorkflowSummary(&out, "Stages", rootPhase, true, false, false, true)
		if got := out.String(); strings.Contains(got, "root escalation") {
			t.Fatalf("prompted run must not explain the password prompt:\n%s", got)
		}
	})

	t.Run("ask-become-pass=false keeps the requirement warning", func(t *testing.T) {
		currentEUID = func() int { return 1000 }
		var out bytes.Buffer
		printWorkflowSummary(&out, "Stages", rootPhase, false, false, false, true)
		if got := out.String(); !strings.Contains(got, "[WARN] root escalation: --ask-become-pass=false requires passwordless sudo") {
			t.Fatalf("ask-become-pass=false must keep the requirement warning:\n%s", got)
		}
	})

	t.Run("dry run keeps the no-execution notice", func(t *testing.T) {
		currentEUID = func() int { return 1000 }
		var out bytes.Buffer
		printWorkflowSummary(&out, "Stages", rootPhase, true, true, false, true)
		if got := out.String(); !strings.Contains(got, "[WARN] root escalation: this run needs sudo; a dry run executes nothing") {
			t.Fatalf("dry run must keep the no-execution notice:\n%s", got)
		}
	})
}
