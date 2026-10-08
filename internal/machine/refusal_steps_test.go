package machine

import (
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func TestMachineRefusalsNameTheirStep(t *testing.T) {
	alone := func(o api.Object) (api.Object, api.Catalog) { return o, api.NewCatalog([]api.Object{o}) }
	for _, test := range []struct {
		name, code, field, remediation string
		setup                          func() (api.Object, api.Catalog)
	}{
		{"an OS-ready Machine with an install profile", "api.invariant", "$.spec.os.installProfileRef",
			"remove spec.os.installProfileRef, or set spec.os.provided to false for a Machine Bootwright installs",
			func() (api.Object, api.Catalog) {
				return alone(object(api.Machine, "probe", m("os", m("provided", true, "installProfileRef", "rhel"))))
			}},
		{"an OS-ready Machine with a network configuration", "api.invariant", "$.spec.network.configRef",
			"remove spec.network.configRef; an OS-ready Machine declares contacts only",
			func() (api.Object, api.Catalog) {
				return alone(object(api.Machine, "probe", m("os", m("provided", true), "network", m("configRef", "net"))))
			}},
		{"a non-provided Machine without a provider", "api.invariant", "$.spec.substrate.providerRef",
			"set spec.substrate.providerRef to the InfraProvider that realizes this Machine, or set spec.os.provided to true for a Machine whose OS is already installed",
			func() (api.Object, api.Catalog) {
				return alone(object(api.Machine, "probe", m("os", m("provided", false))))
			}},
		{"a host key outside a bare-metal installation", "api.invariant", "$.spec.os.install.hostKeyRef",
			"remove spec.os.install.hostKeyRef; only a bare-metal Bootwright-installed Machine delivers its own host key",
			func() (api.Object, api.Catalog) {
				machine, catalog := installedFixture()
				return machine.WithSpec(machine.Spec().With("os", m("provided", true, "install", m("hostKeyRef", "node-host-key")))), catalog
			}},
		{"a BMC without credentials on a bare-metal provider", "api.invariant", "$.spec.hardware.management.bmc.credentialsRef",
			"set spec.hardware.management.bmc.credentialsRef, or inherit it from the provider's spec.baremetal.defaults.bmc.credentialsRef",
			func() (api.Object, api.Catalog) {
				machine, catalog := installedFixture()
				spec := machine.Spec()
				bmc := spec.Get("hardware", "management", "bmc").Without("credentialsRef")
				return machine.WithSpec(spec.With("hardware", spec.Get("hardware").With("management", m("bmc", bmc)))), catalog
			}},
		{"native interfaces that are not a list", "api.type", "$.spec.nmstate.interfaces", "write interfaces as a list",
			func() (api.Object, api.Catalog) {
				return alone(object(api.NetworkConfig, "net", m("nmstate", m("interfaces", "eth0"))))
			}},
	} {
		t.Run(test.name, func(t *testing.T) {
			issues := Validate(test.setup())
			for _, issue := range issues {
				if issue.Field == test.field {
					if issue.Code != test.code || issue.Remediation != test.remediation {
						t.Fatalf("issue = %+v, want %s with %q", issue, test.code, test.remediation)
					}
					return
				}
			}
			t.Fatalf("no issue at %s: %+v", test.field, issues)
		})
	}
}

func TestAnacondaInstallAddressWithoutANetworkIsRefusedOnce(t *testing.T) {
	machine, catalog := fixture()
	machine = machine.WithSpec(machine.Spec().
		With("os", m("provided", false, "installProfileRef", "rhel", "install", m("rootDeviceHints", m("deviceName", "/dev/sda")))).
		With("network", m("installAddressRef", "primary", "addresses", list(
			m("name", "fqdn", "address", "node.example.test"),
			m("name", "primary", "address", "192.0.2.11/24")))))
	objects := []api.Object{machine, object(api.MachineInstallProfile, "rhel", m("installer", m("anaconda", m("imageRef", "image"))))}
	for _, existing := range catalog.Objects() {
		if existing.Kind() != api.Machine {
			objects = append(objects, existing)
		}
	}
	normalized, _ := Normalize(machine, api.NewCatalog(objects))
	objects[0] = normalized
	var found []api.Issue
	for _, issue := range Validate(normalized, api.NewCatalog(objects)) {
		if issue.Field == "$.spec.network.installAddressRef" {
			found = append(found, issue)
		}
	}
	const remediation = "select a network configuration with spec.network.configRef or declare one in spec.network.inline, then assign an IPv4 address with its prefix to the install interface in spec.network.addresses and select it with spec.network.installAddressRef"
	if len(found) != 1 || found[0].Code != "api.invariant" ||
		found[0].Message != "a Bootwright-installed Anaconda Machine installs with one static IPv4 address, and DHCP installation is not supported" ||
		found[0].Remediation != remediation {
		t.Fatalf("install address refusals = %+v, want the one static-install refusal", found)
	}
}
