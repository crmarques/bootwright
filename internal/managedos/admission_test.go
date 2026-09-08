package managedos

import (
	api "github.com/crmarques/bootwright/api/v1alpha1"
	"strings"
	"testing"
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
	for _, path := range [][]string{{"localization"}, {"ssh", "initialPassword"}, {"storage"}, {"packages"}, {"security", "selinux"}, {"security", "firewall"}, {"security", "fips"}, {"security", "diskEncryption"}} {
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
func TestMachineImageMediaAndPinning(t *testing.T) {
	for _, raw := range []string{"local-media:rhel.iso", "file:///var/lib/media/rhel.iso", "https://images.example.test/rhel.iso", "http://images.example.test/rhel.iso"} {
		o := obj(api.MachineImage, "image", m("bootMedia", raw))
		if issues := Validate(o, api.Catalog{}); len(issues) > 0 {
			t.Fatal(raw, issues)
		}
	}
	for _, raw := range []string{"local-media:../rhel.iso", "local-media:dir/rhel.iso", "local-media:rhel.img", "file://remote/rhel.iso", "ftp://images.example.test/rhel.iso", "https://user:pass@images.example.test/rhel.iso"} {
		o := obj(api.MachineImage, "image", m("bootMedia", raw))
		if issues := Validate(o, api.Catalog{}); len(issues) == 0 {
			t.Fatal("unsafe media admitted", raw)
		}
	}
	image := obj(api.MachineImage, "image", m("bootMedia", "https://images.example.test/rhel.iso"))
	if issues := Validate(image, api.NewCatalog([]api.Object{anaconda()})); len(issues) == 0 {
		t.Fatal("consumed remote image admitted without checksum")
	}
	image = image.WithSpec(image.Spec().With("checksum", api.StringValue("  SHA256:"+strings.Repeat("A", 64)+"  ")))
	normalized, _ := Normalize(image, api.Catalog{})
	if normalized.Spec().Get("checksum").Text() != strings.Repeat("a", 64) {
		t.Fatal("checksum not normalized")
	}
	rescue := obj(api.Environment, "env", m("lifecycle", m("rescue", m("imageRef", "image"))))
	if issues := Validate(image.WithSpec(image.Spec().Without("checksum")), api.NewCatalog([]api.Object{rescue})); len(issues) == 0 {
		t.Fatal("rescue media pin omitted")
	}
}
