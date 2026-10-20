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

const clusterNetworksRemedy = ", or remove spec.ceph.networks.clusterCIDRs"

func withClusterCIDRs(cidrs ...string) func(api.Value) api.Value {
	return func(spec api.Value) api.Value {
		return spec.WithPath(api.StringList(cidrs...), "ceph", "networks", "clusterCIDRs")
	}
}

func withNodeFQDN(index int, fqdn string, change func(api.Value) api.Value) func(api.Value) api.Value {
	return func(spec api.Value) api.Value {
		spec = change(spec)
		nodes := spec.Get("ceph", "topology", "nodes").Items()
		nodes[index-1] = nodes[index-1].WithPath(api.StringValue(fqdn), "fqdn")
		return spec.WithPath(api.ListValue(nodes...), "ceph", "topology", "nodes")
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
	cluster, found := catalog.Find(api.StorageCluster, "example")
	if !found {
		t.Fatal("the catalog holds no StorageCluster example")
	}
	return cluster
}

func assertInvariant(t *testing.T, err error, message, remediation string) {
	t.Helper()
	got := diagnostics.Of(err)
	if len(got) != 1 || got[0].Code != "api.invariant" || got[0].Message != message || got[0].Remediation != remediation {
		t.Fatalf("want one api.invariant %q / %q, got %+v (%v)", message, remediation, got, err)
	}
}

func clusterNetworkRefusals(catalog api.Catalog) map[api.Kind][]lifecycle.Refusal {
	state := compilation.NewState(catalog, catalog, nil)
	out := map[api.Kind][]lifecycle.Refusal{}
	for _, kind := range []api.Kind{api.StorageCluster, api.StoragePool, api.StorageClient} {
		out[kind] = New(kind, nil).Unsupported(state)
	}
	return out
}

func TestEveryCephHostNeedsAnAddressInsideTheClusterNetworks(t *testing.T) {
	for _, test := range []struct {
		name      string
		cidrs     []string
		addresses map[int]string
		refused   string
	}{
		{name: "no host has an address inside", cidrs: []string{"198.51.100.0/24"}, refused: "node-1"},
		{name: "one host lacks one", cidrs: []string{"198.51.100.0/24"}, addresses: map[int]string{1: "198.51.100.1/24", 3: "198.51.100.3"}, refused: "node-2"},
		{name: "every host has one", cidrs: []string{"198.51.100.0/24"}, addresses: replicationAddresses("198.51.100.%d/24")},
		{name: "an address in the second network counts", cidrs: []string{"198.51.100.0/24", "203.0.113.0/24"}, addresses: replicationAddresses("203.0.113.%d")},
		{name: "runtime-assigned addresses declare none", cidrs: []string{"198.51.100.0/24"}, addresses: map[int]string{1: "198.51.100.1/24", 2: "node-2-replication.example.test", 3: "198.51.100.3/24"}, refused: "node-2"},
	} {
		t.Run(test.name, func(t *testing.T) {
			catalog := withReplicationAddresses(pinnedCatalog(t, withClusterCIDRs(test.cidrs...)), test.addresses)
			r, err := baseRequest(catalog, clusterOf(t, catalog), "synthetic")
			refusals := clusterNetworkRefusals(catalog)
			if test.refused == "" {
				if err != nil {
					t.Fatal(diagnostics.Of(err))
				}
				if !reflect.DeepEqual(r.ClusterNetworks, test.cidrs) || len(r.Hosts) != 3 {
					t.Fatalf("accepted request froze %v and %d hosts", r.ClusterNetworks, len(r.Hosts))
				}
				for kind, got := range refusals {
					if got != nil {
						t.Fatalf("%s refused %+v", kind, got)
					}
				}
				return
			}
			listed := test.cidrs[0]
			reason := "Ceph host " + test.refused + " (Machine " + test.refused + ") has no address inside the replication networks " + listed
			remedy := "add an address inside one of " + listed + " to the network of " + test.refused + clusterNetworksRemedy
			assertInvariant(t, err, reason, remedy)
			for kind, name := range map[api.Kind]string{api.StorageCluster: "example", api.StoragePool: "volumes", api.StorageClient: "consumer"} {
				want := []lifecycle.Refusal{{Kind: string(kind), Name: name, Reason: reason, Remediation: remedy}}
				if !reflect.DeepEqual(refusals[kind], want) {
					t.Fatalf("%s: got %+v, want %+v", kind, refusals[kind], want)
				}
			}
		})
	}
}

func namedHostCatalog(t *testing.T) api.Catalog {
	t.Helper()
	change := withNodeFQDN(2, "ceph-b.example.test", withClusterCIDRs("198.51.100.0/24", "2001:db8:100::/64"))
	return withReplicationAddresses(pinnedCatalog(t, change), map[int]string{1: "198.51.100.1/24", 3: "2001:db8:100::3/64"})
}

func TestClusterNetworkRefusalNamesHostMachineAndCIDRs(t *testing.T) {
	catalog := namedHostCatalog(t)
	const want = "Ceph host ceph-b.example.test (Machine node-2) has no address inside the replication networks 198.51.100.0/24, 2001:db8:100::/64"
	_, err := baseRequest(catalog, clusterOf(t, catalog), "synthetic")
	if got := diagnostics.Of(err); len(got) != 1 || got[0].Message != want {
		t.Fatalf("baseRequest reported %+v", got)
	}
	if got := clusterNetworkRefusals(catalog)[api.StorageCluster]; len(got) != 1 || got[0].Reason != want {
		t.Fatalf("Unsupported reported %+v", got)
	}
}

func TestClusterNetworkRefusalNamesTheNextStep(t *testing.T) {
	catalog := namedHostCatalog(t)
	const reason = "Ceph host ceph-b.example.test (Machine node-2) has no address inside the replication networks 198.51.100.0/24, 2001:db8:100::/64"
	const want = "add an address inside one of 198.51.100.0/24, 2001:db8:100::/64 to the network of node-2, or remove spec.ceph.networks.clusterCIDRs"
	_, err := baseRequest(catalog, clusterOf(t, catalog), "synthetic")
	assertInvariant(t, err, reason, want)
	if got := clusterNetworkRefusals(catalog)[api.StorageCluster]; len(got) != 1 || got[0].Remediation != want {
		t.Fatalf("Unsupported reported %+v", got)
	}
	input := lifecycle.PlanInput{Verb: reconciliation.Apply, Context: lifecycle.ContextIdentity{Name: "synthetic"}, State: compilation.NewState(catalog, catalog, nil), Controller: "controller"}
	runner := &recordingRunner{}
	for _, capability := range []lifecycle.Capability{New(api.StorageCluster, runner), NewHost(runner), NewImage(runner)} {
		plan, err := capability.Plan(context.Background(), input)
		assertInvariant(t, err, reason, want)
		if len(plan.Definitions) != 0 || len(plan.Secrets) != 0 {
			t.Fatalf("%T planned %+v", capability, plan)
		}
	}
	if runner.calls != 0 {
		t.Fatalf("the refusal ran %d calls", runner.calls)
	}
}

func TestNoClusterNetworksKeepsThePublicOnlyPlan(t *testing.T) {
	empty := func(spec api.Value) api.Value {
		return spec.WithPath(api.ListValue(), "ceph", "networks", "clusterCIDRs")
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
			plan, err := New(api.StorageCluster, nil).Plan(context.Background(), input)
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

func TestAnIPv6HostAddressMatchesOnlyAnIPv6ClusterNetwork(t *testing.T) {
	for _, test := range []struct {
		name, format, cidr string
		accepted           bool
	}{
		{name: "IPv6 inside IPv6", format: "2001:db8:100::%d/64", cidr: "2001:db8:100::/64", accepted: true},
		{name: "IPv6 against IPv4", format: "2001:db8:100::%d/64", cidr: "198.51.100.0/24"},
		{name: "IPv4 against IPv6", format: "198.51.100.%d", cidr: "2001:db8:100::/64"},
		{name: "IPv4-mapped IPv6 against IPv4", format: "::ffff:198.51.100.%d", cidr: "198.51.100.0/24"},
	} {
		t.Run(test.name, func(t *testing.T) {
			catalog := withReplicationAddresses(pinnedCatalog(t, withClusterCIDRs(test.cidr)), replicationAddresses(test.format))
			_, err := baseRequest(catalog, clusterOf(t, catalog), "synthetic")
			refusals := clusterNetworkRefusals(catalog)[api.StorageCluster]
			if test.accepted {
				if err != nil || refusals != nil {
					t.Fatalf("refused: %+v %+v", diagnostics.Of(err), refusals)
				}
				return
			}
			reason := "Ceph host node-1 (Machine node-1) has no address inside the replication networks " + test.cidr
			assertInvariant(t, err, reason, "add an address inside one of "+test.cidr+" to the network of node-1"+clusterNetworksRemedy)
			if len(refusals) != 1 || refusals[0].Reason != reason {
				t.Fatalf("Unsupported reported %+v", refusals)
			}
		})
	}
}
