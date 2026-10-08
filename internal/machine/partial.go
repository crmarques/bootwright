package machine

import (
	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infrastructureservices"
	"github.com/crmarques/bootwright/internal/substrate"
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
		issues = appendIssues(issues, invariant("$.spec.access", "Bootwright-installed Machines must not author access", "remove access; Bootwright derives an installed Machine's access from the fleet key"))
	}
	issues = appendIssues(issues, validateNative(o.Spec().Get("network", "inline", "nmstate"), "$.spec.network.inline.nmstate", true)...)
	issues = appendIssues(issues, validateNative(o.Spec().Get("network", "overrides"), "$.spec.network.overrides", true)...)
	network := o.Spec().Get("network")
	if network.Has("inline") && network.Has("overrides") {
		issues = appendIssues(issues, invariant("$.spec.network.overrides", "inline configuration forbids reusable-template overrides", "remove network.overrides, or select a reusable configuration with network.configRef instead of network.inline"))
	}
	if network.Has("attachmentRef") && network.Has("interfaceAttachments") {
		issues = appendIssues(issues, invariant("$.spec.network.interfaceAttachments", "attachment selections are mutually exclusive", "keep either network.attachmentRef or network.interfaceAttachments"))
	}
	issues = appendIssues(issues, validateDefaultMediaTrust(o.Spec().Get("hardware", "management", "bmc"))...)
	if o.Spec().Get("os", "provided").Bool() {
		if o.Spec().Has("os", "install", "ntp") {
			issues = appendIssues(issues, invariant("$.spec.os.install.ntp", "OS-ready Machines forbid installation NTP choices", "remove os.install.ntp"))
		}
		for _, key := range []string{"configRef", "inline", "attachmentRef", "interfaceAttachments", "interfaceBinding", "installAddressRef", "overrides"} {
			if network.Has(key) {
				issues = appendIssues(issues, invariant("$.spec.network."+key, "OS-ready Machines declare contacts only", "remove network."+key+"; an OS-ready Machine declares contacts only"))
			}
		}
	}
	return issues
}

// validateDefaultMediaTrust refuses the virtual-media exception in a Machine
// kind default: inherited, it would reach every Machine that authors no trust.
func validateDefaultMediaTrust(bmc api.Value) []api.Issue {
	if bmc.Get("virtualMedia", "tls", "trust").Text() != substrate.TrustDisableVerification {
		return nil
	}
	return []api.Issue{{Code: "api.invariant", Field: "$.spec.hardware.management.bmc.virtualMedia.tls.trust",
		Message:     "disable-verification is a per-Machine exception and never a kind default",
		Remediation: "declare hardware.management.bmc.virtualMedia.tls.trust: disable-verification on each Machine that needs it"}}
}
