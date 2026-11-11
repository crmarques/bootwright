package machine

import (
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func TestHostFirewallPolicyPreservesExternalOwnershipAndRequiresPinnedRemoteAccess(t *testing.T) {
	for _, management := range []string{"", "external", "managed"} {
		host := object(api.Machine, "controller", m("os", m("provided", true)))
		if management != "" {
			host = host.WithSpec(host.Spec().WithPath(api.StringValue(management), "firewall", "management"))
		}
		environment := object(api.Environment, "env", m("controller", m("machineRef", "controller")))
		catalog := api.NewCatalog([]api.Object{host, environment})
		if got := ManagesFirewall(catalog, host.Name()); got != (management == "managed") {
			t.Fatalf("%s: managed=%v", management, got)
		}
		if issues := validateFirewall(host, catalog); len(issues) != 0 {
			t.Fatal(issues)
		}
	}
	remote := providedHost("remote", m("user", "root", "auth", m("operatorIdentity", m())))
	remote = remote.WithSpec(remote.Spec().WithPath(api.StringValue("managed"), "firewall", "management"))
	catalog := api.NewCatalog([]api.Object{remote})
	if issues := validateFirewall(remote, catalog); len(issues) != 1 {
		t.Fatal("unbound remote access accepted", issues)
	}
	remote = remote.WithSpec(remote.Spec().WithPath(api.StringValue("host-key"), "access", "ssh", "knownHostsRef").WithPath(api.StringValue("host-id"), "access", "ssh", "auth", "privateKeyRef"))
	if issues := validateFirewall(remote, api.NewCatalog([]api.Object{remote})); len(issues) != 0 {
		t.Fatal(issues)
	}
}
