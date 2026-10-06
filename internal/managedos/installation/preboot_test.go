package installation

import (
	"context"
	"reflect"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/substrate"
)

// An apply names to its runner every refusal its target's pre-boot proof may
// make, each with the diagnostic that names this Machine, its controller and
// the remedy, so a refused proof reaches the operator as that diagnostic
// rather than as an adapter failure. A removal and an observation boot
// nothing, so they name none.
func TestAnApplyRemediesItsTargetsPreBootRefusalsByName(t *testing.T) {
	for _, arm := range []string{substrate.ArmLibvirt, substrate.ArmBaremetal} {
		for _, operation := range []string{"apply", "destroy", "observe"} {
			t.Run(arm+" "+operation, func(t *testing.T) {
				call, request := execution(t, "digest")
				request.Target.Substrate, request.Target.Physical = arm, arm == substrate.ArmBaremetal
				call.Proved, call.Context = dependencyProof(), "lab-b"
				marker, _ := MarkerFor(request, "digest")
				runner := &fakeRunner{}
				capability := New(nil).WithIdentities(&pinReader{})
				capability.runner = runner
				if _, err := capability.run(context.Background(), call, operation, request, marker, ""); err != nil {
					t.Fatalf("run: %v", diagnostics.Of(err))
				}
				got := map[string][]diagnostics.Diagnostic{}
				for reason, err := range runner.requests[0].Refusals {
					got[reason] = diagnostics.Of(err)
				}
				want := map[string][]diagnostics.Diagnostic{}
				if operation == "apply" {
					for reason, err := range substrate.PreBootRefusals(arm, "lab-b", "rhel-01", request.Target.Controller.Endpoint) {
						want[reason] = diagnostics.Of(err)
					}
					if len(want[substrate.RefusalMachineRunning]) != 1 || request.Target.Controller.Endpoint == "" {
						t.Fatalf("the %s proof names %v at %q", arm, want, request.Target.Controller.Endpoint)
					}
				}
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("the run names %#v, want %#v", got, want)
				}
			})
		}
	}
}
