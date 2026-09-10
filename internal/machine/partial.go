package machine

import (
	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infrastructureservices"
)

// ValidatePartial rejects contradictions already provable in unused defaults.
func ValidatePartial(o api.Object, c api.Catalog) []api.Issue {
	issues := []api.Issue{}
	if o.Kind() == api.NetworkConfig {
		return validateNative(o.Spec().Get("nmstate"), "$.spec.nmstate", true)
	}
	if o.Kind() != api.Machine {
		return issues
	}
	issues = appendIssues(issues, infrastructureservices.ValidateProxyChoice(o.Spec().Get("proxy"), "$.spec.proxy")...)
	if installed(o) && o.Spec().Has("access") {
		issues = appendIssues(issues, invariant("$.spec.access", "Bootwright-installed Machines must not author access"))
	}
	issues = appendIssues(issues, validateNative(o.Spec().Get("network", "inline", "nmstate"), "$.spec.network.inline.nmstate", true)...)
	issues = appendIssues(issues, validateNative(o.Spec().Get("network", "overrides"), "$.spec.network.overrides", true)...)
	network := o.Spec().Get("network")
	if network.Has("inline") && network.Has("overrides") {
		issues = appendIssues(issues, invariant("$.spec.network.overrides", "inline configuration forbids reusable-template overrides"))
	}
	if network.Has("attachmentRef") && network.Has("interfaceAttachments") {
		issues = appendIssues(issues, invariant("$.spec.network.interfaceAttachments", "attachment selections are mutually exclusive"))
	}
	if o.Spec().Get("os", "provided").Bool() {
		if o.Spec().Has("os", "install", "ntp") {
			issues = appendIssues(issues, invariant("$.spec.os.install.ntp", "OS-ready Machines forbid installation NTP choices"))
		}
		for _, key := range []string{"configRef", "inline", "attachmentRef", "interfaceAttachments", "interfaceBinding", "installAddressRef", "overrides"} {
			if network.Has(key) {
				issues = appendIssues(issues, invariant("$.spec.network."+key, "OS-ready Machines declare contacts only"))
			}
		}
	}
	return issues
}
