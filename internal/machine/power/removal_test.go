package power

import (
	"context"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/machine"
)

// removalService answers for the KubeVirt Machines as a destroy that is
// current leaves them: node-0's VirtualMachine removed, node-1's removal still
// pending, and vm-0, which that destroy does not take back, still applied.
func removalService(runtime Runtime, runner Runner, access HostAccess, identities *pins) Service {
	states := map[string]machine.OwnershipState{
		"node-0": {Verb: machine.VerbDestroy, State: machine.BlockDone},
		"node-1": {Verb: machine.VerbDestroy, State: machine.BlockPending},
		"vm-0":   {Verb: machine.VerbApply, State: machine.BlockDone},
	}
	return New(stripped{catalog: kubevirtCatalog()}, evidenceSource{states: states}, identities, runtime, runner, nil, &answer{}, nil, nil).WithHostAccess(access)
}

// A Machine whose realization the current destroy proved gone has nothing to
// reach: a stop, graceful or forced, reports it off and unchanged without a
// runtime, a lend, a pin read or a run, and a start or restart refuses naming
// the removal, sending nothing.
func TestAMachineTheCurrentDestroyRemovedIsOffAndCannotBeStarted(t *testing.T) {
	for _, force := range []bool{false, true} {
		runtime, runner, access, identities := &boundary{}, &hostedAdapter{}, &hostLender{}, &pins{identity: vmPin, found: true}
		results, err := removalService(runtime, runner, access, identities).Stop(context.Background(),
			PowerRequest{ContextName: "lab", Names: []string{"node-0"}, Force: force, SkipConfirmation: true})
		if err != nil || len(results.Machines) != 1 {
			t.Fatalf("stop = %+v (%v)", results, diagnostics.Of(err))
		}
		row := results.Machines[0]
		if row.Machine != "node-0" || row.Power != machine.PowerOff || row.Previous != "" || row.Changed {
			t.Fatalf("the stop of a removed Machine reported %+v", row)
		}
		if runtime.entered != 0 || len(access.lends) != 0 || len(runner.runs) != 0 || len(identities.asked) != 0 {
			t.Fatalf("a removed Machine's stop entered %d runtimes, lent %d, ran %d, read pins %v", runtime.entered, len(access.lends), len(runner.runs), identities.asked)
		}
	}
	for _, verb := range []string{Start, Restart} {
		runtime, runner, access := &boundary{}, &hostedAdapter{}, &hostLender{}
		_, err := removalService(runtime, runner, access, &pins{identity: vmPin, found: true}).converge(context.Background(),
			PowerRequest{ContextName: "lab", Names: []string{"node-0"}, Verb: verb, SkipConfirmation: true})
		reported := diagnostics.Of(err)
		if len(reported) != 1 || reported[0].Code != "access.unavailable" ||
			reported[0].Message != "the destroy current in this context removed the realization of Machine/node-0, so there is nothing to "+verb ||
			reported[0].Remediation != "finish the removal with bootwright destroy --context lab, then bootwright apply --context lab realizes Machine/node-0 again" {
			t.Fatalf("%s of a removed Machine = %+v", verb, reported)
		}
		if runtime.entered != 0 || len(access.lends) != 0 || len(runner.runs) != 0 {
			t.Fatalf("a refused %s entered %d runtimes, lent %d, ran %d", verb, runtime.entered, len(access.lends), len(runner.runs))
		}
	}
}

// While the destroy is current a Machine it has not yet removed is still held
// to its pin: every verb reaches its VirtualMachine with the pin the apply
// recorded, and a stop of it beside a removed one runs for it alone.
func TestAMachineTheCurrentDestroyHasNotRemovedIsReachedWithItsPin(t *testing.T) {
	for _, verb := range []string{Start, Stop, Restart} {
		runner, access, identities := &hostedAdapter{}, &hostLender{}, &pins{identity: vmPin, found: true}
		_, err := removalService(&boundary{}, runner, access, identities).converge(context.Background(),
			PowerRequest{ContextName: "lab", Names: []string{"node-1"}, Verb: verb, SkipConfirmation: true})
		if err != nil || len(runner.runs) != 1 {
			t.Fatalf("%s = %v with %d runs", verb, diagnostics.Of(err), len(runner.runs))
		}
		if run := runner.runs[0]; run.MaterialValues["pinnedUUIDBase64"] == "" || run.MaterialValues["pinnedSerialBase64"] == "" || run.Refusals[identityMismatch] == nil {
			t.Fatalf("%s reached node-1 unpinned: %+v", verb, run.MaterialValues)
		}
	}
	runner, identities := &hostedAdapter{}, &pins{identity: vmPin, found: true}
	results, err := removalService(&boundary{}, runner, &hostLender{}, identities).Stop(context.Background(),
		PowerRequest{ContextName: "lab", Names: []string{"node-0", "node-1"}, SkipConfirmation: true})
	if err != nil || len(results.Machines) != 2 || len(runner.runs) != 1 {
		t.Fatalf("stop = %+v (%v) with %d runs", results, diagnostics.Of(err), len(runner.runs))
	}
	if !strings.Contains(string(runner.runs[0].Canonical), `"object":"node-1"`) || len(identities.asked) != 1 || identities.asked[0][1] != "node-1" {
		t.Fatalf("the batch ran %s and read pins %v", runner.runs[0].Canonical, identities.asked)
	}
	if removed, stopped := results.Machines[0], results.Machines[1]; removed.Power != machine.PowerOff || removed.Changed || stopped.Power != machine.PowerOff || !stopped.Changed {
		t.Fatalf("rows = %+v", results.Machines)
	}
}

// A reading during the destroy reports each Machine by what stands: the
// removed one is absent, and the one whose removal is pending and the one
// the destroy keeps are read through their host with their own pin.
func TestAReadingDuringADestroyReportsEachMachineByWhatStands(t *testing.T) {
	runner, identities := &hostedAdapter{power: map[string]string{"node-1": machine.PowerOn, "vm-0": machine.PowerOff}}, &pins{identity: vmPin, found: true}
	readings, err := removalService(&boundary{}, runner, &hostLender{}, identities).Read(context.Background(), "lab", []string{"node-0", "node-1", "vm-0"})
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	if _, read := readings["node-0"]; read || readings["node-1"] != machine.PowerOn || readings["vm-0"] != machine.PowerOff {
		t.Fatalf("readings = %v", readings)
	}
	for _, asked := range identities.asked {
		if asked[1] == "node-0" {
			t.Fatalf("the reading read the pin of the removed Machine: %v", identities.asked)
		}
	}
	for _, run := range runner.runs {
		if run.MaterialValues["pinnedUUIDBase640"] == "" || run.MaterialValues["pinnedSerialBase640"] == "" {
			t.Fatalf("a reading ran unpinned: %+v", run.MaterialValues)
		}
	}
}
