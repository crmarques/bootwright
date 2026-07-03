package inventory

import (
	"path/filepath"
	"reflect"
	"testing"

	desiredstate "github.com/crmarques/bootwright/internal/state/desired"
)

// Bare-metal Ceph nodes carry no substrate provider host: their managed-OS
// install is driven from the controller over the BMC (Redfish), like the
// API-native KubeVirt/vSphere substrates. They must still land in the cluster's
// managed-OS inventory group with a controller-local connection, or the
// machines-phase install task is created but skipped at runtime because its
// --limit group resolves to zero hosts. This is the bare-metal counterpart to
// TestManagedOSInstallVarsFromCephLibvirtFixture (fixture 006).
func TestManagedOSInstallVarsFromCephBaremetalFixture(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join("..", "..", "..", "test", "e2e", "009-ceph-3nodes-baremetal-managed-os")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}

	group := ManagedOSGroupName("ceph-baremetal")
	members := HostGroupMembers(state)[group]
	if len(members) != 3 {
		t.Fatalf("managed-OS group %q members = %v, want 3 bare-metal nodes", group, members)
	}
	if got := HostGroupCounts(state)[group]; got != 3 {
		t.Fatalf("managed-OS group %q count = %d, want 3 (an empty group skips the install task)", group, got)
	}

	// The install runs on the controller (ansible_connection: local) and the
	// host's provider_host_name must equal the component's machineRef so the
	// managed-OS play selects it.
	host := ManagedOSHostName("ceph-baremetal", "ceph-0")
	hosts := Inventory(state, "/context/secrets")["all"].(map[string]any)["hosts"].(map[string]any)
	entry, ok := hosts[host].(map[string]any)
	if !ok {
		t.Fatalf("inventory all.hosts missing managed-OS host %q", host)
	}
	if entry["ansible_connection"] != "local" {
		t.Fatalf("host %q ansible_connection = %v, want local", host, entry["ansible_connection"])
	}
	providerHost := entry["bootwright_machine_task_provider_host_name"]
	if providerHost != "localhost" {
		t.Fatalf("host %q provider_host_name = %v, want localhost", host, providerHost)
	}

	groups := VarsWithSecretsDir(state, "/context/secrets")["bootwright_managed_os_install_groups"].([]any)
	if len(groups) != 1 {
		t.Fatalf("managed OS groups = %v, want 1", groups)
	}
	components := groups[0].(map[string]any)["components"].([]any)
	if len(components) != 3 {
		t.Fatalf("components = %v, want 3", components)
	}
	for _, raw := range components {
		component := raw.(map[string]any)
		if component["machineRef"] != providerHost {
			t.Fatalf("component %v machineRef = %v, want %v to match the inventory host so the play selects it", component["name"], component["machineRef"], providerHost)
		}
		// The bare-metal install runs on the controller (localhost), where the
		// media library already holds the source ISO, so the role must skip the
		// copy-to-provider step and read the media in place. Without this the
		// role copies the full DVD once per node into per-machine paths and can
		// exhaust the controller's disk.
		image := component["osInstall"].(map[string]any)["image"].(map[string]any)
		if image["sourceOnTarget"] != true {
			t.Fatalf("component %v sourceOnTarget = %v, want true for a controller-driven bare-metal install", component["name"], image["sourceOnTarget"])
		}
		// The renderer owns the shared-source identity and effective path the
		// install role consumes. With no checksum the sourceId is the media key,
		// and a controller-local sourceOnTarget install reads the media in place.
		if image["sourceId"] != "rhel-9.7-x86_64-dvd.iso" {
			t.Fatalf("component %v sourceId = %v, want the media key", component["name"], image["sourceId"])
		}
		if image["effectiveSourcePath"] != image["path"] {
			t.Fatalf("component %v effectiveSourcePath = %v, want image.path %v", component["name"], image["effectiveSourcePath"], image["path"])
		}
		// The install-time kickstart stays minimal: one merged bond+VLAN stanza for
		// the routed interface. --bondslaves builds the bond and its port
		// connections (no separate per-slave stanzas), --device names the parent
		// bond, and the static IP settings apply to the VLAN device. MTU and every
		// other interface are configured post-install from osInstall.network.
		// desiredState, so they never appear here.
		network := component["osInstall"].(map[string]any)["kickstart"].(map[string]any)["network"].(map[string]any)
		stanzas, ok := network["interfaces"].([]map[string]any)
		if !ok || len(stanzas) != 1 {
			t.Fatalf("component %v kickstart network interfaces = %v, want a single merged bond+VLAN stanza", component["name"], network["interfaces"])
		}
		vlan := stanzas[0]
		if vlan["device"] != "bond0" || vlan["vlanID"] != 140 {
			t.Fatalf("component %v VLAN stanza = %v, want --device=bond0 --vlanid=140", component["name"], vlan)
		}
		if _, present := vlan["interfaceName"]; present {
			t.Fatalf("component %v VLAN stanza = %v, want no interfaceName for the derived default bond0.140", component["name"], vlan)
		}
		if _, present := vlan["mtu"]; present {
			t.Fatalf("component %v VLAN stanza = %v, want no MTU in the kickstart (post-install nmstate owns it)", component["name"], vlan)
		}
		if got := vlan["bondSlaves"]; !reflect.DeepEqual(got, []string{"eno1", "eno2"}) {
			t.Fatalf("component %v bondSlaves = %v, want eno1/eno2", component["name"], got)
		}
		if vlan["bondOptions"] != "mode=active-backup,miimon=100" {
			t.Fatalf("component %v bondOptions = %v", component["name"], vlan["bondOptions"])
		}
		if vlan["bootproto"] != "static" || vlan["netmask"] != "255.255.255.0" || vlan["hostname"] != true {
			t.Fatalf("component %v static VLAN stanza = %v", component["name"], vlan)
		}

		// The full nmstate is exposed for the post-install network task: the whole
		// document (bond, slaves, VLAN, MTU) that nmstate applies after install,
		// realizing the MTU and secondary interfaces the minimal kickstart omits.
		desiredState, ok := component["osInstall"].(map[string]any)["network"].(map[string]any)["desiredState"].(map[string]any)
		if !ok {
			t.Fatalf("component %v missing osInstall.network.desiredState for the post-install nmstate apply", component["name"])
		}
		if ifaces, _ := desiredState["interfaces"].([]any); len(ifaces) == 0 {
			t.Fatalf("component %v osInstall.network.desiredState has no interfaces", component["name"])
		}

		// A bare-metal node mounts its managed-OS install ISO over the BMC, so
		// the install ISO is published through the artifact server for Redfish
		// virtual media. A StorageCluster cannot author spec.install.artifactAccess,
		// so this access is derived from the Environment defaults; without it the
		// boot.agentIso stage path resolves empty and the install role fails on
		// `dirname("")` with "No such file or directory: b''".
		boot, ok := component["boot"].(map[string]any)
		if !ok {
			t.Fatalf("component %v missing boot block", component["name"])
		}
		iso, ok := boot["agentIso"].(map[string]any)
		if !ok {
			t.Fatalf("component %v missing boot.agentIso", component["name"])
		}
		for _, field := range []string{"stageHost", "stagePath", "fetchUrl"} {
			if value, _ := iso[field].(string); value == "" {
				t.Fatalf("component %v boot.agentIso.%s is empty; bare-metal managed-OS install needs the artifact-server Redfish virtual-media path", component["name"], field)
			}
		}
	}
}
