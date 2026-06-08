package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestUseControllingTTYForNonInteractiveRootWorkflow(t *testing.T) {
	rootPhase := []Phase{{Name: "base", NeedsRoot: true}}
	if !useControllingTTYForWorkflow(rootPhase, false) {
		t.Fatal("noninteractive root workflow should use a controlling tty")
	}
	if useControllingTTYForWorkflow(rootPhase, true) {
		t.Fatal("password-prompting workflow should keep stdio")
	}
	if useControllingTTYForWorkflow([]Phase{{Name: "check"}}, false) {
		t.Fatal("non-root workflow should not use a controlling tty")
	}
}

func TestWorkflowSummaryReportsRootExecutionAsInfo(t *testing.T) {
	oldCurrentEUID := currentEUID
	currentEUID = func() int { return 0 }
	t.Cleanup(func() { currentEUID = oldCurrentEUID })

	var out bytes.Buffer
	printWorkflowSummary(&out, "Apply plan", []Phase{{Name: "base", NeedsRoot: true}}, false, false, false)
	got := out.String()
	if !strings.Contains(got, "[INFO] Root phases: bootwright is running as root, no BECOME password prompt needed") {
		t.Fatalf("root execution summary missing INFO severity:\n%s", got)
	}
	if strings.Contains(got, "[WARN] Root phases: bootwright is running as root") {
		t.Fatalf("root execution summary must not warn:\n%s", got)
	}
}
