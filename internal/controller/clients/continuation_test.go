package clients

import (
	"context"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

// forgetfulDnf reports success for an install that leaves nothing installed.
type forgetfulDnf struct{ *fakeDnf }

func (f *forgetfulDnf) open(prerequisites.SetupEgress) (prerequisites.HostPackages, error) {
	return f, nil
}

func (f *forgetfulDnf) Install(context.Context, []string, prerequisites.RunOutput) ([]prerequisites.HostPackageEvidence, error) {
	return nil, nil
}

// An installation whose dnf run completed but whose packages are not proved
// installed is unknown, and its remedy names the exact continuation of its
// apply, which resolves it from live evidence before anything else starts.
func TestAnUnknownInstallationNamesItsContinuation(t *testing.T) {
	for _, test := range []struct{ continuation, want string }{
		{"bootwright apply --context lab --authorize data-loss", "resolve it from live evidence with bootwright apply --context lab --authorize data-loss"},
		{"", "repeat the operation to resolve it from live evidence"},
	} {
		dnf := &forgetfulDnf{fakeDnf: newDnf()}
		capability := New(&fakeTools{complete: true, present: true}, dnf.open, &fakeSetup{}, &fakeInstaller{})
		block := planBlock(t, capability, stateOf(libvirtGraph()...), reconciliation.Apply)
		execution := newRecorder(&fakeArea{}).execution(t, block, hostState())
		execution.Continuation = test.continuation
		result, err := capability.Apply(context.Background(), execution)
		reported := diagnostics.Of(err)
		if result.Outcome != reconciliation.OutcomeUnknown || len(reported) != 1 || reported[0].Code != "controller.unknown" ||
			reported[0].Message != "the selected host packages are not installed after their install" || reported[0].Remediation != test.want {
			t.Fatalf("the installation left %s with %+v, want controller.unknown remedied %q", result.Outcome, reported, test.want)
		}
	}
}
