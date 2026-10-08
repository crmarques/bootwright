package power

import (
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

// Evidence that proves no settled power state names the exact commands, bound
// to the request's context, that read the machine's state and repeat the
// request. Only a graceful stop or restart also names its forced form: a start
// has no --force, and a forced request already cuts power.
func TestPowerEvidenceRemediesNameTheExactCommand(t *testing.T) {
	for _, test := range []struct {
		verb      string
		force     bool
		command   string
		state     string
		unreached string
	}{
		{Start, false, "bootwright machine start --context lab --name guest", machine.PowerOn,
			"repeat bootwright machine start --context lab --name guest"},
		{Stop, false, "bootwright machine stop --context lab --name guest", machine.PowerOff,
			"repeat bootwright machine stop --context lab --name guest, or cut its power with bootwright machine stop --context lab --name guest --force " +
				"when the guest does not answer a graceful request"},
		{Restart, false, "bootwright machine restart --context lab --name guest", machine.PowerOn,
			"repeat bootwright machine restart --context lab --name guest, or cut its power with bootwright machine restart --context lab --name guest --force " +
				"when the guest does not answer a graceful request"},
		{Stop, true, "bootwright machine stop --context lab --name guest --force", machine.PowerOff,
			"repeat bootwright machine stop --context lab --name guest --force"},
	} {
		request := Request{Identity: Identity{Context: "lab", Object: "guest"}, Verb: test.verb, Force: test.force}
		wrong := machine.PowerOff
		if test.state == machine.PowerOff {
			wrong = machine.PowerOn
		}
		reread := "once its controller answers, read its power state with bootwright machine list --context lab --power-status, and repeat " +
			test.command + " if it is not " + test.state
		for _, evidence := range []struct {
			name     string
			evidence Evidence
			remedy   string
		}{
			{"no postcondition", Evidence{Machine: "guest", Power: test.state, Request: "digest"}, reread},
			{"no reported state", Evidence{Machine: "guest", Postcondition: true, Request: "digest"}, reread},
			{"the wrong state", Evidence{Machine: "guest", Postcondition: true, Power: wrong, Request: "digest"}, test.unreached},
		} {
			_, err := validate(encode(t, evidence.evidence), request, "digest")
			if got := diagnostics.Of(err); len(got) != 1 || got[0].Code != "lifecycle.unknown" || got[0].Remediation != evidence.remedy {
				t.Fatalf("%s: %s refused with %#v, want the remedy %q", test.command, evidence.name, got, evidence.remedy)
			}
		}
	}
	runtime := lifecycle.Runtime{Context: lifecycle.ContextIdentity{Name: "lab"}}
	frozen := Request{Identity: Identity{Context: "lab", Object: "guest"}, Verb: Stop}
	if got, want := invocation(runtime, frozen, nil, "digest", machine.HardwareIdentity{}, nil).Repeat, powerCommand("lab", frozen); got != want {
		t.Fatalf("the power run repeats %q, want %q", got, want)
	}
	if got, want := readInvocation(runtime, ReadSurvey{}, nil, "digest", nil).Repeat, "bootwright machine list --context lab --power-status"; got != want {
		t.Fatalf("the power reading repeats %q, want %q", got, want)
	}
}
