package cli

import (
	"testing"
)

func TestUseControllingTTYForNonInteractiveRootWorkflow(t *testing.T) {
	rootPhase := []Phase{{Name: "clusters", NeedsRoot: true}}
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
