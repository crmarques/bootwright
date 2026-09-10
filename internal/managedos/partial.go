package managedos

import (
	"slices"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infrastructureservices"
)

func ValidatePartial(o api.Object, c api.Catalog) []api.Issue {
	if o.Kind() == api.Entitlement {
		if o.Spec().Get("rhsm", "management").Text() != "external" {
			return nil
		}
		issues := []api.Issue{}
		for _, key := range []string{"organizationRef", "activationKeyRef", "connectToInsights", "satellite"} {
			if o.Spec().Has("rhsm", key) {
				issues = add(issues, issue("$.spec.rhsm."+key, "external RHSM permits only management"))
			}
		}
		return issues
	}
	if o.Kind() != api.MachineInstallProfile {
		return nil
	}
	issues := validateCloneCustomizations(o)
	issues = add(issues, infrastructureservices.ValidateProxyChoice(o.Spec().Get("proxy"), "$.spec.proxy")...)
	custom := o.Spec().Get("customizations")
	for _, service := range custom.Get("services", "enabled").Strings() {
		if slices.Contains(custom.Get("services", "disabled").Strings(), service) {
			issues = add(issues, issue("$.spec.customizations.services", "enabled and disabled services must be disjoint"))
		}
	}
	if o.Spec().Has("subscription") && o.Spec().Has("installer", "anaconda", "packageSource", "fromSubscription") {
		issues = add(issues, issue("$.spec.subscription", "top-level subscription cannot accompany installation fromSubscription"))
	}
	return issues
}
