package substrate

import api "github.com/crmarques/bootwright/api/v1alpha1"

func ValidatePartial(o api.Object, c api.Catalog) []api.Issue {
	issues := ValidateAuthored(o, c)
	if o.Kind() != api.InfraProvider {
		return issues
	}
	if variant := Variant(o); variant != "" {
		for _, segment := range o.Spec().Get("segments").Items() {
			for _, arm := range Arms() {
				if arm != variant && segment.Has(string(arm)) {
					issues = api.AppendIssues(issues, issue("$.spec.segments", "segment arm must match its provider substrate"))
				}
			}
		}
	}
	return issues
}
