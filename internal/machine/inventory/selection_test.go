package inventory

import (
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/machine"
)

func object(kind api.Kind, name string, spec api.Value) api.Object {
	return api.NewObject(kind, name, api.MapValue(), spec)
}

func m(kv ...any) api.Value {
	fields := []api.FieldValue{}
	for i := 0; i < len(kv); i += 2 {
		var value api.Value
		switch typed := kv[i+1].(type) {
		case api.Value:
			value = typed
		case string:
			value = api.StringValue(typed)
		case bool:
			value = api.BoolValue(typed)
		}
		fields = append(fields, api.FieldValue{Name: kv[i].(string), Value: value})
	}
	return api.MapValue(fields...)
}

func list(values ...api.Value) api.Value { return api.ListValue(values...) }

func addresses(value string) api.Value {
	return m("addresses", list(m("name", "ssh", "address", value)))
}

func catalog() api.Catalog {
	guest := object(api.Machine, "guest", m(
		"substrate", m("providerRef", "lab"),
		"os", m("provided", false, "installProfileRef", "rhel"),
		"network", addresses("192.0.2.20/24"),
		"access", m("ssh", m("addressRef", "ssh", "user", "bootwright", "auth", m("privateKeyRef", "fleet")))))
	host := object(api.Machine, "host", m(
		"os", m("provided", true),
		"network", addresses("192.0.2.10/24"),
		"access", m("ssh", m("addressRef", "ssh", "user", "root", "auth", m("privateKeyRef", "host-key")))))
	node := object(api.Machine, "node", m(
		"os", m("provided", true),
		"network", addresses("192.0.2.30/24")))
	cluster := object(api.ContainerCluster, "ocp", m("nodes", list(m("name", "master-0", "machineRef", "node"))))
	return api.NewCatalog([]api.Object{guest, host, node, cluster})
}

func TestRowsReportEveryMachineInCanonicalOrderWithItsDeclaredContact(t *testing.T) {
	rows, err := Rows(catalog(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 || rows[0].Name != "guest" || rows[1].Name != "host" || rows[2].Name != "node" {
		t.Fatalf("rows = %+v", rows)
	}
	if rows[0].OS != "installed" || rows[1].OS != "provided" {
		t.Fatalf("operating system arms = %q, %q", rows[0].OS, rows[1].OS)
	}
	if rows[0].Address != "192.0.2.20" || rows[0].Provider != "lab" {
		t.Fatalf("contact = %+v", rows[0])
	}
	if rows[2].Address != "" {
		t.Fatalf("a Machine with no SSH access reported a contact: %q", rows[2].Address)
	}
	if len(rows[2].Clusters) != 1 || rows[2].Clusters[0] != "ocp" {
		t.Fatalf("membership = %+v", rows[2].Clusters)
	}
	if names := Names(rows); strings.Join(names, ",") != "guest,host,node" {
		t.Fatalf("names = %+v", names)
	}
}

// A selector filters presentation alone. An omitted value selects everything,
// and a value that resolves to no member selects nothing rather than silently
// selecting all.
func TestClusterSelectionFiltersPresentationOnly(t *testing.T) {
	for _, test := range []struct {
		name     string
		clusters []string
		want     string
	}{
		{"omitted", nil, "guest,host,node"},
		{"named", []string{"ocp"}, "node"},
		{"unknown", []string{"absent"}, ""},
		{"resolves to none", []string{}, ""},
		{"whitespace members", []string{" ocp "}, "node"},
	} {
		t.Run(test.name, func(t *testing.T) {
			rows, err := Rows(catalog(), test.clusters, nil)
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.Join(Names(rows), ","); got != test.want {
				t.Fatalf("selection = %q, want %q", got, test.want)
			}
		})
	}
}

// Ownership is what an operation proved. A Machine no frozen plan names is
// unmanaged, and a removal owns what it has not finished removing.
func TestStateFollowsTheEvidenceRatherThanTheDeclaration(t *testing.T) {
	for _, test := range []struct {
		name     string
		evidence machine.OwnershipState
		want     string
	}{
		{"no evidence", machine.OwnershipState{}, StateUnmanaged},
		{"applied", machine.OwnershipState{Verb: "apply", State: "done"}, StateOwned},
		{"applying", machine.OwnershipState{Verb: "apply", State: "pending"}, StatePending},
		{"failed", machine.OwnershipState{Verb: "apply", State: "failed"}, StateFailed},
		{"unproved", machine.OwnershipState{Verb: "apply", State: "unknown"}, StateUnknown},
		{"interrupted", machine.OwnershipState{Verb: "apply", State: "running"}, StateUnknown},
		{"removed", machine.OwnershipState{Verb: "destroy", State: "done"}, StateReleased},
		{"removal pending", machine.OwnershipState{Verb: "destroy", State: "pending"}, StateOwned},
	} {
		t.Run(test.name, func(t *testing.T) {
			owned := map[string]machine.OwnershipState{"Machine/guest": test.evidence}
			rows, err := Rows(catalog(), nil, owned)
			if err != nil {
				t.Fatal(err)
			}
			if rows[0].State != test.want {
				t.Fatalf("state = %q, want %q", rows[0].State, test.want)
			}
			if rows[1].State != StateUnmanaged {
				t.Fatalf("a Machine outside the evidence reported %q", rows[1].State)
			}
		})
	}
}
