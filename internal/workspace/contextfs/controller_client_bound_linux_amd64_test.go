//go:build linux && amd64

package contextfs

import (
	"fmt"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

// requireClientBoundRemedy requires the refusal of a controller stage whose
// client closure meets the host's bound: a conflict naming that bound, whose
// remedy is the setup that retires superseded execution bundles and which
// says that nothing frees a bound client areas and the current bundle hold
// (D90).
func requireClientBoundRemedy(t *testing.T, err error, bound string) {
	t.Helper()
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != "controller.conflict" || !strings.Contains(reported[0].Message, bound) {
		t.Fatalf("refusal = %#v (%v), want a controller.conflict naming %s", reported, err, bound)
	}
	for _, fragment := range []string{"run bootwright setup --purge-old-bundles to retire superseded execution bundles, then repeat the command",
		"when client areas and the current execution bundle fill the host, no command of this build frees that room"} {
		if !strings.Contains(reported[0].Remediation, fragment) {
			t.Fatalf("remediation %q lacks %q", reported[0].Remediation, fragment)
		}
	}
}

// A client closure that finds every bundle area held refuses before it
// reserves one, naming the bound and the setup that makes room.
func TestAClientAreaAtTheBoundNamesItsRemedy(t *testing.T) {
	store, record := lifecycleFixture(t)
	sealedBundleFixture(t, store, record)
	for index := 1; index < maxControllerBundles; index++ {
		if err := publishClients(store, fmt.Sprintf("%064x", index), true); err != nil {
			t.Fatalf("client area %d: %#v", index, diagnostics.Of(err))
		}
	}
	requireClientBoundRemedy(t, publishClients(store, fmt.Sprintf("%064x", maxControllerBundles), true), "the 16 bundle areas this host may hold")
}

// A controller stage whose new resolution finds every retained resolution
// held, and retires none of them, refuses naming that bound, and its remedy
// survives to the stage's own apply result.
func TestAStageResolutionAtTheBoundNamesItsRemedy(t *testing.T) {
	store, record := lifecycleFixture(t)
	resolvedSetupFixture(t, store, record)
	native := &solvingAgain{}
	for native.solves < maxControllerBundles-1 {
		if err := solveLibvirtClient(t, store, record, native, false); err != nil {
			t.Fatalf("filling the bound: solve %d failed: %#v", native.solves, diagnostics.Of(err))
		}
	}
	if held := retainedResolutions(t, store); len(held.RetainedDefinitions) != maxControllerBundles {
		t.Fatalf("the fixture retained %d resolutions, want the bound of %d", len(held.RetainedDefinitions), maxControllerBundles)
	}
	requireClientBoundRemedy(t, solveLibvirtClient(t, store, record, native, false), "the 16 dependency resolutions this host may retain")
}
