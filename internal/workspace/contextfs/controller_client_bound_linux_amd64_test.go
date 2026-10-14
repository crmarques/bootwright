//go:build linux && amd64

package contextfs

import (
	"fmt"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

// requireClientBoundRemedy requires the refusal of a tool area past the bound
// to name the bound and say that no command of this build frees a tool area.
func requireClientBoundRemedy(t *testing.T, err error, bound string) {
	t.Helper()
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != "controller.conflict" || !strings.Contains(reported[0].Message, bound) {
		t.Fatalf("refusal = %#v (%v), want a controller.conflict naming %s", reported, err, bound)
	}
	if want := "no command of this build frees a tool area; run this context's controller stage on another controller host"; reported[0].Remediation != want {
		t.Fatalf("remediation %q, want %q", reported[0].Remediation, want)
	}
}

// A client closure that finds every tool area held refuses before it reserves
// one, naming the bound.
func TestAClientAreaAtTheBoundNamesItsRemedy(t *testing.T) {
	store, record := lifecycleFixture(t)
	sealedBundleFixture(t, store, record)
	for index := 1; index < maxControllerBundles; index++ {
		if err := publishClients(store, fmt.Sprintf("%064x", index), true); err != nil {
			t.Fatalf("client area %d: %#v", index, diagnostics.Of(err))
		}
	}
	requireClientBoundRemedy(t, publishClients(store, fmt.Sprintf("%064x", maxControllerBundles), true), "the 16 tool areas this host may hold")
}
