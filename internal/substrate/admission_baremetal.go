package substrate

import (
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func normalizeBaremetal(arm api.Value) api.Value {
	bmc := arm.Get("defaults", "bmc")
	if !bmc.Present() {
		return arm
	}
	return arm.WithPath(NormalizeBMCDefaults(bmc), "defaults", "bmc")
}

func validateBaremetal(o api.Object, _ api.Catalog) []api.Issue {
	return ValidateBMCDefaults(o.Spec().Get("baremetal", "defaults", "bmc"), "$.spec.baremetal.defaults.bmc", false)
}

func validateBaremetalAuthored(o api.Object) []api.Issue {
	bmc := o.Spec().Get("baremetal", "defaults", "bmc")
	issues := ValidateBMCDefaults(bmc, "$.spec.baremetal.defaults.bmc", true)
	return api.AppendIssues(issues, validateProviderMediaTrust(bmc, "$.spec.baremetal.defaults.bmc")...)
}

func NormalizeBMCDefaults(bmc api.Value) api.Value {
	media := bmc.Get("virtualMedia")
	tls := media.Get("tls").Default("trust", api.StringValue(TrustImportCertificate))
	switch tls.Get("trust").Text() {
	case "disableVerification":
		tls = tls.Default("restoreVerificationAfterBoot", api.BoolValue(true))
	case "importCertificate":
		tls = tls.Default("removeCertificateAfterBoot", api.BoolValue(false))
	}
	if tls.Get("trust").Text() != TrustDisableVerification {
		verified := !bmc.Has("address") || BMCVerified(bmc)
		tls = tls.Default("enableVerification", api.BoolValue(verified))
	}
	return bmc.With("virtualMedia", media.With("tls", tls))
}

func ValidateBMCDefaults(bmc api.Value, path string, authored bool) []api.Issue {
	tls := bmc.Get("virtualMedia", "tls")
	issues := []api.Issue{}
	if authored && tls.Present() && tls.Len() == 0 {
		issues = api.AppendIssues(issues, issue(path+".virtualMedia.tls", "an authored virtual-media TLS block must set an option"))
	}
	issues = api.AppendIssues(issues, validateControllerTrust(bmc, path)...)
	trust := tls.Get("trust").Text()
	if trust == "" {
		trust = TrustImportCertificate
	}
	if tls.Has("restoreVerificationAfterBoot") && trust != "disableVerification" {
		issues = api.AppendIssues(issues, issue(path+".virtualMedia.tls.restoreVerificationAfterBoot", "restoring verification requires disableVerification trust"))
	}
	if tls.Has("removeCertificateAfterBoot") && trust != "importCertificate" {
		issues = api.AppendIssues(issues, issue(path+".virtualMedia.tls.removeCertificateAfterBoot", "removing a certificate requires importCertificate trust"))
	}
	if tls.Get("enableVerification").Bool() {
		controllerTLS := bmc.Get("tls")
		if trust == TrustDisableVerification || controllerTLS.Get("verification").Text() == "none" ||
			(bmc.Has("address") && !strings.HasPrefix(bmc.Get("address").Text(), "https://")) {
			issues = api.AppendIssues(issues, issue(path+".virtualMedia.tls.enableVerification", "enabling media verification requires verified HTTPS BMC access and importCertificate or established trust"))
		}
	}
	return issues
}

// validateControllerTrust refuses any verification mode on a plain-HTTP
// controller endpoint, which has no TLS leg to verify.
func validateControllerTrust(bmc api.Value, path string) []api.Issue {
	var issues []api.Issue
	if !strings.HasPrefix(bmc.Get("address").Text(), "http://") {
		return nil
	}
	if bmc.Has("tls", "verification") {
		issues = append(issues, api.Issue{Code: "api.invariant", Field: path + ".tls.verification", Message: "TLS verification applies only to an HTTPS controller endpoint, and a plain-HTTP BMC address has no TLS leg",
			Remediation: "set the BMC address to its https:// ComputerSystem endpoint, or remove tls.verification"})
	}
	if bmc.Has("tls", "trustBundleRef") {
		issues = append(issues, api.Issue{Code: "api.invariant", Field: path + ".tls.trustBundleRef", Message: "a trust bundle anchors only a TLS leg, and a plain-HTTP BMC address has none",
			Remediation: "set the BMC address to its https:// ComputerSystem endpoint, or remove tls.trustBundleRef"})
	}
	return issues
}

// validateProviderMediaTrust refuses the one virtual-media trust that is an
// exception: a provider default would hand it to every Machine it hosts.
func validateProviderMediaTrust(bmc api.Value, path string) []api.Issue {
	if bmc.Get("virtualMedia", "tls", "trust").Text() != TrustDisableVerification {
		return nil
	}
	return []api.Issue{{Code: "api.invariant", Field: path + ".virtualMedia.tls.trust",
		Message:     "disableVerification is a per-Machine exception and never a provider default",
		Remediation: "declare hardware.bmc.virtualMedia.tls.trust: disableVerification on each Machine that needs it"}}
}
