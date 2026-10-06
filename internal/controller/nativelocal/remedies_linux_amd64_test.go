//go:build linux && amd64

package nativelocal

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

// Setup and a context's controller stage share this resolver, so a failure it
// raises keeps its code in both and names the command of the scope that met
// it: setup's own rerun, or the stage of the named context, never setup.
func TestAdapterFailuresNameTheirScope(t *testing.T) {
	err := failure("the native solver failed")
	setup := diagnostics.Of(err)
	if len(setup) != 1 || setup[0].Code != "controller.setup" || setup[0].Source != nil || !strings.HasSuffix(setup[0].Remediation, ", then rerun bootwright setup.") {
		t.Fatalf("setup scope: %+v", setup)
	}
	staged := diagnostics.Of(prerequisites.InStage(err, "lab"))
	if len(staged) != 1 || staged[0].Code != "controller.setup" || staged[0].Source != nil ||
		staged[0].Remediation != "Restore the provided OS package-manager foundation and approved repository access, then run bootwright apply --stage controller --context lab." {
		t.Fatalf("stage scope: %+v", staged)
	}
}
