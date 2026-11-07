package cephcluster

import (
	"fmt"
	"reflect"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

// withIPv6Hosts gives each fixture Machine a second address on 2001:db8::/64,
// so a dual-stack public network has its hosts on both families.
func withIPv6Hosts(catalog api.Catalog) api.Catalog {
	catalog = replace(catalog, lanNetwork("public-v6", "2001:db8::/64"))
	for i := 1; i <= 3; i++ {
		catalog = withMachineAddress(catalog, fmt.Sprintf("machine-%d", i), fmt.Sprintf("2001:db8::%d", i))
	}
	return catalog
}

func withMachineAddress(catalog api.Catalog, name, address string) api.Catalog {
	machine, _ := catalog.Find(api.Machine, name)
	addresses := append(machine.Spec().Get("network", "addresses").Items(), assignedAddress("extra", address))
	return replace(catalog, machine.WithSpec(machine.Spec().WithPath(api.ListValue(addresses...), "network", "addresses")))
}

func withReplicationHosts(catalog api.Catalog, names ...string) api.Catalog {
	catalog = replace(catalog, lanNetwork("replication", "203.0.113.0/24"))
	for i, name := range names {
		catalog = withMachineAddress(catalog, "machine-"+name, fmt.Sprintf("203.0.113.%d", 10+i))
	}
	return catalog
}

func TestEveryCephCIDRContainsATopologyMachineAddress(t *testing.T) {
	for _, test := range []struct {
		name, field, message string
		change               func(api.Value) api.Value
	}{
		{"the tiebreaker's own routed subnet counts", "", "", nil},
		{"a public CIDR no topology Machine is on", "$.spec.networks.publicCIDRs[2]", "the public CIDR 203.0.113.0/24 contains no address a topology Machine declares", func(s api.Value) api.Value {
			return s.WithPath(api.StringList("192.0.2.0/24", "198.51.100.0/24", "203.0.113.0/24"), "networks", "publicCIDRs")
		}},
		{"a cluster CIDR no topology Machine is on", "$.spec.networks.clusterCIDRs[0]", "the replication CIDR 203.0.113.0/24 contains no address a topology Machine declares", func(s api.Value) api.Value {
			return s.WithPath(api.StringList("203.0.113.0/24"), "networks", "clusterCIDRs")
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			cluster, catalog := stretchCatalog(t, test.change)
			catalog = replace(catalog, lanNetwork("unused", "203.0.113.0/24"))
			issues := Validate(cluster, catalog)
			if test.field == "" {
				if len(issues) != 0 {
					t.Fatalf("refused: %#v", issues)
				}
				return
			}
			requireOnlyIssue(t, issuesUnder(issues, "$.spec.networks"), test.field, test.message)
		})
	}
}

func TestReplicationCoverageIsAdmittedAndExemptsTheTiebreaker(t *testing.T) {
	replication := func(s api.Value) api.Value {
		return s.WithPath(api.StringList("203.0.113.0/24"), "networks", "clusterCIDRs")
	}
	cluster, catalog := stretchCatalog(t, replication)
	catalog = withReplicationHosts(catalog, "a1", "a2", "a3", "b1", "b2", "b3")
	if issues := Validate(cluster, catalog); len(issues) != 0 {
		t.Fatalf("a MON-only tiebreaker without a replication address refused: %#v", issues)
	}
	cluster, catalog = stretchCatalog(t, replication)
	catalog = withReplicationHosts(catalog, "a1", "a2", "b1", "b2", "b3")
	want := []api.Issue{{Code: "api.invariant", Field: "$.spec.topology.nodes[2].machineRef",
		Message:     "Ceph host a3.storage.example.test (Machine machine-a3) has no address inside the replication networks 203.0.113.0/24",
		Remediation: "add an address inside one of 203.0.113.0/24 to the network of machine-a3, or remove spec.networks.clusterCIDRs"}}
	if issues := Validate(cluster, catalog); !reflect.DeepEqual(issues, want) {
		t.Fatalf("got %#v, want %#v", issues, want)
	}
}

func TestEveryIngressHostIsOnTheVirtualIPSubnet(t *testing.T) {
	catalog := bindingCatalog()
	moved, _ := catalog.Find(api.Machine, "machine-2")
	catalog = replace(catalog, lanNetwork("public", "192.0.2.0/24", "site-a"))
	catalog = replace(catalog, lanNetwork("narrow", "192.0.2.0/25", "site-b"))
	catalog = replace(catalog, moved.WithSpec(moved.Spec().WithPath(api.StringValue("site-b"), "placement", "site")))
	services := graphServices()
	services[1] = services[1].WithPath(api.StringValue("192.0.2.200/24"), "spec", "virtual_ip")
	cluster, changed := withGraph(t, catalog, services)
	requireOnlyIssue(t, issuesAt(Validate(cluster, changed), rgwIngressAt+".spec.virtual_ip"), rgwIngressAt+".spec.virtual_ip",
		"ingress host node-2.storage.example.test (Machine machine-2) has no address whose prefix contains the virtual IP")
	services[1] = services[1].WithPath(api.StringList("192.0.2.0/25"), "spec", "virtual_interface_networks")
	cluster, changed = withGraph(t, catalog, services)
	if found := issuesUnder(Validate(cluster, changed), rgwIngressAt); len(found) != 0 {
		t.Fatalf("hosts inside virtual_interface_networks refused: %#v", found)
	}
}

func TestEveryCephCIDRIsADeclaredNetwork(t *testing.T) {
	cluster, catalog := stretchCatalog(t, nil)
	cidrs := cluster.Spec().Get("networks", "publicCIDRs").Strings()
	if len(cidrs) < 2 {
		t.Fatalf("the fixture declares public CIDRs %v", cidrs)
	}
	declare := func(cidrs ...string) api.Catalog {
		objects := []api.Object{}
		for _, object := range catalog.Objects() {
			if object.Kind() != api.Network {
				objects = append(objects, object)
			}
		}
		for i, cidr := range cidrs {
			objects = append(objects, lanNetwork(fmt.Sprintf("public-%d", i), cidr))
		}
		return api.NewCatalog(objects)
	}
	if issues := issuesUnder(Validate(cluster, declare(cidrs...)), "$.spec.networks"); len(issues) != 0 {
		t.Fatalf("declared networks refused: %#v", issues)
	}
	requireOnlyIssue(t, issuesUnder(Validate(cluster, declare(cidrs[0])), "$.spec.networks"), "$.spec.networks.publicCIDRs[1]",
		"the public CIDR "+cidrs[1]+" of "+cluster.Identity()+" is the cidr of no declared Network")
}
