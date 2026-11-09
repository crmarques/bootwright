package managedservice

import (
	"slices"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func cephHostConfig() api.Object {
	return api.NewObject(api.NetworkConfig, "hosts", api.Value{}, api.MapValue(
		field("nmstate", api.MapValue(field("interfaces", api.ListValue(
			api.MapValue(text("name", "nic0"), text("type", "ethernet"), text("state", "up")),
			api.MapValue(text("name", "nic1"), text("type", "ethernet"), text("state", "up")),
		)))),
	))
}

func cephHost(name, installRef string, addresses ...api.Value) api.Object {
	all := append([]api.Value{assignment("fqdn", name+".lab.example.test", "")}, addresses...)
	return clientMachine(name, "", installRef, all...)
}

func recordAddresses(t *testing.T, records []Record, name string) []string {
	t.Helper()
	for _, record := range records {
		if record.Name == name {
			return record.Addresses
		}
	}
	return nil
}

// A name answers what its resolver's clients can dial: an address on a Network
// with a gateway, and the installation address. The unrouted replication VLAN
// of a Ceph host is published under no name.
func TestAMachineNameAnswersRoutedAndInstallAddressesOnly(t *testing.T) {
	public := clientNetwork("ceph-public", field("ipv4", api.MapValue(text("cidr", "192.0.2.0/24"), text("gateway", "192.0.2.254"))))
	cluster := clientNetwork("ceph-cluster", field("ipv4", api.MapValue(text("cidr", "10.200.0.0/24"))))
	mgmt := clientNetwork("mgmt", field("ipv4", api.MapValue(text("cidr", "198.51.100.0/24"))))
	for _, tc := range []struct {
		name    string
		objects []api.Object
		want    []string
	}{
		{"an unrouted address beside a routed install address", []api.Object{cephHostConfig(), public, cluster,
			cephHost("ceph-01", "ip", assignment("ip", "192.0.2.11", "nic0"), assignment("cluster", "10.200.0.11", "nic0.200"))},
			[]string{"192.0.2.11"}},
		{"a routed address beside the install address on an unrouted Network", []api.Object{cephHostConfig(), public, mgmt,
			cephHost("ceph-01", "boot", assignment("boot", "198.51.100.11", "nic1"), assignment("ip", "192.0.2.11", "nic0"))},
			[]string{"192.0.2.11", "198.51.100.11"}},
		{"an install address on an unrouted Network alone", []api.Object{cephHostConfig(), mgmt,
			cephHost("ceph-01", "ip", assignment("ip", "198.51.100.11", "nic1"))},
			[]string{"198.51.100.11"}},
		{"a contact held by no interface", []api.Object{cephHostConfig(), public, cluster,
			cephHost("ceph-01", "ip", assignment("ip", "192.0.2.11", "nic0"), assignment("ssh", "10.200.0.99", ""))},
			[]string{"10.200.0.99", "192.0.2.11"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := recordAddresses(t, MachineRecords(catalogOf(tc.objects...)), "ceph-01.lab.example.test")
			if !slices.Equal(got, tc.want) {
				t.Fatalf("addresses = %v, want %v", got, tc.want)
			}
		})
	}
}

// The shape of the lab-5-nodes-ceph-networks run: every Ceph host stacks the
// unrouted replication VLAN on its public interface, and the application host
// holds two routed Networks. Only the Ceph names lose an address.
func TestARunWithAnUnroutedReplicationVLANPublishesItUnderNoName(t *testing.T) {
	public := clientNetwork("ceph-public", field("ipv4", api.MapValue(text("cidr", "192.0.2.0/24"), text("gateway", "192.0.2.254"))))
	users := clientNetwork("app-users", field("ipv4", api.MapValue(text("cidr", "203.0.113.0/24"), text("gateway", "203.0.113.254"))))
	cluster := clientNetwork("ceph-cluster", field("ipv4", api.MapValue(text("cidr", "10.200.0.0/24"))))
	objects := []api.Object{cephHostConfig(), public, users, cluster,
		cephHost("app-01", "ip", assignment("ip", "192.0.2.21", "nic0"), assignment("user", "203.0.113.21", "nic1"))}
	for index, last := range []string{"11", "12", "13"} {
		name := []string{"ceph-01", "ceph-02", "ceph-03"}[index]
		objects = append(objects, cephHost(name, "ip", assignment("ip", "192.0.2."+last, "nic0"), assignment("cluster", "10.200.0."+last, "nic0.200")))
	}
	records := MachineRecords(catalogOf(objects...))
	want := map[string][]string{
		"app-01.lab.example.test":  {"192.0.2.21", "203.0.113.21"},
		"ceph-01.lab.example.test": {"192.0.2.11"},
		"ceph-02.lab.example.test": {"192.0.2.12"},
		"ceph-03.lab.example.test": {"192.0.2.13"},
	}
	if len(records) != len(want) {
		t.Fatalf("records = %+v", records)
	}
	for name, addresses := range want {
		if got := recordAddresses(t, records, name); !slices.Equal(got, addresses) {
			t.Fatalf("%s answers %v, want %v", name, got, addresses)
		}
	}
}
