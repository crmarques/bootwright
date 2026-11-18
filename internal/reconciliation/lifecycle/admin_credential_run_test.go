package lifecycle

import (
	"context"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	machineref "github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/secrets"
)

type countingCredentialRunner struct{ calls int }

func (r *countingCredentialRunner) Run(context.Context, RunRequest) (RunResult, error) {
	r.calls++
	return RunResult{}, nil
}

func TestAnAdminCredentialRunPlacedOffTheControllerRefusesAfterReleasingItsBinding(t *testing.T) {
	for name, placement := range map[string]machineref.Placement{
		"no machine": {Connection: machineref.ConnectionLocal},
		"remote":     {Connection: "ssh", Machine: "node-1"},
	} {
		t.Run(name, func(t *testing.T) {
			runner, released := &countingCredentialRunner{}, 0
			call := AdminCredentialCall{Runner: runner, Adapter: "test", Bind: func(_ context.Context, e Execution) (Execution, Invocation, func(), error) {
				return e, Invocation{Placement: placement}, func() { released++ }, nil
			}}
			err := UpdateAdminCredential(context.Background(), call, Execution{}, secrets.Material{})
			if d := diagnostics.Of(err); len(d) != 1 || !refusedBeforeEffect(d[0].Code) || d[0].Message != "the frozen administrator credential request is placed off the controller" || runner.calls != 0 || released != 1 {
				t.Fatalf("an off-controller run returned %+v after %d runs and %d releases", d, runner.calls, released)
			}
		})
	}
}
