package managedos

import (
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func updating(fields ...any) api.Object {
	o := anaconda()
	spec := o.Spec().Without("updates")
	for index := 0; index+1 < len(fields); index += 2 {
		spec = spec.With(fields[index].(string), fields[index+1].(api.Value))
	}
	return o.WithSpec(spec)
}

func updateIssues(o api.Object, c api.Catalog) []api.Issue {
	var found []api.Issue
	for _, reported := range Validate(o, c) {
		if reported.Field == "$.spec.updates.policy" || reported.Field == "$.spec.customizations.security.diskEncryption.unlock.tpm2.pcrIds" {
			found = append(found, reported)
		}
	}
	return found
}

func TestAnOmittedUpdatePolicyNormalizesToWithinRelease(t *testing.T) {
	effective, _ := Normalize(updating(), api.Catalog{})
	if got := effective.Spec().Get("updates", "policy").Text(); got != UpdateWithinRelease || !UpdatesWithinRelease(effective) {
		t.Fatalf("an omitted policy normalizes to %q", got)
	}
	kept, _ := Normalize(updating("updates", m("policy", "none")), api.Catalog{})
	if got := kept.Spec().Get("updates", "policy").Text(); got != UpdateNone || UpdatesWithinRelease(kept) {
		t.Fatalf("a none policy normalizes to %q", got)
	}
}

func TestUpdatingWithinTheReleaseNeedsAnUpdateSource(t *testing.T) {
	managed := obj(api.Entitlement, "rhel", m("type", "redhatRhel", "rhsm", m("management", "managed", "organizationRef", "org", "activationKeyRef", "key")))
	implicit := obj(api.Entitlement, "rhel", m("type", "redhatRhel", "rhsm", m("organizationRef", "org", "activationKeyRef", "key")))
	external := obj(api.Entitlement, "rhel", m("type", "redhatRhel", "rhsm", m("management", "external")))
	subscription := m("entitlementRef", "rhel")
	repository := func(enabled ...bool) api.Value {
		entry := m("id", "mirror", "baseURL", "https://mirror.example.test/rhel/$releasever/", "gpgCheck", false)
		if len(enabled) != 0 {
			entry = entry.With("enabled", api.BoolValue(enabled[0]))
		}
		return m("repositories", m("configure", list(entry)))
	}
	refused := api.Issue{Code: "api.invariant", Field: "$.spec.updates.policy",
		Message:     "MachineInstallProfile/install updates within its release but declares no update source: no managed subscription and no enabled configured repository",
		Remediation: "set spec.updates.policy to none on MachineInstallProfile/install, or declare a managed spec.subscription or an enabled spec.customizations.repositories.configure entry"}
	for name, test := range map[string]struct {
		profile api.Object
		catalog api.Catalog
		refused bool
	}{
		"no source":                         {updating(), api.Catalog{}, true},
		"an explicit withinRelease":         {updating("updates", m("policy", "withinRelease")), api.Catalog{}, true},
		"an external entitlement alone":     {updating("subscription", subscription), api.NewCatalog([]api.Object{external}), true},
		"a disabled repository alone":       {updating("customizations", repository(false)), api.Catalog{}, true},
		"a managed entitlement":             {updating("subscription", subscription), api.NewCatalog([]api.Object{managed}), false},
		"an entitlement managed by default": {updating("subscription", subscription), api.NewCatalog([]api.Object{implicit}), false},
		"an enabled repository":             {updating("customizations", repository(true)), api.Catalog{}, false},
		"a repository enabled by default":   {updating("customizations", repository()), api.Catalog{}, false},
		"an external entitlement and a repository": {updating("subscription", subscription, "customizations", repository()),
			api.NewCatalog([]api.Object{external}), false},
		"an entitlement the graph lacks": {updating("subscription", subscription), api.Catalog{}, false},
		"policy none":                    {updating("updates", m("policy", "none")), api.Catalog{}, false},
	} {
		issues := updateIssues(test.profile, test.catalog)
		if test.refused && (len(issues) != 1 || issues[0] != refused) {
			t.Errorf("%s: refusals = %#v, want %#v", name, issues, refused)
		}
		if !test.refused && len(issues) != 0 {
			t.Errorf("%s: refused %#v", name, issues)
		}
	}
}

func TestUpdatingWithinTheReleaseRefusesASealedBootMeasurement(t *testing.T) {
	sealed := func(pcrs ...string) api.Value {
		values := make([]api.Value, len(pcrs))
		for index, pcr := range pcrs {
			values[index] = api.IntegerValue(pcr)
		}
		return m("repositories", m("configure", list(m("id", "mirror", "baseURL", "https://mirror.example.test/rhel/", "gpgCheck", false))),
			"security", m("diskEncryption", m("unlock", m("tpm2", m("pcrIds", list(values...))), "recoveryPassphraseRef", "recovery")))
	}
	refused := api.Issue{Code: "api.invariant", Field: "$.spec.customizations.security.diskEncryption.unlock.tpm2.pcrIds",
		Message: "MachineInstallProfile/install seals its root disk to PCR 4, 8 or 9, which measure the boot loader, its commands and the kernel " +
			"an update within the release replaces, so the disk would not unlock after the update's reboot",
		Remediation: "remove 4, 8 and 9 from spec.customizations.security.diskEncryption.unlock.tpm2.pcrIds on MachineInstallProfile/install, " +
			"or set spec.updates.policy to none on MachineInstallProfile/install"}
	for _, pcrs := range [][]string{{"4"}, {"7", "8"}, {"9"}, {"0", "4", "7", "8", "9"}} {
		if issues := updateIssues(updating("customizations", sealed(pcrs...)), api.Catalog{}); len(issues) != 1 || issues[0] != refused {
			t.Errorf("PCRs %v: refusals = %#v", pcrs, issues)
		}
		if issues := updateIssues(updating("customizations", sealed(pcrs...), "updates", m("policy", "none")), api.Catalog{}); len(issues) != 0 {
			t.Errorf("PCRs %v with policy none refused %#v", pcrs, issues)
		}
	}
	for _, pcrs := range [][]string{{"7"}, {"0", "1", "7"}, {"14", "23"}} {
		if issues := updateIssues(updating("customizations", sealed(pcrs...)), api.Catalog{}); len(issues) != 0 {
			t.Errorf("PCRs %v refused %#v", pcrs, issues)
		}
	}
}
