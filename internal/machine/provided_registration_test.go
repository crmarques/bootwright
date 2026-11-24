package machine

import (
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func TestProvidedRegistrationRequiresARemotePinnedPlacement(t *testing.T) {
	environment := object(api.Environment, "example", m("controller", m("machineRef", "controller")))
	provided := m("os", m("provided", m("subscription", m("entitlementRef", "satellite"))))
	contacts := m("addresses", list(m("name", "ssh", "address", "192.0.2.234")))
	access := m("ssh", m("addressRef", "ssh", "auth", m("operatorIdentity", m())),
		"lifecycle", m("user", "maintenance", "privilege", "sudo", "privateKeyRef", "update-admin", "knownHostsRef", "host-key"))
	remote := object(api.Machine, "remote", provided.WithPath(contacts, "network").WithPath(access, "access"))
	catalog := api.NewCatalog([]api.Object{environment, remote})
	if got := validateProvidedRegistration(remote, catalog); len(got) != 0 {
		t.Fatal(got)
	}
	if ProvidedSubscription(remote) != "satellite" || ProvidedSubscription(object(api.Machine, "plain", m("os", m("provided", m())))) != "" {
		t.Fatal("the registration's Entitlement was not read from the Machine")
	}
	hosts, err := HardwareHosts(catalog, "controller")
	if err != nil || len(hosts) != 1 || !hosts[0].PinnedSSH() {
		t.Fatalf("hardware claims = %+v, %v", hosts, err)
	}
	for _, test := range []struct {
		machine api.Object
		message string
	}{
		{object(api.Machine, "controller", provided), "the controller keeps its own registration"},
		{object(api.Machine, "unpinned", provided.WithPath(contacts, "network")), "requires pinned lifecycle SSH access"},
	} {
		issues := validateProvidedRegistration(test.machine, catalog)
		if len(issues) != 1 || issues[0].Field != "$.spec.os.provided.subscription" || !strings.Contains(issues[0].Message, test.message) {
			t.Errorf("%s issues = %+v, want %q", test.machine.Name(), issues, test.message)
		}
	}
}
