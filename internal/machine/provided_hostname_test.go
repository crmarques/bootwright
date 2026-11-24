package machine

import (
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func TestProvidedHostnameSelectsARemotePinnedProvidedMachineWithAnFQDN(t *testing.T) {
	provided := m("os", m("provided", m()))
	contacts := m("addresses", list(m("name", "ssh", "address", "192.0.2.234"), m("name", "fqdn", "address", "witness.example.test")))
	access := m("ssh", m("addressRef", "ssh", "auth", m("privateKeyRef", "witness-admin"), "knownHostsRef", "witness-host-key"))
	remote := provided.WithPath(contacts, "network").WithPath(access, "access")
	noFQDN := provided.WithPath(m("addresses", list(m("name", "ssh", "address", "192.0.2.234"))), "network").WithPath(access, "access")
	for _, test := range []struct {
		name   string
		object api.Object
		want   string
	}{
		{"remote", object(api.Machine, "witness", remote), "witness.example.test"},
		{"controller", object(api.Machine, "controller", remote), ""},
		{"unpinned", object(api.Machine, "witness", provided.WithPath(contacts, "network")), ""},
		{"no fqdn", object(api.Machine, "witness", noFQDN), ""},
		{"installed", object(api.Machine, "witness", m("os", m("install", m()), "access", access).WithPath(contacts, "network")), ""},
	} {
		if got := ProvidedHostname(test.object, "controller"); got != test.want {
			t.Errorf("%s hostname = %q, want %q", test.name, got, test.want)
		}
	}
}
