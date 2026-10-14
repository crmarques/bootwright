//go:build linux && amd64

package contextfs

import (
	"fmt"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

// requireClientBoundRemedy requires the refusal of a tool area past the bound
// to name the bound and the deletion that frees an area.
func requireClientBoundRemedy(t *testing.T, err error, bound string) {
	t.Helper()
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != "controller.conflict" || !strings.Contains(reported[0].Message, bound) {
		t.Fatalf("refusal = %#v (%v), want a controller.conflict naming %s", reported, err, bound)
	}
	if reported[0].Remediation != clientBoundRemedy || !strings.Contains(clientBoundRemedy, "bootwright context delete --name <name> --purge") {
		t.Fatalf("remediation %q, want %q", reported[0].Remediation, clientBoundRemedy)
	}
}

// The bound counts tool areas alone: a closure that finds all 16 held by
// bindings or unfinished stages refuses before it reserves one, naming it.
func TestAClientAreaAtTheBoundNamesItsRemedy(t *testing.T) {
	store, record := lifecycleFixture(t)
	sealedBundleFixture(t, store, record)
	for index := 1; index < maxControllerBundles; index++ {
		if err := publishClients(store, fmt.Sprintf("%064x", index), false); err != nil {
			t.Fatalf("client area %d: %#v", index, diagnostics.Of(err))
		}
	}
	requireClientBoundRemedy(t, publishClients(store, fmt.Sprintf("%064x", maxControllerBundles), true), "the 16 tool areas this host may hold")
}

// A closure that finds the bound held by sealed areas no binding names
// retires them and takes the room they free.
func TestAClientAreaAtTheBoundRetiresUnnamedAreasFirst(t *testing.T) {
	store, record := lifecycleFixture(t)
	sealedBundleFixture(t, store, record)
	for index := 1; index < maxControllerBundles; index++ {
		if err := publishClients(store, fmt.Sprintf("%064x", index), true); err != nil {
			t.Fatalf("client area %d: %#v", index, diagnostics.Of(err))
		}
	}
	last := fmt.Sprintf("%064x", maxControllerBundles)
	if err := publishClients(store, last, true); err != nil {
		t.Fatalf("the closure past the bound refused: %#v", diagnostics.Of(err))
	}
	requireAreas(t, store, fmt.Sprintf("%064x", maxControllerBundles-1), last)
}
