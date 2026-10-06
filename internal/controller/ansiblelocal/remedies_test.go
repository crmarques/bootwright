package ansiblelocal

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

// Setup and a context's controller stage share this adapter, so a failure it
// raises keeps its code in both and names the command of the scope that met
// it: setup's own rerun, or the stage of the named context, never setup.
func TestAdapterFailuresNameTheirScope(t *testing.T) {
	for _, code := range []string{"controller.setup", "controller.identity", "controller.unknown", "controller.unsupported"} {
		err := failure(code, "the Ansible operation has no complete result")
		setup := diagnostics.Of(err)
		if len(setup) != 1 || setup[0].Code != code || setup[0].Source != nil || !strings.HasSuffix(setup[0].Remediation, ", then rerun bootwright setup.") {
			t.Errorf("setup scope: %+v", setup)
		}
		staged := diagnostics.Of(prerequisites.InStage(err, "lab"))
		if len(staged) != 1 || staged[0].Code != code || staged[0].Source != nil ||
			!strings.HasSuffix(staged[0].Remediation, ", then run bootwright apply --stage controller --context lab.") ||
			strings.Contains(staged[0].Remediation, "setup") || strings.Contains(staged[0].Remediation, "receipt") {
			t.Errorf("stage scope: %+v", staged)
		}
	}
}
