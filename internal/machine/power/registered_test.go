package power

import (
	"context"
	"testing"

	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/substrate"
)

func TestEveryCapabilityDeadlineCoversItsFrozenBudgets(t *testing.T) {
	e := lifecycle.Execution{Context: "lab", Operation: "destroy", Attempt: 1, Block: reconciliation.Block{BlockDefinition: reconciliation.BlockDefinition{ID: "install"}}}
	for _, arm := range substrate.Realized() {
		bounds, _ := substrate.MachineBoundsFor(arm)
		for _, verb := range []Verb{Start, Stop} {
			runner := &adapter{power: expected(verb)}
			r := Request{Arm: arm, Identity: Identity{Context: "lab", Object: "guest"}, Version: RequestVersion, Verb: verb}
			if err := DriveRegistered(context.Background(), runner, e, r, machine.HardwareIdentity{UUID: "guest"}, HostIdentity{UUID: "controller"}); err != nil {
				t.Fatal(err)
			}
			if runner.seen.Deadline < bounds.StateRead*2+bounds.Power+bounds.PreBoot+runOverhead {
				t.Errorf("%s %s deadline %s omits media, identity, convergence or settlement", arm, verb, runner.seen.Deadline)
			}
		}
	}
}

func TestRegisteredPowerRefusesWithoutAnEffectfulAttemptAndOriginalTarget(t *testing.T) {
	e := lifecycle.Execution{Context: "lab", Operation: "destroy", Attempt: 1, Block: reconciliation.Block{BlockDefinition: reconciliation.BlockDefinition{ID: "install"}}}
	r := Request{Arm: substrate.ArmBaremetal, Identity: Identity{Context: "lab", Object: "guest"}, Version: RequestVersion, Verb: Start}
	for _, invalid := range []func(*lifecycle.Execution, *Request, *machine.HardwareIdentity, *HostIdentity){
		func(e *lifecycle.Execution, _ *Request, _ *machine.HardwareIdentity, _ *HostIdentity) { e.Attempt = 0 },
		func(e *lifecycle.Execution, _ *Request, _ *machine.HardwareIdentity, _ *HostIdentity) {
			e.Resolution = 1
		},
		func(_ *lifecycle.Execution, r *Request, _ *machine.HardwareIdentity, _ *HostIdentity) { r.Force = true },
		func(_ *lifecycle.Execution, _ *Request, p *machine.HardwareIdentity, _ *HostIdentity) { p.UUID = "" },
		func(_ *lifecycle.Execution, _ *Request, _ *machine.HardwareIdentity, h *HostIdentity) { h.UUID = "" },
	} {
		target, request, pin, host := e, r, machine.HardwareIdentity{UUID: "guest"}, HostIdentity{UUID: "controller"}
		invalid(&target, &request, &pin, &host)
		runner := &adapter{}
		if err := DriveRegistered(context.Background(), runner, target, request, pin, host); err == nil || runner.seen.Operation != "" {
			t.Fatalf("unqualified request reached the adapter: %+v (%v)", runner.seen, err)
		}
	}
}
