package inventory

import (
	"path/filepath"
	"testing"

	desiredstate "github.com/crmarques/bootwright/internal/state/desired"
)

// TestMachineTaskHostEntriesUseLocalhostForAPISubstrates pins the per-machine
// task-host entries for API-native substrates: no Machine object backs the
// localhost provider-host ref, yet the machine-task groups reference the
// per-machine host names, so the entries must be emitted with a local
// connection instead of being silently dropped.
func TestMachineTaskHostEntriesUseLocalhostForAPISubstrates(t *testing.T) {
	cases := []struct {
		name        string
		paths       []string
		clusterName string
		machineName string
	}{
		{
			name:        "vsphere fixture",
			paths:       []string{filepath.Join(fixtureRoot, "007-sno-vsphere")},
			clusterName: "sno-vsphere",
			machineName: "master-0",
		},
		{
			name:        "kubevirt child example",
			paths:       []string{filepath.Join("..", "..", "..", "examples", "baremetal-redfish-multidc-virtualized-odf-ceph")},
			clusterName: "dc1-child-ocp",
			machineName: "dc1-child-ocp-infra-master-0",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			state, err := desiredstate.LoadNormalizeValidate(tc.paths)
			if err != nil {
				t.Fatalf("LoadNormalizeValidate: %v", err)
			}
			inv := Inventory(state, "")
			all := inv["all"].(map[string]any)
			hosts := all["hosts"].(map[string]any)
			hostName := MachineInfraHostName(tc.clusterName, tc.machineName)
			raw, ok := hosts[hostName]
			if !ok {
				t.Fatalf("inventory missing machine task host %q (hosts: %v)", hostName, mapKeys(hosts))
			}
			entry := raw.(map[string]any)
			if got := entry["ansible_connection"]; got != "local" {
				t.Fatalf("%s ansible_connection = %v, want local", hostName, got)
			}
			if got := entry["bootwright_machine_task_cluster_name"]; got != tc.clusterName {
				t.Fatalf("%s task cluster = %v, want %s", hostName, got, tc.clusterName)
			}
			if got := entry["bootwright_machine_task_machine_name"]; got != tc.machineName {
				t.Fatalf("%s task machine = %v, want %s", hostName, got, tc.machineName)
			}
			if got := entry["bootwright_machine_task_provider_host_name"]; got != "localhost" {
				t.Fatalf("%s task provider host = %v, want localhost", hostName, got)
			}
			children := all["children"].(map[string]any)
			group := children[MachineInfraGroupName(tc.clusterName)].(map[string]any)["hosts"].(map[string]any)
			if _, ok := group[hostName]; !ok {
				t.Fatalf("machine task group %s missing host %s: %v", MachineInfraGroupName(tc.clusterName), hostName, group)
			}
		})
	}
}
