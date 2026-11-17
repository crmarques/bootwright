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
	tls := bmc.Get("tls")
	if mode := tls.Get("verification").Text(); mode != "" {
		tls = tls.Default("verify", api.BoolValue(mode != "none"))
	}
	bmc = bmc.With("tls", tls.Default("verify", api.BoolValue(true)))
	media := bmc.Get("virtualMedia")
	tls = media.Get("tls").Default("trust", api.StringValue(TrustImportCertificate))
	switch tls.Get("trust").Text() {
	case "disableVerification":
		tls = tls.Default("restoreVerificationAfterBoot", api.BoolValue(true))
	case "importCertificate":
		tls = tls.Default("removeCertificateAfterBoot", api.BoolValue(false))
	}
	if tls.Get("trust").Text() != TrustDisableVerification {
		verified := bmc.Get("tls", "verify").Bool() && bmc.Get("tls", "verification").Text() != "none"
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
			(controllerTLS.Has("verify") && !controllerTLS.Get("verify").Bool()) ||
			(bmc.Has("address") && !strings.HasPrefix(bmc.Get("address").Text(), "https://")) {
			issues = api.AppendIssues(issues, issue(path+".virtualMedia.tls.enableVerification", "enabling media verification requires verified HTTPS BMC access and importCertificate or established trust"))
		}
	}
	return issues
}

// validateControllerTrust refuses a bundle beside verification turned off. It
// reads the value it is given, so an authored block and an effective one that
// inherited either half are held to the same rule.
func validateControllerTrust(bmc api.Value, path string) []api.Issue {
	tls := bmc.Get("tls")
	var issues []api.Issue
	if tls.Get("verification").Text() == "certificate" && bmc.Has("address") && !strings.HasPrefix(bmc.Get("address").Text(), "https://") {
		issues = append(issues, api.Issue{Code: "api.invariant", Field: path + ".tls.verification", Message: "certificate verification requires an HTTPS controller endpoint", Remediation: "set the BMC address to its HTTPS ComputerSystem endpoint"})
	}
	if !tls.Has("trustBundleRef") || !tls.Has("verify") || tls.Get("verify").Bool() {
		return issues
	}
	return append(issues, api.Issue{Code: "api.invariant", Field: path + ".tls.trustBundleRef",
		Message:     "a BMC trust bundle requires verification, and tls.verify is false here, whether authored or inherited",
		Remediation: "set tls.verify: true where the bundle is declared, or remove tls.trustBundleRef"})
}

// validateProviderMediaTrust refuses the one virtual-media trust that is an
// exception: a provider default would hand it to every Machine it hosts.
func validateProviderMediaTrust(bmc api.Value, path string) []api.Issue {
	if bmc.Get("virtualMedia", "tls", "trust").Text() != TrustDisableVerification {
		return nil
	}
	return []api.Issue{{Code: "api.invariant", Field: path + ".virtualMedia.tls.trust",
		Message:     "disableVerification is a per-Machine exception and never a provider default",
		Remediation: "declare substrate.baremetal.bmc.virtualMedia.tls.trust: disableVerification on each Machine that needs it"}}
}
