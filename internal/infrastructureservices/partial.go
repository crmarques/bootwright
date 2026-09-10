package infrastructureservices

import api "github.com/crmarques/bootwright/api/v1alpha1"

func ValidatePartial(o api.Object, _ api.Catalog) []api.Issue {
	issues := validateIntrinsic(o, true)
	if o.Kind() == api.ArtifactServer && o.Spec().Has("endpoints") && !o.Spec().Has("management") {
		issues = add(issues, api.Issue{
			Code: "api.required", Field: "$.spec.management",
			Message:     "ArtifactServer endpoint defaults require an explicit management mode",
			Remediation: "set management to managed or external in the same defaults.ArtifactServer fragment as endpoints",
		})
	}
	return issues
}
