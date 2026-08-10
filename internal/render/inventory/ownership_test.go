package inventory

import (
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/ownership"
)

func TestInventoryWithOwnershipRecordsAddsRecordedHost(t *testing.T) {
	records := []ownership.ResourceRecord{{
		Kind: "libvirt-domain",
		Name: "cluster-a-machine-0",
		Host: "provider-0",
		HostFacts: map[string]string{
			"ansible_connection": "local",
		},
	}}
	inv := InventoryWithOwnershipRecordsAndPathOptions(v1alpha1.State{}, PathOptions{}, records)
	all := inv["all"].(map[string]any)
	hosts := all["hosts"].(map[string]any)
	host := hosts["provider-0"].(map[string]any)
	if got := host["ansible_connection"]; got != "local" {
		t.Fatalf("recorded host ansible_connection = %v, want local", got)
	}
	children := all["children"].(map[string]any)
	infraHosts := children[GroupInfraHosts].(map[string]any)["hosts"].(map[string]any)
	if _, ok := infraHosts["provider-0"]; !ok {
		t.Fatalf("%s hosts = %v, want provider-0", GroupInfraHosts, infraHosts)
	}
	counts := HostGroupCountsWithOwnershipRecords(v1alpha1.State{}, records)
	if got := counts[GroupInfraHosts]; got != 1 {
		t.Fatalf("%s count = %d, want 1", GroupInfraHosts, got)
	}
}

func TestInventoryWithOwnershipRecordsMergesHostFactsAcrossRecords(t *testing.T) {
	records := []ownership.ResourceRecord{{
		Kind: "infra-component",
		Name: "InfraComponent-artifact-server",
		Host: "bastion",
		HostFacts: map[string]string{
			"ansible_connection": "local",
		},
	}, {
		Kind: "infra-component",
		Name: "InfraComponent-registry",
		Host: "bastion",
	}}

	inv := InventoryWithOwnershipRecordsAndPathOptions(v1alpha1.State{}, PathOptions{}, records)
	host := inv["all"].(map[string]any)["hosts"].(map[string]any)["bastion"].(map[string]any)

	if got := host["ansible_connection"]; got != "local" {
		t.Fatalf("a record without host facts must not strip the connection facts of another record for the same host: %v", host)
	}
}

func TestControllerOwnershipRecordCreatesOnlyLocalControllerInventory(t *testing.T) {
	records := []ownership.ResourceRecord{{
		Kind: string(ownership.KindControllerNameResolver),
		Name: "resolver-record",
		Host: "untrusted-remote-host",
		HostFacts: map[string]string{
			"ansible_connection": "ssh",
			"ansible_host":       "203.0.113.10",
		},
	}}

	inv := InventoryWithOwnershipRecordsAndPathOptions(v1alpha1.State{}, PathOptions{}, records)
	all := inv["all"].(map[string]any)
	hosts := all["hosts"].(map[string]any)
	if _, found := hosts["untrusted-remote-host"]; found {
		t.Fatalf("controller ownership evidence created a remote inventory host: %#v", hosts)
	}
	localhost := hosts["localhost"].(map[string]any)
	if localhost["ansible_connection"] != "local" || localhost["ansible_host"] != "localhost" {
		t.Fatalf("controller ownership evidence did not pin localhost: %#v", localhost)
	}
	controllerHosts := all["children"].(map[string]any)[GroupControllerHosts].(map[string]any)["hosts"].(map[string]any)
	if len(controllerHosts) != 1 {
		t.Fatalf("%s hosts = %#v, want only localhost", GroupControllerHosts, controllerHosts)
	}
	if _, found := controllerHosts["localhost"]; !found {
		t.Fatalf("%s hosts = %#v, want localhost", GroupControllerHosts, controllerHosts)
	}
	counts := HostGroupCountsWithOwnershipRecords(v1alpha1.State{}, records)
	members := HostGroupMembersWithOwnershipRecords(v1alpha1.State{}, records)
	if counts[GroupControllerHosts] != 1 || len(members[GroupControllerHosts]) != 1 || members[GroupControllerHosts][0] != "localhost" {
		t.Fatalf("controller group facts counts=%#v members=%#v", counts, members)
	}
}

func TestDNSOwnershipFirewallPortsRenderForRecordDrivenTeardown(t *testing.T) {
	records := []ownership.ResourceRecord{{
		Kind:  string(ownership.KindInfraComponent),
		Name:  "InfraComponent-dns",
		Owner: ownership.Owner,
		Attributes: map[string]string{
			"port":    "53/tcp",
			"udpPort": "53/udp",
		},
	}}

	vars := VarsWithPathOptionsAndOwnership(v1alpha1.State{}, PathOptions{}, records)
	rendered := vars["bootwright_ownership_records"].([]any)[0].(map[string]any)
	attrs := rendered["attributes"].(map[string]any)
	if attrs["port"] != "53/tcp" || attrs["udpPort"] != "53/udp" {
		t.Fatalf("rendered DNS teardown ports = %#v, want scalar TCP and UDP endpoints", attrs)
	}
	if _, found := attrs["ports"]; found {
		t.Fatalf("rendered DNS teardown attributes retained a plural list value: %#v", attrs)
	}
}
