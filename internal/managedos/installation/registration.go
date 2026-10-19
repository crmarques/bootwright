package installation

import (
	"bytes"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/substrate"
)

var registrationPackages = []string{"subscription-manager"}

const (
	subscriptionField      = "spec.subscription"
	subscriptionReposField = "spec.customizations.repositories.subscription"
	maxRegistrationValue   = 255
)

func subscriptionEntitlement(catalog api.Catalog, profile api.Object) (string, api.Object, bool) {
	reference := profile.Spec().Get("subscription", "entitlementRef").Text()
	if reference == "" {
		return "", api.Object{}, false
	}
	entitlement, found := catalog.Find(api.Entitlement, reference)
	return reference, entitlement, found
}

func externalRHSM(entitlement api.Object) bool {
	return entitlement.Spec().Get("rhsm", "management").Text() == "external"
}

func registrationFor(catalog api.Catalog, profile api.Object) (*Registration, error) {
	reference, entitlement, found := subscriptionEntitlement(catalog, profile)
	if reference == "" {
		return nil, nil
	}
	if !found {
		return nil, refusal("api.reference", "the install profile's subscription Entitlement is not in the selected graph",
			"declare Entitlement/"+reference+" or correct spec.subscription.entitlementRef on "+profile.Identity())
	}
	if externalRHSM(entitlement) {
		return nil, nil
	}
	rhsm := entitlement.Spec().Get("rhsm")
	registration := Registration{
		ActivationKeyRef: rhsm.Get("activationKeyRef").Text(),
		EntitlementRef:   reference,
		OrganizationRef:  rhsm.Get("organizationRef").Text(),
		Repositories:     subscriptionRepositories(profile),
	}
	if registration.OrganizationRef == "" || registration.ActivationKeyRef == "" {
		return nil, refusal("api.required", "managed RHSM requires organization and activation-key references",
			"set spec.rhsm.organizationRef and spec.rhsm.activationKeyRef on Entitlement/"+reference)
	}
	return &registration, nil
}

func subscriptionRepositories(profile api.Object) SubscriptionRepositories {
	selected := profile.Spec().Get("customizations", "repositories", "subscription")
	listed := func(name string) []string {
		if values := SortedUnique(selected.Get(name).Strings()); values != nil {
			return values
		}
		return []string{}
	}
	return SubscriptionRepositories{Disable: listed("disable"), Enable: listed("enable")}
}

func refusedRegistration(catalog api.Catalog, machine, profile api.Object, derived substrate.Target, derivedOK bool) (reason, remediation string) {
	reference, entitlement, found := subscriptionEntitlement(catalog, profile)
	if reference == "" {
		return "", ""
	}
	withoutSubscription := "remove " + subscriptionField + " from " + profile.Identity()
	if derivedOK && derived.Identity.Channel == substrate.ChannelDeliveredKey {
		return "the registration profile arm is qualified only on virtual Machines, and " + machine.Identity() + " is physical; physical qualification is B78",
			"install " + machine.Identity() + " with a profile that does not select " + subscriptionField + ", or " + withoutSubscription
	}
	if !found {
		return "", ""
	}
	rhsm := entitlement.Spec().Get("rhsm")
	if externalRHSM(entitlement) {
		if !profile.Spec().Has("customizations", "repositories", "subscription") {
			return "", ""
		}
		return "subscription repositories are enabled on a registration this executable performs, and " + entitlement.Identity() + " sets spec.rhsm.management to external",
			"remove " + subscriptionReposField + " from " + profile.Identity() + ", or set spec.rhsm.management to managed on " + entitlement.Identity()
	}
	if rhsm.Has("satellite") {
		return "this executable registers only with the hosted subscription service, and " + entitlement.Identity() + " selects a Satellite in spec.rhsm.satellite",
			"remove spec.rhsm.satellite from " + entitlement.Identity() + ", or " + withoutSubscription
	}
	if rhsm.Get("connectToInsights").Bool() {
		return "this executable does not enroll a Machine in Insights, a second remote identity it neither proves nor removes, and " + entitlement.Identity() + " sets spec.rhsm.connectToInsights to true",
			"set spec.rhsm.connectToInsights to false on " + entitlement.Identity() + ", or " + withoutSubscription
	}
	choice := effectiveProxy(machine, profile)
	if proxy, found := catalog.Find(api.Proxy, choice.Get("proxyRef").Text()); found && !choice.Has("direct") {
		return "this executable registers only over a direct route, and " + machine.Identity() + " reaches the network through " + proxy.Identity(),
			"select direct: {} in spec.proxy of " + machine.Identity() + " or of " + profile.Identity() + ", or " + withoutSubscription
	}
	return "", ""
}

func registrationMaterial(operation string, request Request) []lifecycle.MaterialFile {
	if operation != "apply" || request.Registration == nil {
		return nil
	}
	return []lifecycle.MaterialFile{
		{Name: "rhsm-organization", Part: secrets.ValuePart, Secret: request.Registration.OrganizationRef, Variable: "rhsmOrganization"},
		{Name: "rhsm-activation-key", Part: secrets.ValuePart, Secret: request.Registration.ActivationKeyRef, Variable: "rhsmActivationKey"},
	}
}

func registrationCarried(value []byte) bool {
	value = bytes.TrimSuffix(value, []byte("\n"))
	if len(value) == 0 || len(value) > maxRegistrationValue {
		return false
	}
	for _, b := range value {
		if b < 0x21 || b > 0x7e {
			return false
		}
	}
	return true
}

func registrationValues(execution lifecycle.Execution, request Request) error {
	if request.Registration == nil {
		return nil
	}
	entitlement := "Entitlement/" + request.Registration.EntitlementRef
	for _, bound := range []struct{ reference, subject string }{
		{request.Registration.OrganizationRef, "organization"},
		{request.Registration.ActivationKeyRef, "activation key"},
	} {
		material, ok := execution.Material[bound.reference]
		var value []byte
		if ok {
			value, ok = material.Part(secrets.ValuePart)
		}
		if !ok {
			return refusal("secret.store", "the bound "+bound.subject+" of "+entitlement+" is not available to this attempt",
				lifecycle.ReopenBindings(execution.Context, execution.Continuation))
		}
		if !registrationCarried(value) {
			secret := "Secret/" + bound.reference
			return refusal("api.value", secret+" holds an "+bound.subject+" for "+entitlement+
				" that is not one line of 1 to 255 printable ASCII characters without whitespace, so this attempt changed nothing",
				"replace "+secret+" with one line of 1 to 255 printable ASCII characters without whitespace, then run bootwright apply --context "+execution.Context+" again")
		}
	}
	return nil
}

func registrationRefusals(contextName string, request Request) map[string]error {
	if request.Registration == nil {
		return nil
	}
	machine := "Machine/" + request.Identity.Object
	entitlement := "Entitlement/" + request.Registration.EntitlementRef
	profile := "MachineInstallProfile/" + request.Identity.Profile
	organization, key := "Secret/"+request.Registration.OrganizationRef, "Secret/"+request.Registration.ActivationKeyRef
	again := "then run bootwright apply --context " + contextName + " again"
	refused := machineRefusal(request)
	return map[string]error{
		"registration-input": refused("the registration worker on "+machine+" refused the organization or activation key of "+entitlement+" as not one line of printable ASCII without whitespace",
			"replace "+organization+" or "+key+" with one line of 1 to 255 printable ASCII characters without whitespace, "+again),
		"registration-foreign": refused(machine+" is registered with a subscription consumer this installation did not create, and it was left registered",
			"unregister "+machine+" from its subscription service yourself, "+again+", or take this context back with bootwright destroy --context "+contextName),
		"registration-refused": refused("the subscription service refused to register "+machine+" with the organization and activation key of "+entitlement,
			"check the values of "+organization+" and "+key+", which "+entitlement+" names, "+again),
		"registration-identity-collision": refused("the subscription service reports another registered host with the hardware identity of "+machine,
			"remove the subscription service's stale consumer for "+machine+", left by an earlier installation that was not unregistered, "+again),
		"registration-unavailable": refused("the subscription service, or the registration tooling on "+machine+", did not answer",
			"restore the route from "+machine+" to the subscription service, "+again),
		"registration-repositories": refused("the subscription service did not accept "+subscriptionReposField+" of "+profile+" on "+machine,
			"correct the repository IDs in "+subscriptionReposField+" of "+profile+", then take this context back with bootwright destroy --context "+contextName+
				" and run bootwright apply --context "+contextName+"; a repository edit is another installation"),
	}
}

func machineRefusal(request Request) func(message, remediation string) error {
	return func(message, remediation string) error {
		return &diagnostics.Failure{Diagnostics: []diagnostics.Diagnostic{{
			Severity: "error", Code: "lifecycle.state", Message: message, Remediation: remediation,
			Object: &diagnostics.ObjectIdentity{APIVersion: api.APIVersion, Kind: string(api.Machine), Name: request.Identity.Object},
		}}}
	}
}

func unregistrationRefusals(contextName string, request Request) map[string]error {
	if request.Registration == nil {
		return nil
	}
	machine := "Machine/" + request.Identity.Object
	again := "then run bootwright destroy --context " + contextName + " again"
	refused := machineRefusal(request)
	return map[string]error{
		"registration-unreachable": refused(machine+" did not answer through its identity channel or its pinned connection, so the subscription registration its installation created was not removed",
			"start "+machine+" with bootwright machine start --context "+contextName+" --name "+request.Identity.Object+", "+again),
		"registration-unavailable": refused("the subscription service, or the registration tooling on "+machine+", did not complete the removal of the subscription consumer its installation created",
			"restore the route from "+machine+" to the subscription service, "+again),
	}
}
