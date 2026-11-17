package diskdiscovery

import "github.com/crmarques/bootwright/internal/diagnostics"

func refusal(code, message, remediation string) error {
	return diagnostics.NewFailureWithRemediation(code, message, "", remediation)
}
