package power

import (
	"context"
	"errors"
	"slices"
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

// A registered power transition returns the adapter's own failure, refuses an
// unbound adapter, and completes only on evidence naming the Machine and
// request it drove, whichever verb it carries.
func TestRegisteredPowerCompletesOnlyOnItsOwnEvidence(t *testing.T) {
	e := lifecycle.Execution{Context: "lab", Operation: "destroy", Attempt: 1, Block: reconciliation.Block{BlockDefinition: reconciliation.BlockDefinition{ID: "install"}}}
	r := Request{Arm: substrate.ArmBaremetal, Identity: Identity{Context: "lab", Object: "guest"}, Version: RequestVersion, Verb: Stop}
	pin, host := machine.HardwareIdentity{UUID: "guest"}, HostIdentity{UUID: "controller"}
	if err := DriveRegistered(context.Background(), nil, e, r, pin, host); code(t, err) != powerRequestInvalid.Code {
		t.Fatalf("an unbound runner = %v", err)
	}
	failure := errors.New("unreachable")
	runner := &adapter{err: failure}
	if err := DriveRegistered(context.Background(), runner, e, r, pin, host); !errors.Is(err, failure) || runner.seen.Operation != Operation {
		t.Fatalf("a failing adapter = %v, ran %q", err, runner.seen.Operation)
	}
	runner = &adapter{power: expected(Stop), machine: "another"}
	if err := DriveRegistered(context.Background(), runner, e, r, pin, host); code(t, err) != powerEvidenceMismatch.Code {
		t.Fatalf("another Machine's evidence = %v", err)
	}
	runner = &adapter{power: expected(Stop)}
	if err := DriveRegistered(context.Background(), runner, e, r, pin, host); err != nil || runner.seen.MaterialValues["lifecyclePower"] != "true" || runner.seen.Digest == "" {
		t.Fatalf("a stop = %v, ran %+v", err, runner.seen)
	}
}

// An unproved registered machine refuses as a changed identity, and the
// declared stops are a copy a caller cannot edit.
func TestTheRegisteredMachineRefusalAndTheDeclaredStopsAreFixed(t *testing.T) {
	if got := code(t, UnprovedRegisteredMachine()); got != powerIdentityChanged.Code {
		t.Fatalf("refusal = %s", got)
	}
	first := Stops()
	if !slices.Equal(first, stops) {
		t.Fatalf("stops = %+v", first)
	}
	first[0].Code = "edited"
	if Stops()[0].Code != powerRequestInvalid.Code {
		t.Fatal("editing the returned stops changed the declared ones")
	}
}
