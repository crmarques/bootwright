//go:build linux && amd64

package bundlelocal

import (
	"context"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

// Setup, preflight, apply, destroy and a power run each meet the execution
// foundation, so its refusal repeats the command that met it, and a context's
// controller stage names its own command instead.
func TestAnExecutionFailureNamesTheCommandThatMetIt(t *testing.T) {
	err := ExecutionGuard{}.WithPython(context.Background(), nil, prerequisites.ExecutionRequirement{},
		func(prerequisites.PythonLaunch, func() error) error { return nil })
	for _, test := range []struct {
		name string
		err  error
		want string
	}{
		{"unscoped", err, "Restore the qualified host execution foundation, then repeat this command."},
		{"the controller stage", prerequisites.InStage(err, "lab"),
			"Restore the qualified host execution foundation, then run bootwright apply --stage controller --context lab."},
	} {
		t.Run(test.name, func(t *testing.T) {
			reported := diagnostics.Of(test.err)
			if len(reported) != 1 || reported[0].Code != "controller.unsupported" || reported[0].Remediation != test.want {
				t.Fatalf("diagnostics = %+v, want one remediation %q", reported, test.want)
			}
		})
	}
}
