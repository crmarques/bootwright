package converge

import (
	"testing"
)

func TestUseControllingTTYForNonInteractiveRootWorkflow(t *testing.T) {
	rootPhase := []Phase{{Name: "base", NeedsRoot: true}}
	if !UseControllingTTYForWorkflow(rootPhase, false) {
		t.Fatal("noninteractive root workflow should use a controlling tty")
	}
	if UseControllingTTYForWorkflow(rootPhase, true) {
		t.Fatal("password-prompting workflow should keep stdio")
	}
	if UseControllingTTYForWorkflow([]Phase{{Name: "check"}}, false) {
		t.Fatal("non-root workflow should not use a controlling tty")
	}
}
