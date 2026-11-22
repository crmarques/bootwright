package machine

import (
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func TestProvidedUpdateRequiresARemotePinnedPlacement(t *testing.T) {
	environment := object(api.Environment, "example", m("controller", m("machineRef", "controller")))
	provided := m("os", m("provided", m("updates", m("policy", "withinRelease", "release", "9.8"))))
	contacts := m("addresses", list(m("name", "ssh", "address", "192.0.2.234")))
	access := m("ssh", m("addressRef", "ssh", "auth", m("operatorIdentity", m())),
		"lifecycle", m("user", "maintenance", "privilege", "sudo", "privateKeyRef", "update-admin", "knownHostsRef", "host-key"))
	remote := object(api.Machine, "remote", provided.WithPath(contacts, "network").WithPath(access, "access"))
	catalog := api.NewCatalog([]api.Object{environment, remote})
	if got := validateProvidedUpdate(remote, catalog); len(got) != 0 {
		t.Fatal(got)
	}
	hosts, err := HardwareHosts(catalog, "controller")
	if err != nil || len(hosts) != 1 || !hosts[0].PinnedSSH() || !hosts[0].Sudo() {
		t.Fatalf("hardware claims = %+v, %v", hosts, err)
	}
	for _, test := range []struct {
		machine api.Object
		message string
	}{
		{object(api.Machine, "controller", provided), "cannot reboot the controller"},
		{object(api.Machine, "unpinned", provided.WithPath(contacts, "network")), "require pinned lifecycle SSH access"},
	} {
		issues := validateProvidedUpdate(test.machine, catalog)
		if len(issues) != 1 || !strings.Contains(issues[0].Message, test.message) {
			t.Errorf("%s issues = %+v, want %q", test.machine.Name(), issues, test.message)
		}
	}
}
