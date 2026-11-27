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
		{"an OS-ready Machine with a network configuration", "api.invariant", "$.spec.network.configRef",
			"remove spec.network.configRef; an OS-ready Machine declares contacts only",
			func() (api.Object, api.Catalog) {
				return alone(object(api.Machine, "probe", m("os", m("provided", m()), "network", m("configRef", "net"))))
			}},
		{"a Bootwright-installed Machine without a provider", "api.invariant", "$.spec.hardware.providerRef",
			"set spec.hardware.providerRef to the InfraProvider that realizes this Machine, or declare os.provided: {} for a Machine whose OS is already installed",
			func() (api.Object, api.Catalog) {
				return alone(object(api.Machine, "probe", m("os", m("install", m("profileRef", "rhel")))))
			}},
		{"a cluster-installed Machine without a provider", "api.invariant", "$.spec.hardware.providerRef",
			"set spec.hardware.providerRef to the InfraProvider that realizes this Machine, or declare os.provided: {} for a Machine whose OS is already installed",
			func() (api.Object, api.Catalog) {
				return alone(object(api.Machine, "probe", m("os", m("clusterInstalled", m()))))
			}},
		{"a host key outside a libvirt or bare-metal installation", "api.invariant", "$.spec.os.install.hostKeyRef",
			"remove spec.os.install.hostKeyRef or select an installation method that delivers the key",
			func() (api.Object, api.Catalog) {
				provider := object(api.InfraProvider, "kv", m("kubevirt", m("kubeconfigRef", "host", "namespace", "guests")))
				machine := object(api.Machine, "probe", m(
					"hardware", m("providerRef", "kv", "profileRef", "small"),
					"os", m("install", m("profileRef", "rhel", "hostKeyRef", "node-host-key"))))
				return machine, api.NewCatalog([]api.Object{machine, provider})
			}},
		{"a BMC without credentials on a bare-metal provider", "api.invariant", "$.spec.hardware.bmc.credentialsRef",
			"set spec.hardware.bmc.credentialsRef, or inherit it from the provider's spec.baremetal.defaults.bmc.credentialsRef",
			func() (api.Object, api.Catalog) {
				machine, catalog := installedFixture()
				spec := machine.Spec()
				bmc := spec.Get("hardware", "bmc").Without("credentialsRef")
				return machine.WithSpec(spec.WithPath(bmc, "hardware", "bmc")), catalog
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
		With("os", m("install", m("profileRef", "rhel"))).
		With("network", m("installAddressRef", "primary", "addresses", list(
			m("name", "fqdn", "address", "node.example.test"),
			m("name", "primary", "address", "192.0.2.11")))))
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
	const remediation = "select a network configuration with spec.network.configRef or declare one in spec.network.inline, then assign an IPv4 address to the install interface in spec.network.addresses and select it with spec.network.installAddressRef"
	if len(found) != 1 || found[0].Code != "api.invariant" ||
		found[0].Message != "a Bootwright-installed Anaconda Machine installs with one static IPv4 address, and DHCP installation is not supported" ||
		found[0].Remediation != remediation {
		t.Fatalf("install address refusals = %+v, want the one static-install refusal", found)
	}
}
