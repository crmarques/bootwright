package installation

import (
	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

func foreignInstallation(contextName string, request Request) error {
	return &diagnostics.Failure{Diagnostics: []diagnostics.Diagnostic{{
		Severity: "error", Code: "lifecycle.state",
		Object:      &diagnostics.ObjectIdentity{APIVersion: api.APIVersion, Kind: string(api.Machine), Name: request.Identity.Object},
		Message:     "Machine/" + request.Identity.Object + " answered with another installation's marker, so no installer was booted and its disks were not overwritten",
		Remediation: "verify the declared Machine and its management controller; Bootwright does not replace another installation in place. Correct the declaration by taking this context back with bootwright destroy --context " + contextName + ", importing the corrected input with bootwright context update --name " + contextName + " --input-dir <directory>, then running bootwright apply --context " + contextName,
	}}}
}
