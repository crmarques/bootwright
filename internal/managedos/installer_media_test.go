package managedos

import (
	"reflect"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func TestInstallerMediaHostsListTheServersOfInstalledAnacondaMachines(t *testing.T) {
	profile := func(name, server string) api.Object {
		return obj(api.MachineInstallProfile, name, m("installer", m("anaconda", m("redfishVirtualMedia", m("artifactServerEndpoint", m("serverRef", server))))))
	}
	machine := func(name, profileRef string, provided bool) api.Object {
		return obj(api.Machine, name, m("os", osOf(provided, profileRef)))
	}
	catalog := api.NewCatalog([]api.Object{
		obj(api.ArtifactServer, "near", m("machineRef", "zeta")),
		obj(api.ArtifactServer, "far", m("machineRef", "alpha")),
		profile("a", "near"),
		profile("b", "far"),
		profile("same", "far"),
		profile("missing-server", "gone"),
		obj(api.MachineInstallProfile, "clone", m("installer", m("templateClone", m()))),
		machine("one", "a", false),
		machine("two", "b", false),
		machine("three", "same", false),
		machine("four", "missing-server", false),
		machine("five", "clone", false),
		machine("six", "absent-profile", false),
		machine("seven", "a", true),
	})
	if got, want := InstallerMediaHosts(catalog), []string{"alpha", "zeta"}; !reflect.DeepEqual(got, want) {
		t.Errorf("hosts = %v, want %v", got, want)
	}
	if got := InstallerMediaHosts(api.NewCatalog(nil)); len(got) != 0 {
		t.Errorf("an empty catalog names hosts %v", got)
	}
}

func TestInstallerMediaHostsIncludeOnlyTheOptedInAgentClusters(t *testing.T) {
	agent := func(name, node, server string) api.Object {
		return obj(api.OpenShiftCluster, name, m(
			"install", m("agent", m("redfishVirtualMedia", m("artifactServerEndpoint", m("serverRef", server)))),
			"nodes", api.ListValue(m("machineRef", node)),
		))
	}
	optedIn := obj(api.Machine, "metal", m("os", m("clusterInstalled", m()),
		"hardware", m("rootDisk", m("linuxPreflight", m("imageRef", "rhel")))))
	defaultProof := obj(api.Machine, "default", m("os", m("clusterInstalled", m())))
	catalog := api.NewCatalog([]api.Object{
		obj(api.ArtifactServer, "inventory", m("machineRef", "controller")),
		obj(api.ArtifactServer, "other", m("machineRef", "other-host")),
		optedIn, defaultProof, agent("opaque", "metal", "inventory"), agent("ordinary", "default", "other"),
	})
	if got, want := InstallerMediaHosts(catalog), []string{"controller"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("installer media hosts = %v, want %v", got, want)
	}
}

func TestEffectBearingNamesAMachineWhoseOperatingSystemIsNotProvided(t *testing.T) {
	for name, test := range map[string]struct {
		object  api.Object
		bearing bool
	}{
		"installed":           {obj(api.Machine, "n", m("os", m("install", m("profileRef", "install")))), true},
		"cluster installed":   {obj(api.Machine, "n", m("os", m("clusterInstalled", m()))), true},
		"provided":            {obj(api.Machine, "n", m("os", m("provided", m()))), false},
		"provided undeclared": {obj(api.Machine, "n", m("os", m())), false},
		"provided update":     {obj(api.Machine, "n", m("os", m("provided", m("updates", m("policy", "withinRelease", "release", "9.8"))))), true},
		"provided registered": {obj(api.Machine, "n", m("os", m("provided", m("subscription", m("entitlementRef", "satellite"))))), true},
		"another kind":        {obj(api.Proxy, "n", m("os", m("clusterInstalled", m()))), false},
	} {
		reason, bearing := EffectBearing(test.object)
		if bearing != test.bearing || bearing != (reason != "") {
			t.Errorf("%s: EffectBearing = %q, %t", name, reason, bearing)
		}
	}
	if reason, _ := EffectBearing(obj(api.Machine, "n", m("os", m("clusterInstalled", m())))); !strings.Contains(reason, "operating system is not provided") {
		t.Errorf("reason = %q", reason)
	}
}

func TestMediaOriginAdmitsOnlyBoundedCleanText(t *testing.T) {
	for name, test := range map[string]struct {
		origin string
		valid  bool
	}{
		"a URL":            {"https://mirror.example.test/rhel.iso", true},
		"empty":            {"", false},
		"leading space":    {" https://x", false},
		"trailing space":   {"https://x ", false},
		"a control byte":   {"https://x\x01", false},
		"a newline":        {"a\nb", false},
		"a delete byte":    {"a\x7fb", false},
		"invalid UTF-8":    {"a\xffb", false},
		"at the bound":     {strings.Repeat("a", MaxMediaOrigin), true},
		"beyond the bound": {strings.Repeat("a", MaxMediaOrigin+1), false},
	} {
		if got := ValidMediaOrigin(test.origin); got != test.valid {
			t.Errorf("%s: ValidMediaOrigin = %t, want %t", name, got, test.valid)
		}
	}
}

func TestMediaSizeTextIsTheBareDecimalSize(t *testing.T) {
	for size, want := range map[int64]string{0: "0", 1: "1", 1 << 40: "1099511627776"} {
		if got := MediaSizeText(size); got != want {
			t.Errorf("MediaSizeText(%d) = %q, want %q", size, got, want)
		}
	}
}

func TestAMediaRecordWithAnInvalidOriginIsNotEncoded(t *testing.T) {
	entry := MediaEntry{Name: "rhel.iso", Size: 1, SHA256: strings.Repeat("a", 64), Source: "https://x/rhel.iso", Added: "2026-01-02T03:04:05Z"}
	if _, err := EncodeMediaRecord(entry); err != nil {
		t.Fatalf("a valid record = %v", err)
	}
	entry.Source = " padded"
	if _, err := EncodeMediaRecord(entry); err == nil {
		t.Error("a record with a padded origin was encoded")
	}
}

func TestValidateAuthoredChecksOnlyRHELEntitlementsAndInstallProfiles(t *testing.T) {
	if issues := ValidateAuthored(obj(api.Machine, "n", m()), api.NewCatalog(nil)); issues != nil {
		t.Errorf("a Machine = %v", issues)
	}
	if issues := ValidateAuthored(obj(api.Entitlement, "e", m("type", "other")), api.NewCatalog(nil)); issues != nil {
		t.Errorf("another entitlement type = %v", issues)
	}
	rhel := obj(api.Entitlement, "e", m("type", "redhatRhel"))
	if got, want := ValidateAuthored(rhel, api.NewCatalog(nil)), ValidateRHSM(rhel, true); !reflect.DeepEqual(got, want) {
		t.Errorf("a RHEL entitlement = %v, want the RHSM validation %v", got, want)
	}
}
