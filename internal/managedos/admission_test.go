package managedos

import (
	"context"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/desiredstate/yamlstream"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

func m(kv ...any) api.Value {
	fields := []api.FieldValue{}
	for i := 0; i < len(kv); i += 2 {
		var v api.Value
		switch x := kv[i+1].(type) {
		case api.Value:
			v = x
		case string:
			v = api.StringValue(x)
		case bool:
			v = api.BoolValue(x)
		}
		fields = append(fields, api.FieldValue{Name: kv[i].(string), Value: v})
	}
	return api.MapValue(fields...)
}
func obj(kind api.Kind, name string, spec api.Value) api.Object {
	return api.NewObject(kind, name, m(), spec)
}
func list(v ...api.Value) api.Value { return api.ListValue(v...) }
func anaconda() api.Object {
	return obj(api.MachineInstallProfile, "install", m("os", m("family", "RHEL", "version", "9.6", "architecture", "x86_64"), "installer", m("anaconda", m("imageRef", "image"))))
}
func TestInstallProfileNormalizationAndCustomization(t *testing.T) {
	o := anaconda()
	o = o.WithSpec(o.Spec().With("customizations", m("localization", m("language", "en_US.UTF-8"), "repositories", m("configure", list(m("id", "custom", "baseURL", "https://packages.example.test/repo", "gpgCheck", false))), "security", m("diskEncryption", m("unlock", m("tpm2", m("pcrIds", list(api.IntegerValue("7")))))))))
	machine := obj(api.Machine, "node", m("os", m("provided", false, "installProfileRef", "install")))
	normalized, issues := Normalize(o, api.NewCatalog([]api.Object{machine}))
	if len(issues) > 0 {
		t.Fatal(issues)
	}
	if normalized.Spec().Get("os", "family").Text() != "rhel" || normalized.Spec().Get("customizations", "localization", "formats").Text() != "en_US.UTF-8" {
		t.Fatal("OS normalization missing")
	}
	if normalized.Spec().Get("customizations", "security", "diskEncryption", "unlock", "tpm2", "pcrBank").Text() != "sha256" {
		t.Fatal("PCR bank default missing")
	}
	if got := normalized.Spec().Get("customizations", "services", "enabled").Strings(); len(got) != 1 || got[0] != "sshd" {
		t.Fatal("consumed profile did not enable sshd")
	}
	if issues := Validate(normalized, api.Catalog{}); len(issues) > 0 {
		t.Fatal(issues)
	}
	if o.Spec().Has("customizations", "services") {
		t.Fatal("normalization mutated original")
	}
}
func TestCloneRestrictionsAndProviderBinding(t *testing.T) {
	profile := obj(api.MachineInstallProfile, "clone", m("os", m("family", "rhel", "version", "9", "architecture", "x86_64"), "installer", m("templateClone", m("seed", m("cloudInit", m("growRootFilesystem", true)))), "customizations", m("services", m("enabled", api.StringList("sshd")))))
	provider := obj(api.InfraProvider, "virtual", m("vsphere", m("machineProfiles", list(m("name", "template", "template", "Templates/rhel")))))
	machine := obj(api.Machine, "node", m("os", m("provided", false, "installProfileRef", "clone"), "substrate", m("providerRef", "virtual", "profileRef", "template")))
	c := api.NewCatalog([]api.Object{provider, machine})
	if issues := Validate(profile, c); len(issues) > 0 {
		t.Fatal(issues)
	}
	for _, path := range [][]string{{"localization"}, {"ssh", "initialPassword"}, {"packages"}, {"security", "selinux"}, {"security", "firewall"}, {"security", "fips"}, {"security", "diskEncryption"}} {
		t.Run(strings.Join(path, "/"), func(t *testing.T) {
			s := profile.Spec().WithPath(m(), append([]string{"customizations"}, path...)...)
			if issues := ValidateAuthored(profile.WithSpec(s), c); len(issues) == 0 {
				t.Fatal("clone customization admitted")
			}
		})
	}
	provider = provider.WithSpec(m("libvirt", m("machineProfiles", list(m("name", "template")))))
	if issues := Validate(profile, api.NewCatalog([]api.Object{provider, machine})); len(issues) == 0 {
		t.Fatal("non-vSphere clone admitted")
	}
}
func TestCustomizationContradictions(t *testing.T) {
	cases := map[string]api.Value{
		"whitespace locale":         m("localization", m("language", "en US")),
		"whitespace package":        m("packages", m("install", api.StringList("bad package"))),
		"repository key missing":    m("repositories", m("configure", list(m("id", "custom", "baseURL", "https://example.test/repo", "gpgCheck", true)))),
		"repository bad ID":         m("repositories", m("configure", list(m("id", "bad/id", "gpgCheck", false)))),
		"repository key URL":        m("repositories", m("configure", list(m("id", "repo", "gpgKeyURL", "ftp://example.test/key")))),
		"services overlap":          m("services", m("enabled", api.StringList("sshd"), "disabled", api.StringList("sshd"))),
		"firewall package missing":  m("security", m("firewall", m("enabled", true))),
		"PCRs missing":              m("security", m("diskEncryption", m("unlock", m("tpm2", m("pcrBank", "sha256"))))),
		"unregistered repositories": m("repositories", m("subscription", m("enable", api.StringList("repo")))),
	}
	for name, custom := range cases {
		t.Run(name, func(t *testing.T) {
			o := anaconda()
			o = o.WithSpec(o.Spec().With("customizations", custom))
			if issues := Validate(o, api.Catalog{}); len(issues) == 0 {
				t.Fatal("contradiction admitted")
			}
		})
	}
}
func TestRHSMManagedExternalAndSatellite(t *testing.T) {
	for _, kind := range []string{"redhat-rhel", "redhat-ceph"} {
		t.Run(kind, func(t *testing.T) {
			o := obj(api.Entitlement, "subscription", m("type", kind, "rhsm", m("organizationRef", "organization", "activationKeyRef", "activation", "satellite", m("hostname", "satellite.example.test"))))
			o = NormalizeRHSM(o)
			if o.Spec().Get("rhsm", "management").Text() != "managed" || o.Spec().Get("rhsm", "satellite", "contentBaseURL").Text() != "https://satellite.example.test/pulp/content" {
				t.Fatal("RHSM defaults")
			}
			if issues := ValidateRHSM(o, false); len(issues) > 0 {
				t.Fatal(issues)
			}
		})
	}
	external := obj(api.Entitlement, "external", m("type", "redhat-rhel", "rhsm", m("management", "external")))
	normalized := NormalizeRHSM(external)
	if !normalized.Spec().Equal(external.Spec()) {
		t.Fatal("external RHSM acquired managed defaults")
	}
	if issues := ValidateRHSM(normalized, false); len(issues) > 0 {
		t.Fatal(issues)
	}
	for _, key := range []string{"organizationRef", "activationKeyRef", "connectToInsights", "satellite"} {
		bad := external.WithSpec(external.Spec().WithPath(api.StringValue("supplied"), "rhsm", key))
		if issues := ValidateRHSM(bad, true); len(issues) == 0 {
			t.Fatalf("external accepted %s", key)
		}
	}
	if issues := ValidateRHSM(obj(api.Entitlement, "missing", m("type", "redhat-rhel", "rhsm", m())), true); len(issues) != 2 {
		t.Fatal(issues)
	}
}

// The installation reads boot media only from the host media store, so a
// MachineImage admits exactly local-media:<name> for a name the store admits,
// and every other source is one refusal naming the import that fixes it.
func TestMachineImageAcceptsOnlyStoreMedia(t *testing.T) {
	consumers := api.NewCatalog([]api.Object{anaconda(), obj(api.Environment, "env", m("lifecycle", m("rescue", m("imageRef", "image"))))})
	for _, raw := range []string{"local-media:rhel.iso", "local-media:Image_1.2-test.iso"} {
		o := obj(api.MachineImage, "image", m("bootMedia", raw))
		if issues := Validate(o, consumers); len(issues) > 0 {
			t.Fatal(raw, issues)
		}
	}
	for _, raw := range []string{"https://images.example.test/rhel.iso", "http://images.example.test/rhel.iso", "file:///var/lib/media/rhel.iso",
		"local-media:rhel@9.8+boot.iso", "local-media:CON.iso", "local-media:a_.iso", "local-media:rhel.ISO",
		"local-media:../rhel.iso", "local-media:dir/rhel.iso", "local-media:rhel.img", "ftp://images.example.test/rhel.iso", "rhel.iso"} {
		t.Run(raw, func(t *testing.T) {
			o := obj(api.MachineImage, "image", m("bootMedia", raw))
			issues := Validate(o, consumers)
			if len(issues) != 1 || issues[0].Code != "api.value" || issues[0].Field != "$.spec.bootMedia" ||
				!strings.Contains(issues[0].Remediation, "bootwright media add") || !strings.Contains(issues[0].Remediation, "local-media:") ||
				!strings.Contains(issues[0].Remediation, "MachineImage/image") {
				t.Fatalf("refusal = %#v, want one api.value at $.spec.bootMedia naming the media import", issues)
			}
		})
	}
	image := obj(api.MachineImage, "image", m("bootMedia", "local-media:rhel.iso", "checksum", "  SHA256:"+strings.Repeat("A", 64)+"  "))
	normalized, _ := Normalize(image, api.Catalog{})
	if normalized.Spec().Get("checksum").Text() != strings.Repeat("a", 64) {
		t.Fatal("checksum not normalized")
	}
	if issues := Validate(normalized, consumers); len(issues) != 0 {
		t.Fatal(issues)
	}
}

func hostedTreeProfile(fromMedia string) (api.Object, api.Catalog) {
	server := obj(api.ArtifactServer, "artifacts", m("management", "managed", "machineRef", "controller",
		"listeners", list(m("name", "plain", "protocol", "http"), m("name", "secure", "protocol", "https")),
		"endpoints", list(m("name", "packages", "listenerRef", "plain"), m("name", "media", "listenerRef", "secure"))))
	profile := anaconda()
	profile = profile.WithSpec(profile.Spec().
		WithPath(m("serverRef", "artifacts", "endpointRef", "media"), "installer", "anaconda", "redfishVirtualMedia", "artifactServerEndpoint").
		WithPath(m("fromMedia", fromMedia, "artifactServerEndpoint", m("serverRef", "artifacts", "endpointRef", "packages")), "installer", "anaconda", "packageSource", "hostedTree"))
	return profile, api.NewCatalog([]api.Object{server, obj(api.MachineImage, "image", m("bootMedia", "local-media:boot.iso"))})
}

// A hosted package tree is copied from a DVD image of the host media store,
// the only source the installation extracts a tree from.
func TestHostedTreeAcceptsOnlyStoreMedia(t *testing.T) {
	const field = "$.spec.installer.anaconda.packageSource.hostedTree.fromMedia"
	profile, catalog := hostedTreeProfile("local-media:dvd.iso")
	if issues := Validate(profile, catalog); len(issues) != 0 {
		t.Fatal(issues)
	}
	for _, raw := range []string{"file:///var/lib/media/dvd.iso", "local-media:CON.iso", "https://images.example.test/dvd.iso"} {
		t.Run(raw, func(t *testing.T) {
			profile, catalog := hostedTreeProfile(raw)
			issues := Validate(profile, catalog)
			if len(issues) != 1 || issues[0].Code != "api.value" || issues[0].Field != field ||
				!strings.Contains(issues[0].Remediation, "bootwright media add") || !strings.Contains(issues[0].Remediation, "MachineInstallProfile/install") {
				t.Fatalf("refusal = %#v, want one api.value at %s naming the media import", issues, field)
			}
		})
	}
	profile, catalog = hostedTreeProfile("local-media:boot.iso")
	issues := Validate(profile, catalog)
	if len(issues) != 1 || issues[0].Field != field || !strings.Contains(issues[0].Remediation, "MachineImage/image") {
		t.Fatalf("refusal = %#v, want the boot image refused as the tree's media", issues)
	}
}

// Every installation boots its installer through Redfish virtual media, so a
// profile that a Bootwright-installed Machine selects names the endpoint its
// image is published through, whatever the substrate, once per profile.
func TestAnInstalledConsumerNeedsTheVirtualMediaEndpoint(t *testing.T) {
	const field = "$.spec.installer.anaconda.redfishVirtualMedia.artifactServerEndpoint"
	provider := obj(api.InfraProvider, "virtual", m("libvirt", m("machineProfiles", list(m("name", "small")))))
	virtual := obj(api.Machine, "virtual-node", m("os", m("provided", false, "installProfileRef", "install"), "substrate", m("providerRef", "virtual", "profileRef", "small")))
	loose := obj(api.Machine, "loose-node", m("os", m("provided", false, "installProfileRef", "install")))
	for name, objects := range map[string][]api.Object{
		"a libvirt consumer":        {provider, virtual},
		"a provider-less consumer":  {loose},
		"two consumers report once": {provider, virtual, loose},
	} {
		t.Run(name, func(t *testing.T) {
			issues := Validate(anaconda(), api.NewCatalog(objects))
			if len(issues) != 1 || issues[0].Code != "api.invariant" || issues[0].Field != field ||
				!strings.Contains(issues[0].Remediation, "MachineInstallProfile/install") {
				t.Fatalf("refusal = %#v, want one api.invariant at %s naming the profile", issues, field)
			}
		})
	}
	provided := obj(api.Machine, "provided-node", m("os", m("provided", true, "installProfileRef", "install")))
	for name, catalog := range map[string]api.Catalog{"a provided Machine": api.NewCatalog([]api.Object{provided}), "no consumer": {}} {
		if issues := Validate(anaconda(), catalog); len(issues) != 0 {
			t.Fatalf("%s: %v", name, issues)
		}
	}
}

// A Machine's installer image is published beneath os/<machine>/ and a
// profile's package tree beneath os/<profile>/, so a Machine and a profile of
// one name that publish through one server would share a directory whose
// removal takes both.
func TestAMachineAndAProfileOfOneNameShareNoServedDirectory(t *testing.T) {
	const field = "$.spec.installer.anaconda.packageSource.hostedTree.artifactServerEndpoint.serverRef"
	endpoints := m("listeners", list(m("name", "plain", "protocol", "http"), m("name", "secure", "protocol", "https")),
		"endpoints", list(m("name", "packages", "listenerRef", "plain"), m("name", "media", "listenerRef", "secure")))
	server := func(name string) api.Object {
		return obj(api.ArtifactServer, name, endpoints.With("management", api.StringValue("managed")).With("machineRef", api.StringValue("controller")))
	}
	profile := func(name, treeServer string) api.Object {
		p := obj(api.MachineInstallProfile, name, anaconda().Spec())
		return p.WithSpec(p.Spec().
			WithPath(m("serverRef", "lab-artifacts", "endpointRef", "media"), "installer", "anaconda", "redfishVirtualMedia", "artifactServerEndpoint").
			WithPath(m("fromMedia", "local-media:dvd.iso", "artifactServerEndpoint", m("serverRef", treeServer, "endpointRef", "packages")), "installer", "anaconda", "packageSource", "hostedTree"))
	}
	machine := func(name, selected string, provided bool) api.Object {
		return obj(api.Machine, name, m("os", m("provided", provided, "installProfileRef", selected)))
	}
	servers := []api.Object{server("lab-artifacts"), server("other-artifacts")}
	refused := map[string]struct {
		profile api.Object
		others  []api.Object
	}{
		"the Machine selects that profile":    {profile("rhel-01", "lab-artifacts"), []api.Object{machine("rhel-01", "rhel-01", false)}},
		"the Machine selects another profile": {profile("rhel-01", "lab-artifacts"), []api.Object{profile("rhel-9", "lab-artifacts"), machine("rhel-01", "rhel-9", false), machine("rhel-02", "rhel-01", false)}},
	}
	for name, test := range refused {
		t.Run(name, func(t *testing.T) {
			issues := Validate(test.profile, api.NewCatalog(append(append([]api.Object{test.profile}, servers...), test.others...)))
			if len(issues) != 1 || issues[0].Code != "api.invariant" || issues[0].Field != field {
				t.Fatalf("refusal = %#v, want one api.invariant at %s", issues, field)
			}
			for _, named := range []string{"os/rhel-01/", "Machine/rhel-01", "ArtifactServer/lab-artifacts"} {
				if !strings.Contains(issues[0].Message, named) {
					t.Errorf("message %q does not name %s", issues[0].Message, named)
				}
			}
			if !strings.Contains(issues[0].Remediation, "rename Machine/rhel-01 or MachineInstallProfile/rhel-01") {
				t.Errorf("remediation %q does not name the rename", issues[0].Remediation)
			}
		})
	}
	admitted := map[string]struct {
		profile api.Object
		others  []api.Object
	}{
		"the tree uses another server":      {profile("rhel-01", "other-artifacts"), []api.Object{machine("rhel-01", "rhel-01", false)}},
		"the Machine is provided":           {profile("rhel-01", "lab-artifacts"), []api.Object{profile("rhel-9", "lab-artifacts"), machine("rhel-01", "rhel-9", true), machine("rhel-02", "rhel-01", false)}},
		"the profile has no installed user": {profile("rhel-01", "lab-artifacts"), []api.Object{profile("rhel-9", "lab-artifacts"), machine("rhel-01", "rhel-9", false), machine("rhel-02", "rhel-01", true)}},
	}
	for name, test := range admitted {
		t.Run(name, func(t *testing.T) {
			if issues := Validate(test.profile, api.NewCatalog(append(append([]api.Object{test.profile}, servers...), test.others...))); len(issues) != 0 {
				t.Fatal(issues)
			}
		})
	}
}

// A server placed on the Machine an installation lays down cannot serve that
// installation, and the refusal says so rather than the opposite rule.
func TestTheBootstrapCycleIsStatedAsRefused(t *testing.T) {
	profile, catalog := hostedTreeProfile("local-media:dvd.iso")
	objects := []api.Object{obj(api.Machine, "node", m("os", m("provided", false, "installProfileRef", "install")))}
	for _, existing := range catalog.Objects() {
		if existing.Kind() == api.ArtifactServer {
			existing = existing.WithSpec(existing.Spec().With("machineRef", api.StringValue("node")))
		}
		objects = append(objects, existing)
	}
	issues := Validate(profile, api.NewCatalog(objects))
	for _, path := range []string{"redfishVirtualMedia", "packageSource.hostedTree"} {
		field := "$.spec.installer.anaconda." + path + ".artifactServerEndpoint.serverRef"
		found := issuesAt(issues, field)
		if len(found) != 1 || found[0].Code != "api.invariant" ||
			found[0].Message != "an installation cannot publish through an artifact server placed on the Machine it installs, because that server cannot serve until the installation completes" ||
			strings.Contains(found[0].Message, "requires an artifact server hosted on") ||
			!strings.Contains(found[0].Remediation, "ArtifactServer/artifacts") || !strings.Contains(found[0].Remediation, "controller") {
			t.Errorf("refusal at %s = %#v, want the cycle stated as refused with a remedy naming the server and the controller", field, found)
		}
	}
}

func issuesAt(issues []api.Issue, field string) []api.Issue {
	var found []api.Issue
	for _, issue := range issues {
		if issue.Field == field {
			found = append(found, issue)
		}
	}
	return found
}

// Every managed-OS admission refusal names the exact field or object whose
// change clears it, instead of one slogan shared by every rule.
func TestEveryManagedOSAdmissionRefusalNamesItsRemedy(t *testing.T) {
	const slogan = "make OS installation intent consistent with its consumers and references"
	profile := func(edit func(api.Value) api.Value) api.Object {
		o := anaconda()
		return o.WithSpec(edit(o.Spec()))
	}
	custom := func(value api.Value) api.Object {
		return profile(func(s api.Value) api.Value { return s.With("customizations", value) })
	}
	clone := obj(api.MachineInstallProfile, "install", m("os", m("family", "rhel", "version", "9", "architecture", "x86_64"), "installer", m("templateClone", m())))
	provider := obj(api.InfraProvider, "virtual", m("libvirt", m("machineProfiles", list(m("name", "small")))))
	consumer := obj(api.Machine, "node", m("os", m("provided", false, "installProfileRef", "install"), "substrate", m("providerRef", "virtual", "profileRef", "small")))
	consumed := api.NewCatalog([]api.Object{provider, consumer})
	served, servedCatalog := hostedTreeProfile("local-media:dvd.iso")
	cycle := []api.Object{consumer}
	for _, existing := range servedCatalog.Objects() {
		if existing.Kind() == api.ArtifactServer {
			existing = existing.WithSpec(existing.Spec().With("machineRef", api.StringValue("node")))
		}
		cycle = append(cycle, existing)
	}
	sameName := obj(api.MachineInstallProfile, "node", served.Spec())
	rhel := func(rhsm api.Value) api.Object {
		return obj(api.Entitlement, "rhel", m("type", "redhat-rhel", "rhsm", rhsm))
	}
	registered := func(value api.Value) api.Object {
		return profile(func(s api.Value) api.Value {
			return s.With("subscription", m("entitlementRef", "rhel")).With("customizations", m("repositories", m("subscription", value)))
		})
	}
	cases := map[string]struct {
		issues []api.Issue
		field  string
	}{
		"OS family":              {Validate(profile(func(s api.Value) api.Value { return s.WithPath(api.StringValue("debian"), "os", "family") }), api.Catalog{}), "$.spec.os.family"},
		"OS version":             {Validate(profile(func(s api.Value) api.Value { return s.WithPath(api.StringValue("8.10"), "os", "version") }), api.Catalog{}), "$.spec.os.version"},
		"repository key missing": {Validate(custom(m("repositories", m("configure", list(m("id", "custom", "baseURL", "https://example.test/repo"))))), api.Catalog{}), "$.spec.customizations.repositories.configure[0].gpgKeyURL"},
		"repository key URL":     {Validate(custom(m("repositories", m("configure", list(m("id", "repo", "gpgKeyURL", "ftp://example.test/key"))))), api.Catalog{}), "$.spec.customizations.repositories.configure[0].gpgKeyURL"},
		"two registrations": {Validate(profile(func(s api.Value) api.Value {
			return s.With("subscription", m("entitlementRef", "rhel")).WithPath(m("entitlementRef", "rhel"), "installer", "anaconda", "packageSource", "fromSubscription")
		}), api.Catalog{}), "$.spec.subscription"},
		"a non-RHEL entitlement":            {Validate(profile(func(s api.Value) api.Value { return s.With("subscription", m("entitlementRef", "ceph")) }), api.NewCatalog([]api.Object{obj(api.Entitlement, "ceph", m("type", "redhat-ceph"))})), "$.spec.subscription.entitlementRef"},
		"empty subscription repositories":   {Validate(registered(m()), api.Catalog{}), "$.spec.customizations.repositories.subscription"},
		"unregistered repositories":         {Validate(custom(m("repositories", m("subscription", m("enable", api.StringList("repo"))))), api.Catalog{}), "$.spec.customizations.repositories.subscription"},
		"an enabled wildcard":               {Validate(registered(m("enable", api.StringList("*"))), api.Catalog{}), "$.spec.customizations.repositories.subscription.enable"},
		"an enabled and disabled ID":        {Validate(registered(m("enable", api.StringList("repo"), "disable", api.StringList("repo"))), api.Catalog{}), "$.spec.customizations.repositories.subscription"},
		"a disabled ID":                     {Validate(registered(m("disable", api.StringList("bad/id"))), api.Catalog{}), "$.spec.customizations.repositories.subscription.disable"},
		"an enabled and disabled service":   {Validate(custom(m("services", m("enabled", api.StringList("sshd"), "disabled", api.StringList("sshd")))), api.Catalog{}), "$.spec.customizations.services"},
		"a firewall without firewalld":      {Validate(custom(m("security", m("firewall", m("enabled", true)))), api.Catalog{}), "$.spec.customizations.security.firewall.enabled"},
		"a PCR bank without PCRs":           {Validate(custom(m("security", m("diskEncryption", m("unlock", m("tpm2", m("pcrBank", "sha256")))))), api.Catalog{}), "$.spec.customizations.security.diskEncryption.unlock.tpm2.pcrBank"},
		"a server on the installed Machine": {Validate(served, api.NewCatalog(cycle)), "$.spec.installer.anaconda.redfishVirtualMedia.artifactServerEndpoint.serverRef"},
		"tree media off the store":          {Validate(hostedTreeProfile("file:///media/dvd.iso")), "$.spec.installer.anaconda.packageSource.hostedTree.fromMedia"},
		"tree media is the boot image":      {Validate(hostedTreeProfile("local-media:boot.iso")), "$.spec.installer.anaconda.packageSource.hostedTree.fromMedia"},
		"a clone on libvirt":                {Validate(clone, consumed), "$.spec.installer.templateClone"},
		"no virtual media endpoint":         {Validate(anaconda(), consumed), "$.spec.installer.anaconda.redfishVirtualMedia.artifactServerEndpoint"},
		"encryption without a TPM":          {Validate(custom(m("security", m("diskEncryption", m()))), consumed), "$.spec.customizations.security.diskEncryption"},
		"a shared served directory":         {Validate(sameName, api.NewCatalog(append(servedCatalog.Objects(), sameName, obj(api.Machine, "node", m("os", m("provided", false, "installProfileRef", "node")))))), "$.spec.installer.anaconda.packageSource.hostedTree.artifactServerEndpoint.serverRef"},
		"boot media off the store":          {Validate(obj(api.MachineImage, "image", m("bootMedia", "https://images.example.test/rhel.iso")), api.Catalog{}), "$.spec.bootMedia"},
		"no RHSM intent":                    {Validate(obj(api.Entitlement, "rhel", m("type", "redhat-rhel")), api.Catalog{}), "$.spec.rhsm"},
		"external RHSM detail":              {Validate(rhel(m("management", "external", "organizationRef", "organization")), api.Catalog{}), "$.spec.rhsm.organizationRef"},
		"managed RHSM without keys":         {Validate(rhel(m("management", "managed")), api.Catalog{}), "$.spec.rhsm.activationKeyRef"},
		"a clone customization":             {ValidateAuthored(clone.WithSpec(clone.Spec().With("customizations", m("packages", m()))), api.Catalog{}), "$.spec.customizations.packages"},
		"partial external RHSM detail":      {ValidatePartial(rhel(m("management", "external", "satellite", m())), api.Catalog{}), "$.spec.rhsm.satellite"},
		"partial service overlap":           {ValidatePartial(custom(m("services", m("enabled", api.StringList("sshd"), "disabled", api.StringList("sshd")))), api.Catalog{}), "$.spec.customizations.services"},
		"partial two registrations": {ValidatePartial(profile(func(s api.Value) api.Value {
			return s.With("subscription", m("entitlementRef", "rhel")).WithPath(m("entitlementRef", "rhel"), "installer", "anaconda", "packageSource", "fromSubscription")
		}), api.Catalog{}), "$.spec.subscription"},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			if len(issuesAt(test.issues, test.field)) == 0 {
				t.Fatalf("no refusal at %s: %#v", test.field, test.issues)
			}
			for _, issue := range test.issues {
				if issue.Remediation == "" || issue.Remediation == slogan || !strings.Contains(issue.Remediation, "spec.") && !strings.Contains(issue.Remediation, "/") {
					t.Errorf("refusal at %s carries remediation %q, want the exact field or object to change", issue.Field, issue.Remediation)
				}
			}
		})
	}
}

// Every customization the Kickstart carries is one token of its own grammar,
// so none can end its line, open a section or add an option. Each refusal
// names the exact entry and the grammar it breaks.
func TestKickstartBoundCustomizationsHaveAGrammar(t *testing.T) {
	cases := map[string]struct {
		custom api.Value
		field  string
	}{
		"service opening a section":  {m("services", m("enabled", api.StringList("sshd\n%post\nPROBE\n%end"))), "$.spec.customizations.services.enabled[0]"},
		"service holding a space":    {m("services", m("disabled", api.StringList("my service"))), "$.spec.customizations.services.disabled[0]"},
		"package closing a section":  {m("packages", m("install", api.StringList("chrony", "%end"))), "$.spec.customizations.packages.install[1]"},
		"package opening a section":  {m("packages", m("install", api.StringList("%post"))), "$.spec.customizations.packages.install[0]"},
		"package exclusion":          {m("packages", m("install", api.StringList("-kernel"))), "$.spec.customizations.packages.install[0]"},
		"keyboard opening a section": {m("localization", m("keyboard", "%post")), "$.spec.customizations.localization.keyboard"},
		"keyboard holding a comment": {m("localization", m("keyboard", "us#x")), "$.spec.customizations.localization.keyboard"},
		"locale list separator":      {m("localization", m("additionalLocales", api.StringList("en_US,de_DE"))), "$.spec.customizations.localization.additionalLocales[0]"},
		"repository ID comment":      {m("repositories", m("configure", list(m("id", "a#b", "baseURL", "https://mirror.example.test/extras", "gpgCheck", false)))), "$.spec.customizations.repositories.configure[0].id"},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			o := anaconda()
			o = o.WithSpec(o.Spec().With("customizations", test.custom))
			issues := Validate(o, api.Catalog{})
			if len(issues) != 1 || issues[0].Code != "api.value" || issues[0].Field != test.field || issues[0].Message == "" ||
				!strings.Contains(issues[0].Remediation, strings.TrimPrefix(test.field, "$.")) {
				t.Fatalf("refusal = %#v, want one api.value at %s naming its field", issues, test.field)
			}
		})
	}
	o := anaconda()
	o = o.WithSpec(o.Spec().With("customizations", m(
		"localization", m("language", "en_US.UTF-8", "formats", "en_US.UTF-8", "keyboard", "us", "timezone", "Etc/UTC", "additionalLocales", api.StringList("sr_RS@latin")),
		"packages", m("install", api.StringList("chrony", "qemu-guest-agent", "@container-tools", "kernel-*")),
		"services", m("enabled", api.StringList("chronyd", "sshd", "cockpit.socket", "getty@tty1.service")),
	)))
	if issues := Validate(o, api.Catalog{}); len(issues) != 0 {
		t.Fatalf("valid Kickstart-bound customizations were refused: %v", issues)
	}
}

// The installation reads no hostname source, root-device source or package
// environment, so the closed schema refuses each as an unknown field instead of
// admitting a value the installed system would silently ignore.
func TestTheInstallProfileRefusesFieldsTheInstallationNeverReads(t *testing.T) {
	for name, test := range map[string]struct{ customizations, field string }{
		"hostname.source":      {"hostname: {source: machineName}", "$.spec.customizations.hostname"},
		"storage.rootDevice":   {"storage: {rootDevice: {source: machineRootDeviceHints}}", "$.spec.customizations.storage"},
		"packages.environment": {"packages: {environment: minimal}", "$.spec.customizations.packages.environment"},
	} {
		t.Run(name, func(t *testing.T) {
			content := `apiVersion: bootwright.io/v1alpha1
kind: Environment
metadata: {name: synthetic}
spec:
  domains: {base: example.test}
  controller: {machineRef: controller}
---
apiVersion: bootwright.io/v1alpha1
kind: Machine
metadata: {name: controller}
spec: {os: {provided: true}}
---
apiVersion: bootwright.io/v1alpha1
kind: MachineInstallProfile
metadata: {name: install}
spec:
  os: {family: rhel, version: "9.8", architecture: x86_64}
  installer: {anaconda: {imageRef: image}}
  customizations: {` + test.customizations + `}
`
			input := desiredstate.Sources{Files: []desiredstate.SourceFile{desiredstate.NewSourceFile("/synthetic/profile.yaml", []byte(content))}, Roots: []string{"/synthetic"}}
			_, _, err := compilation.NewCompiler(yamlstream.Parser{}, nil).Compile(context.Background(), input)
			var refused []diagnostics.Diagnostic
			for _, reported := range diagnostics.Of(err) {
				if strings.HasPrefix(reported.Field, "$.spec.customizations") {
					refused = append(refused, reported)
				}
			}
			if len(refused) != 1 || refused[0].Code != "api.field" || refused[0].Field != test.field || !strings.HasPrefix(refused[0].Message, "unknown field") {
				t.Fatalf("refusals = %#v, want one api.field unknown-field refusal at %s", refused, test.field)
			}
		})
	}
}

// A configured repository ID is what dnf accepts in one, since it names the
// section and the /etc/yum.repos.d/bootwright-<id>.repo file the installation
// writes.
func TestAConfiguredRepositoryIDIsWhatDnfAccepts(t *testing.T) {
	configure := func(id string) []api.Issue {
		o := anaconda()
		o = o.WithSpec(o.Spec().With("customizations", m("repositories", m("configure", list(m("id", id, "baseURL", "https://mirror.example.test/x", "gpgCheck", false))))))
		return Validate(o, api.Catalog{})
	}
	const field = "$.spec.customizations.repositories.configure[0].id"
	for _, id := range []string{"a@b", "x*y", "a=b", "[x]", "a;b", ".", "..", "", strings.Repeat("a", 240)} {
		issues := configure(id)
		if len(issues) != 1 || issues[0].Code != "api.value" || issues[0].Field != field ||
			issues[0].Message != repositoryGrammar.message || !strings.Contains(issues[0].Remediation, strings.TrimPrefix(field, "$.")) {
			t.Errorf("repository ID %q: refusal = %#v, want one api.value at %s", id, issues, field)
		}
	}
	for _, id := range []string{"extras", "rhel-9:appstream_x.1", strings.Repeat("a", 239)} {
		if issues := configure(id); len(issues) != 0 {
			t.Errorf("repository ID %q was refused: %#v", id, issues)
		}
	}
}

// A subscription repository ID may be a pattern subscription-manager expands,
// so the dnf rule of a configured ID does not narrow it.
func TestASubscriptionRepositoryIDMayBeAPattern(t *testing.T) {
	o := anaconda()
	o = o.WithSpec(o.Spec().With("subscription", m("entitlementRef", "rhel")).With("customizations",
		m("repositories", m("subscription", m("enable", api.StringList("rhel-9-for-x86_64-*"), "disable", api.StringList("rhel-9-for-x86_64-supplementary-rpms"))))))
	if issues := Validate(o, api.Catalog{}); len(issues) != 0 {
		t.Fatalf("subscription repository patterns were refused: %#v", issues)
	}
}

// A mirror repository ID follows the configured-repository ID rule before the
// mirror arm is supported, so the arm cannot later carry an ID its Kickstart
// directive could not.
func TestAMirrorRepositoryIDIsAKickstartToken(t *testing.T) {
	mirror := func(id string) []api.Issue {
		o := anaconda()
		o = o.WithSpec(o.Spec().WithPath(m("baseURL", "https://mirror.example.test/os",
			"repositories", list(m("id", "base", "baseURL", "https://mirror.example.test/base"), m("id", id, "baseURL", "https://mirror.example.test/x"))),
			"installer", "anaconda", "packageSource", "mirror"))
		return Validate(o, api.Catalog{})
	}
	const field = "$.spec.installer.anaconda.packageSource.mirror.repositories[1].id"
	for _, id := range []string{"a b", "x/y", "a@b"} {
		issues := mirror(id)
		if len(issues) != 1 || issues[0].Code != "api.value" || issues[0].Field != field ||
			issues[0].Message != repositoryGrammar.message || !strings.Contains(issues[0].Remediation, strings.TrimPrefix(field, "$.")) {
			t.Fatalf("mirror repository ID %q: refusal = %#v, want one api.value at %s", id, issues, field)
		}
	}
	if issues := mirror("extras"); len(issues) != 0 {
		t.Fatalf("mirror repository ID extras was refused: %#v", issues)
	}
}

// A repository's display name and GPG key URL are written into the .repo file
// the installation renders, so admission refuses what the renderer's guard
// would: a display name of more than one line, and a key URL holding a quote,
// a backslash or '#'.
func TestARepositoryDisplayNameAndKeyURLAreWhatTheRepoFileCarries(t *testing.T) {
	configure := func(entry api.Value) []api.Issue {
		o := anaconda()
		o = o.WithSpec(o.Spec().With("customizations", m("repositories", m("configure", list(entry)))))
		return Validate(o, api.Catalog{})
	}
	for name, test := range map[string]struct {
		entry api.Value
		field string
	}{
		"a two-line name":    {m("id", "extras", "baseURL", "https://mirror.example.test/x", "gpgCheck", false, "displayName", "a\nb"), "$.spec.customizations.repositories.configure[0].displayName"},
		"a separator name":   {m("id", "extras", "baseURL", "https://mirror.example.test/x", "gpgCheck", false, "displayName", "a b"), "$.spec.customizations.repositories.configure[0].displayName"},
		"a quoted key URL":   {m("id", "extras", "baseURL", "https://mirror.example.test/x", "gpgKeyURL", `https://mirror.example.test/"key`), "$.spec.customizations.repositories.configure[0].gpgKeyURL"},
		"a key URL with '#'": {m("id", "extras", "baseURL", "https://mirror.example.test/x", "gpgKeyURL", "https://mirror.example.test/key#"), "$.spec.customizations.repositories.configure[0].gpgKeyURL"},
	} {
		t.Run(name, func(t *testing.T) {
			if issues := configure(test.entry); len(issues) != 1 || issues[0].Field != test.field || issues[0].Remediation == "" {
				t.Fatalf("refusal = %#v, want one at %s", issues, test.field)
			}
		})
	}
	if issues := configure(m("id", "extras", "baseURL", "https://mirror.example.test/x", "displayName", "Extra packages #1",
		"gpgKeyURL", "file:///etc/pki/rpm-gpg/RPM-GPG-KEY-redhat-release")); len(issues) != 0 {
		t.Fatalf("a valid repository was refused: %#v", issues)
	}
}
