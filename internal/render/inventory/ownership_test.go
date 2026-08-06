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
