package machine

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

// placementCatalog names "services" as a managed Proxy's host, "hypervisor" as
// a libvirt provider's host and "node" only as a cluster node.
func placementCatalog(machine api.Object) api.Catalog {
	return api.NewCatalog([]api.Object{
		object(api.Environment, "env", m("domains", m("base", "example.test"), "remoteMachinesAccessKey", m("keyRef", "fleet-key"))),
		object(api.Secret, "fleet-key", m("type", "sshKeyPair")),
		object(api.Proxy, "egress", m("management", "managed", "implementation", "squid", "machineRef", "services")),
		object(api.InfraProvider, "lab", m("libvirt", m("machineRef", "hypervisor", "uri", "qemu:///system"))),
		object(api.OpenShiftCluster, "cluster", m("nodes", list(m("name", "master", "role", "master", "machineRef", "node")))),
		machine,
	})
}

func admitPlacementHost(machine api.Object) []api.Issue {
	normalized, _ := Normalize(machine, placementCatalog(machine))
	return Validate(normalized, placementCatalog(normalized))
}

func providedHost(name string, ssh api.Value) api.Object {
	return object(api.Machine, name, m("os", m("provided", true), "access", m("ssh", ssh)))
}

func privilegeInvariants(issues []api.Issue) []string {
	var fields []string
	for _, issue := range issues {
		if issue.Code == "api.invariant" && (issue.Field == "$.spec.access.ssh.user" || issue.Field == "$.spec.access.ssh.sudoPasswordRef") {
			fields = append(fields, issue.Field)
		}
	}
	return fields
}

func TestALifecyclePlacementHostConnectsAsRoot(t *testing.T) {
	key := m("privateKeyRef", "services-key")
	for name, tc := range map[string]struct {
		machine api.Object
		field   string
	}{
		"managed service host as another account":  {providedHost("services", m("user", "operator", "auth", key)), "$.spec.access.ssh.user"},
		"libvirt provider host as another account": {providedHost("hypervisor", m("user", "operator", "auth", key)), "$.spec.access.ssh.user"},
		"operator identity as another account":     {providedHost("services", m("user", "operator", "auth", m("operatorIdentity", m()))), "$.spec.access.ssh.user"},
		"root host with an escalation Secret":      {providedHost("services", m("user", "root", "auth", key, "sudoPasswordRef", "services-sudo")), "$.spec.access.ssh.sudoPasswordRef"},
		"installed host of a libvirt provider":     {object(api.Machine, "hypervisor", m("os", m("provided", false, "installProfileRef", "rhel"))), "$.spec.access.ssh.user"},
	} {
		t.Run(name, func(t *testing.T) {
			if issues := admitPlacementHost(tc.machine); !slices.Equal(privilegeInvariants(issues), []string{tc.field}) {
				t.Fatalf("a placement host that cannot connect as root without escalation was admitted: %v", issues)
			}
		})
	}
	for name, machine := range map[string]api.Object{
		"cluster node as another account":     providedHost("node", m("user", "operator", "auth", key)),
		"operator identity with no user":      providedHost("services", m("auth", m("operatorIdentity", m()))),
		"root host without escalation Secret": providedHost("services", m("user", "root", "auth", key, "knownHostsRef", "services-host-key")),
	} {
		t.Run(name, func(t *testing.T) {
			if issues := admitPlacementHost(machine); len(issues) != 0 {
				t.Fatalf("issues = %v", issues)
			}
		})
	}
	if issues := admitPlacementHost(object(api.Machine, "services", m("os", m("provided", false, "installProfileRef", "rhel")))); len(privilegeInvariants(issues)) != 0 {
		t.Fatalf("an installed host of a managed service, whose services escalate as its installation configured, was refused its account: %v", issues)
	}
	host := func(ssh api.Value) api.Object {
		return object(api.Machine, "services", m("os", m("provided", true), "network", m("addresses", list(m("name", "ip", "address", "192.0.2.2"))), "access", m("ssh", ssh)))
	}
	root := m("addressRef", "ip", "auth", key, "knownHostsRef", "services-host-key")
	for _, ssh := range []api.Value{root, root.With("user", api.StringValue("root"))} {
		if placement, err := PlacementFor(host(ssh), "controller"); err != nil || placement.User != "root" {
			t.Fatalf("placement = %+v (%v)", placement, err)
		}
	}
	for name, ssh := range map[string]api.Value{
		"another account":    root.With("user", api.StringValue("operator")),
		"an escalation":      root.With("sudoPasswordRef", api.StringValue("services-sudo")),
		"root and escalates": root.With("user", api.StringValue("root")).With("sudoPasswordRef", api.StringValue("services-sudo")),
	} {
		t.Run("placement refuses "+name, func(t *testing.T) {
			_, err := PlacementFor(host(ssh), "controller")
			if reported := diagnostics.Of(err); err == nil || len(reported) != 1 || reported[0].Code != "lifecycle.state" {
				t.Fatalf("placement = %v", reported)
			}
		})
	}
}

// A placement binds its SSH identity and host key and names no escalation
// Secret, so one frozen by a build that still recorded it does not read as a
// placement, and the request carrying it refuses rather than decoding a field
// nothing uses.
func TestAPlacementNeverBindsAnEscalationSecret(t *testing.T) {
	decoder := json.NewDecoder(strings.NewReader(`{"address":"192.0.2.2","connection":"ssh","knownHostsRef":"services-host-key",` +
		`"machine":"services","port":22,"privateKeyRef":"services-key","sudoPasswordRef":"services-sudo","user":"root"}`))
	decoder.DisallowUnknownFields()
	var escalating Placement
	if err := decoder.Decode(&escalating); err == nil {
		t.Fatalf("a placement naming an escalation Secret was read: %+v", escalating)
	}
	frozen := Placement{
		Address: "192.0.2.2", Connection: ConnectionSSH, KnownHostsRef: "services-host-key", Machine: "services",
		Port: 22, PrivateKeyRef: "services-key", User: "root",
	}
	if got := frozen.SecretReferences(); !slices.Equal(got, []string{"services-key", "services-host-key"}) {
		t.Fatalf("secret references = %v", got)
	}
}

// A service on a Machine Bootwright installs takes the installed arm: its
// install address on port 22 as the account and fleet key its effective access
// names, with no host-key Secret, while an OS-ready Machine and the controller
// keep PlacementFor's arms and a consumer's PlacementFor still refuses it.
func TestAServiceOnAnInstalledMachineTakesTheInstalledArm(t *testing.T) {
	addresses := list(m("name", "fqdn", "address", "mirror.example.test"), m("name", "ip", "address", "198.51.100.20"))
	installed := func(ssh api.Value) api.Object {
		spec := m("os", m("provided", false, "installProfileRef", "rhel"), "network", m("installAddressRef", "ip", "addresses", addresses))
		if ssh.Present() {
			spec = spec.With("access", m("ssh", ssh))
		}
		return object(api.Machine, "mirror", spec)
	}
	effective := m("addressRef", "ip", "port", api.IntegerValue("22"), "user", "bootwright", "auth", m("privateKeyRef", "fleet-key"))
	placement, err := ServicePlacementFor(installed(effective), "controller")
	want := Placement{Address: "198.51.100.20", Connection: ConnectionInstalled, Machine: "mirror", Port: 22, PrivateKeyRef: "fleet-key", User: "bootwright"}
	if err != nil || placement != want || !placement.Installed() || len(placement.SecretReferences()) != 1 {
		t.Fatalf("placement = %+v (%v)", placement, err)
	}
	for name, ssh := range map[string]api.Value{
		"another address": effective.With("addressRef", api.StringValue("fqdn")),
		"another port":    effective.With("port", api.IntegerValue("2222")),
		"no key":          effective.With("auth", m("operatorIdentity", m())),
		"no access":       {},
	} {
		if _, err := ServicePlacementFor(installed(ssh), "controller"); err == nil {
			t.Fatalf("a service on an installed Machine reached by %s was placed", name)
		}
	}
	if _, err := PlacementFor(installed(effective), "controller"); err == nil {
		t.Fatal("a consumer's placement reached a Machine Bootwright installs")
	}
	if placement, err := ServicePlacementFor(installed(effective), "mirror"); err != nil || !placement.Local() {
		t.Fatalf("the controller Machine was placed %+v (%v)", placement, err)
	}
	provided := providedHost("services", m("addressRef", "ip", "auth", m("privateKeyRef", "services-key"), "knownHostsRef", "services-host-key"))
	provided = provided.WithSpec(provided.Spec().With("network", m("addresses", addresses)))
	if placement, err := ServicePlacementFor(provided, "controller"); err != nil || placement.Connection != ConnectionSSH {
		t.Fatalf("an OS-ready Machine was placed %+v (%v)", placement, err)
	}
}
