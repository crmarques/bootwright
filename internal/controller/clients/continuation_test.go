package clients

import (
	"context"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

// An installation whose transaction ran but whose clients are not proved
// installed is unknown, and its remedy names the exact continuation of its
// apply, which resolves it from live evidence before anything else starts.
func TestAnUnknownInstallationNamesItsContinuation(t *testing.T) {
	native := &fakeNative{plan: nativePlanFor(t, prerequisites.NativeRequirements{ContainerRuntime: true, LibvirtClient: true})}
	capability := New(&fakeTools{complete: true, present: true}, native, native, &fakeInstaller{})
	block := planBlock(t, capability, stateOf(environment(), machine("container-runtime", "libvirt")), reconciliation.Apply)
	for _, test := range []struct{ continuation, want string }{
		{"bootwright apply --context lab --authorize data-loss", "resolve it from live evidence with bootwright apply --context lab --authorize data-loss"},
		{"", "repeat the operation to resolve it from live evidence"},
	} {
		execution := newRecorder(&fakeArea{}).execution(t, block, libvirtHostState(t))
		execution.Continuation = test.continuation
		result, err := capability.Apply(context.Background(), execution)
		reported := diagnostics.Of(err)
		if result.Outcome != reconciliation.OutcomeUnknown || len(reported) != 1 || reported[0].Code != "controller.unknown" ||
			reported[0].Message != "the selected native clients are not installed after their transaction" || reported[0].Remediation != test.want {
			t.Fatalf("the installation left %s with %+v, want controller.unknown remedied %q", result.Outcome, reported, test.want)
		}
	}
}
