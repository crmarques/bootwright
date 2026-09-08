package infrastructureservices

import api "github.com/crmarques/bootwright/api/v1alpha1"

func ValidatePartial(o api.Object, c api.Catalog) []api.Issue {
	if o.Kind() != api.InfraComponent {
		return nil
	}
	// HTTPS may receive its missing TLS block from a concrete declaration.
	issues := []api.Issue{}
	for _, i := range validateTransport(o) {
		if i.Message != "HTTPS artifact listeners require TLS configuration" {
			issues = add(issues, i)
		}
	}
	return issues
}
