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
		object(api.ContainerCluster, "cluster", m("nodes", list(m("name", "master", "role", "master", "machineRef", "node")))),
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
		"installed host of a managed service":      {object(api.Machine, "services", m("os", m("provided", false, "installProfileRef", "rhel"))), "$.spec.access.ssh.user"},
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
