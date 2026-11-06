package ceph

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func withClusterCIDRs(cidrs ...string) func(api.Value) api.Value {
	return func(spec api.Value) api.Value {
		return spec.WithPath(api.StringList(cidrs...), "networks", "clusterCIDRs")
	}
}

func withReplicationAddresses(catalog api.Catalog, addresses map[int]string) api.Catalog {
	objects := catalog.Objects()
	for i := range objects {
		for index, address := range addresses {
			if objects[i].Kind() != api.Machine || objects[i].Name() != fmt.Sprintf("node-%d", index) {
				continue
			}
			list := api.ListValue(requestMap("name", "ip", "address", fmt.Sprintf("192.0.2.%d", index)), requestMap("name", "replication", "address", address))
			objects[i] = objects[i].WithSpec(objects[i].Spec().WithPath(list, "network", "addresses"))
		}
	}
	return api.NewCatalog(objects)
}

func replicationAddresses(format string) map[int]string {
	return map[int]string{1: fmt.Sprintf(format, 1), 2: fmt.Sprintf(format, 2), 3: fmt.Sprintf(format, 3)}
}

func clusterOf(t *testing.T, catalog api.Catalog) api.Object {
	t.Helper()
	cluster, found := catalog.Find(api.CephCluster, "example")
	if !found {
		t.Fatal("the catalog holds no CephCluster example")
	}
	return cluster
}

func clusterNetworkRefusals(catalog api.Catalog) map[api.Kind][]lifecycle.Refusal {
	state := compilation.NewState(catalog, catalog, nil)
	out := map[api.Kind][]lifecycle.Refusal{}
	for _, kind := range []api.Kind{api.CephCluster, api.CephPool, api.CephClient} {
		out[kind] = New(kind, nil).Unsupported(state)
	}
	return out
}

// Admission owns the replication coverage rule (cephcluster, network
// coverage); the plan freezes what it admitted.
func TestAdmittedClusterNetworksFreezeIntoTheRequest(t *testing.T) {
	for _, test := range []struct {
		name      string
		cidrs     []string
		addresses map[int]string
	}{
		{name: "every host has one", cidrs: []string{"198.51.100.0/24"}, addresses: replicationAddresses("198.51.100.%d/24")},
		{name: "an address in the second network counts", cidrs: []string{"198.51.100.0/24", "203.0.113.0/24"}, addresses: replicationAddresses("203.0.113.%d")},
		{name: "IPv6 inside IPv6", cidrs: []string{"2001:db8:100::/64"}, addresses: replicationAddresses("2001:db8:100::%d/64")},
	} {
		t.Run(test.name, func(t *testing.T) {
			catalog := withReplicationAddresses(pinnedCatalog(t, withClusterCIDRs(test.cidrs...)), test.addresses)
			r, err := baseRequest(catalog, clusterOf(t, catalog), "synthetic")
			if err != nil {
				t.Fatal(diagnostics.Of(err))
			}
			if !reflect.DeepEqual(r.ClusterNetworks, test.cidrs) || len(r.Hosts) != 3 {
				t.Fatalf("accepted request froze %v and %d hosts", r.ClusterNetworks, len(r.Hosts))
			}
			for kind, got := range clusterNetworkRefusals(catalog) {
				if got != nil {
					t.Fatalf("%s refused %+v", kind, got)
				}
			}
		})
	}
}

func TestNoClusterNetworksKeepsThePublicOnlyPlan(t *testing.T) {
	empty := func(spec api.Value) api.Value {
		return spec.WithPath(api.ListValue(), "networks", "clusterCIDRs")
	}
	for name, change := range map[string]func(api.Value) api.Value{"absent": nil, "empty": empty} {
		t.Run(name, func(t *testing.T) {
			catalog := pinnedCatalog(t, change)
			for kind, got := range clusterNetworkRefusals(catalog) {
				if got != nil {
					t.Fatalf("%s refused %+v", kind, got)
				}
			}
			input := lifecycle.PlanInput{Verb: reconciliation.Apply, Context: lifecycle.ContextIdentity{Name: "synthetic"}, State: compilation.NewState(catalog, catalog, nil), Controller: "controller"}
			plan, err := New(api.CephCluster, nil).Plan(context.Background(), input)
			if err != nil {
				t.Fatal(diagnostics.Of(err))
			}
			if len(plan.Definitions) != 2 || plan.Definitions[1].ID != "ceph-service-example-crash" {
				t.Fatalf("planned %d definitions", len(plan.Definitions))
			}
			for _, definition := range plan.Definitions {
				var r Request
				if err := json.Unmarshal(definition.Request, &r); err != nil || len(r.ClusterNetworks) != 0 {
					t.Fatalf("public-only plan froze %v (%v)", r.ClusterNetworks, err)
				}
			}
		})
	}
}
