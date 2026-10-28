package architecture_test

import (
	"slices"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

var warningOnlyDiagnosticCodes = []string{
	"api.deferred", "api.unrealized", "access.exit", "access.unproved", "context.orphaned",
	"controller.loader-preload", "controller.runtime-retention", "controller.untested-release",
	"input.secrets-directory", "lifecycle.surviving-consumer", "substrate.shadowed-route",
}

// specs/cli/output.md requires every error diagnostic other than cli.usage to
// carry a remediation. The CLI writes a registered code's default when its
// emitter gave none, so every registered code that can be an error has one and
// no default belongs to an unregistered code.
func TestEveryErrorCodeHasADefaultRemediation(t *testing.T) {
	documented := documentedDiagnosticCodes(t)
	defaults := diagnostics.DefaultRemediationCodes()
	for code := range documented {
		if code == "cli.usage" || slices.Contains(warningOnlyDiagnosticCodes, code) {
			continue
		}
		if diagnostics.DefaultRemediation(code) == "" {
			t.Errorf("%s is a registered error code with no default remediation in internal/diagnostics/remediation.go", code)
		}
	}
	for _, code := range defaults {
		if !documented[code] {
			t.Errorf("internal/diagnostics/remediation.go gives %s a default, which specs/cli/output.md does not register", code)
		}
	}
	if len(defaults) < 60 {
		t.Fatalf("read %d defaults; the table has changed shape", len(defaults))
	}
}
