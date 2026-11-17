package tooltable

import (
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

// Failure is a refusal from a selection helper that names no remedy
// of its own: the field it names is the one to correct, and a refusal no
// declared field settles is an invariant of this executable.
func Failure(object api.Object, code, field, message string) error {
	remediation := "use a compatible executable"
	if object.Kind() != "" && field != "" {
		remediation = "correct " + strings.TrimPrefix(field, "$.") + " on " + object.Identity()
	}
	return Refuse(object, code, field, message, remediation)
}

// Refuse is a selection refusal on the object and field that declare
// it, with the action that settles it. It never echoes an authored value.
func Refuse(object api.Object, code, field, message, remediation string) error {
	diagnostic := diagnostics.Diagnostic{Severity: "error", Code: code, Field: field, Message: message, Remediation: remediation}
	if object.Kind() != "" {
		diagnostic.Object = &diagnostics.ObjectIdentity{APIVersion: api.APIVersion, Kind: string(object.Kind()), Name: object.Name()}
	}
	return &diagnostics.Failure{Diagnostics: []diagnostics.Diagnostic{diagnostic}}
}
