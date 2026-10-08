package prerequisites

import (
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

// A scoped failure that names no command of its own is setup's, so every
// failure raised before Command existed keeps setup's retry, and one that
// names a command ends with it when read without a scope.
func TestAnUnscopedFailureNamesItsCommandOrSetup(t *testing.T) {
	for _, test := range []struct {
		failure *ScopedFailure
		want    string
	}{
		{&ScopedFailure{Message: "m", Correction: "Restore it"}, "Restore it, then rerun bootwright setup."},
		{&ScopedFailure{Message: "m", Correction: "Restore it", Command: "repeat this command"}, "Restore it, then repeat this command."},
	} {
		reported := diagnostics.Of(test.failure)
		if len(reported) != 1 || reported[0].Code != "controller.setup" || reported[0].Remediation != test.want {
			t.Fatalf("diagnostics = %+v, want %q", reported, test.want)
		}
	}
}
