package inventory

import (
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
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
		"network", m("addresses", list(
			m("name", "ssh", "address", "192.0.2.20/24"),
			m("name", "fqdn", "address", "guest.lab.example.test"),
			m("name", "storage", "address", "198.51.100.20/24"))),
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
	// The contact is one declaration; the IPs are every one this Machine
	// declares, so a DNS name never reads as an address and a second network
	// is never lost behind the one SSH happens to use.
	if want := []string{"192.0.2.20", "198.51.100.20"}; !slices.Equal(rows[0].IPs, want) {
		t.Fatalf("declared IPs = %v, want %v", rows[0].IPs, want)
	}
	if rows[2].Address != "" {
		t.Fatalf("a Machine with no SSH access reported a contact: %q", rows[2].Address)
	}
	if !slices.Equal(rows[2].IPs, []string{"192.0.2.30"}) {
		t.Fatalf("a Machine reached by no contact still declares its IP: %v", rows[2].IPs)
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
		{"resolves to none", []string{}, ""},
		{"empty members", []string{"", " "}, ""},
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

// A lifecycle position is what an operation proved. A Machine no frozen plan
// names has had nothing applied to it, and a verb that has not completed names
// itself in progress rather than the position it was heading for: a Machine
// halfway through a removal still owns what is not yet removed, so it must not
// read as one nothing has touched or as one that is fully applied.
func TestLifecycleFollowsTheEvidenceRatherThanTheDeclaration(t *testing.T) {
	for _, test := range []struct {
		name     string
		evidence machine.OwnershipState
		want     string
	}{
		{"no evidence", machine.OwnershipState{}, LifecycleNotApplied},
		{"applied", machine.OwnershipState{Verb: "apply", State: "done"}, LifecycleApplied},
		{"apply pending", machine.OwnershipState{Verb: "apply", State: "pending"}, LifecycleApplying},
		{"apply running", machine.OwnershipState{Verb: "apply", State: "running"}, LifecycleApplying},
		{"failed", machine.OwnershipState{Verb: "apply", State: "failed"}, LifecycleFailed},
		{"unproved", machine.OwnershipState{Verb: "apply", State: "unknown"}, LifecycleUnknown},
		{"removed", machine.OwnershipState{Verb: "destroy", State: "done"}, LifecycleDestroyed},
		{"removal pending", machine.OwnershipState{Verb: "destroy", State: "pending"}, LifecycleDestroying},
		{"removal running", machine.OwnershipState{Verb: "destroy", State: "running"}, LifecycleDestroying},
		{"removal unproved", machine.OwnershipState{Verb: "destroy", State: "unknown"}, LifecycleUnknown},
	} {
		t.Run(test.name, func(t *testing.T) {
			owned := map[string]machine.OwnershipState{"Machine/guest": test.evidence}
			rows, err := Rows(catalog(), nil, owned)
			if err != nil {
				t.Fatal(err)
			}
			if rows[0].Lifecycle != test.want {
				t.Fatalf("lifecycle = %q, want %q", rows[0].Lifecycle, test.want)
			}
			if rows[1].Lifecycle != LifecycleNotApplied {
				t.Fatalf("a Machine outside the evidence reported %q", rows[1].Lifecycle)
			}
			if rows[0].Power != "" || rows[1].Power != "" {
				t.Fatal("a row derived from local state alone carried a power reading")
			}
		})
	}
}

// A member that names no cluster this context selects is a mistake, not a
// cluster with no Machines, so it refuses naming every unknown member and the
// clusters an operator can select instead.
func TestAnUnknownClusterRefusesNamingTheSelectableClusters(t *testing.T) {
	withStorage := api.NewCatalog(append(catalog().Objects(),
		object(api.StorageCluster, "ceph", m("ceph", m("topology", m("nodes", list(m("name", "host", "machineRef", "host"))))))))
	noClusters := api.NewCatalog(slices.DeleteFunc(catalog().Objects(), func(o api.Object) bool { return o.Kind() == api.ContainerCluster }))
	for _, test := range []struct {
		name        string
		catalog     api.Catalog
		clusters    []string
		message     string
		remediation string
	}{
		{"unknown", withStorage, []string{"absent"}, "the selected context declares no ContainerCluster or StorageCluster named absent",
			"select one of ceph, ocp with --clusters, or omit --clusters"},
		{"one of several", withStorage, []string{"ocp", "absent"}, "the selected context declares no ContainerCluster or StorageCluster named absent",
			"select one of ceph, ocp with --clusters, or omit --clusters"},
		{"several unknown", catalog(), []string{"zeta", " absent ", "absent", "ocp"},
			"the selected context declares no ContainerCluster or StorageCluster named absent, zeta",
			"select one of ocp with --clusters, or omit --clusters"},
		{"no cluster to select", noClusters, []string{"ocp"}, "the selected context declares no ContainerCluster or StorageCluster named ocp",
			"the selected context declares no cluster; omit --clusters"},
	} {
		t.Run(test.name, func(t *testing.T) {
			rows, err := Rows(test.catalog, test.clusters, nil)
			reported := diagnostics.Of(err)
			if rows != nil || len(reported) != 1 || reported[0].Code != "access.target" ||
				reported[0].Message != test.message || reported[0].Remediation != test.remediation {
				t.Fatalf("rows = %+v, refusal = %+v", rows, reported)
			}
		})
	}
	for _, clusters := range [][]string{{"ceph"}, {"ocp", "ceph"}} {
		if _, err := Rows(withStorage, clusters, nil); err != nil {
			t.Fatalf("%v: %v", clusters, err)
		}
	}
	if rows, err := Rows(noClusters, []string{}, nil); err != nil || len(rows) != 0 {
		t.Fatalf("an empty selection = %+v (%v)", rows, err)
	}
}
