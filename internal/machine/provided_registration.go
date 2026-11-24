package machine

import api "github.com/crmarques/bootwright/api/v1alpha1"

const ProvidedRegistrationFamily = "os-provided-registration"

func ProvidedSubscription(object api.Object) string {
	return object.Spec().Get("os", "provided", "subscription", "entitlementRef").Text()
}

func validateProvidedRegistration(object api.Object, catalog api.Catalog) []api.Issue {
	if ProvidedSubscription(object) == "" {
		return nil
	}
	const field = "$.spec.os.provided.subscription"
	controller := ControllerName(catalog)
	if object.Name() == controller {
		return []api.Issue{invariant(field, "a provided registration converges a remote Machine; the controller keeps its own registration", "remove spec.os.provided.subscription and maintain the controller's registration separately")}
	}
	if _, err := PlacementFor(object, controller); err != nil {
		return []api.Issue{invariant(field, "a provided registration requires pinned lifecycle SSH access", "author spec.access.lifecycle or a pinned root login in spec.access.ssh")}
	}
	return nil
}
