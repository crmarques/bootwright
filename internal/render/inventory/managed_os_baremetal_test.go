package inventory

import (
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
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

		// The full nmstate is exposed for the post-install network task, and it carries
		// exactly what the minimal kickstart drops: BOTH VLANs (routed bond0.140 plus
		// the secondary cluster bond0.141 that never reaches the kickstart), MTU 9000
		// on every layer, and the cluster-network IP. This is the multi-VLAN + jumbo
		// golden the gitignored nprd fixture cannot pin.
		desiredState, ok := component["osInstall"].(map[string]any)["network"].(map[string]any)["desiredState"].(map[string]any)
		if !ok {
			t.Fatalf("component %v missing osInstall.network.desiredState for the post-install nmstate apply", component["name"])
		}
		byName := map[string]map[string]any{}
		ifaces, _ := desiredState["interfaces"].([]any)
		for _, raw := range ifaces {
			if iface, ok := raw.(map[string]any); ok {
				name, _ := iface["name"].(string)
				byName[name] = iface
			}
		}
		for _, name := range []string{"eno1", "eno2", "bond0", "bond0.140", "bond0.141"} {
			iface, ok := byName[name]
			if !ok {
				t.Fatalf("component %v desiredState missing interface %q; the post-install apply needs the whole document", component["name"], name)
			}
			if got := fmt.Sprint(iface["mtu"]); got != "9000" {
				t.Fatalf("component %v desiredState interface %q mtu = %v, want 9000 (nmstate sets the MTU the kickstart cannot)", component["name"], name, iface["mtu"])
			}
		}
		if got := firstDesiredStateIPv4(byName["bond0.141"]); !strings.HasPrefix(got, "192.168.141.") {
			t.Fatalf("component %v desiredState cluster VLAN bond0.141 IP = %q, want a 192.168.141.x cluster address", component["name"], got)
		}

		// Each ethernet port carries the machine's authored (permanent) MAC and is
		// marked identifier: mac-address. Applied to the already-running OS, every
		// bond member's running MAC is the shared bond MAC, so verifying an authored
		// permanent MAC by kernel name always fails and rolls the apply back; MAC
		// identity makes nmstate match on the permanent MAC and verify the in-config
		// value instead. The bond and VLAN layers own no MAC, so they must NOT be
		// marked - identifier: mac-address requires a mac-address match key.
		for _, name := range []string{"eno1", "eno2"} {
			iface := byName[name]
			if got, _ := iface["mac-address"].(string); got == "" {
				t.Fatalf("component %v desiredState ethernet %q missing mac-address; identifier: mac-address needs it as the match key", component["name"], name)
			}
			if got := iface["identifier"]; got != "mac-address" {
				t.Fatalf("component %v desiredState ethernet %q identifier = %v, want mac-address so a bonded port verifies against its permanent MAC not the bond MAC", component["name"], name, got)
			}
		}
		for _, name := range []string{"bond0", "bond0.140", "bond0.141"} {
			if got, present := byName[name]["identifier"]; present {
				t.Fatalf("component %v desiredState %q identifier = %v, want none (no mac-address to match on)", component["name"], name, got)
			}
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

// firstDesiredStateIPv4 returns the first IPv4 address of a rendered nmstate
// interface, or "" if it carries none.
func firstDesiredStateIPv4(iface map[string]any) string {
	v4, ok := iface["ipv4"].(map[string]any)
	if !ok {
		return ""
	}
	addrs, ok := v4["address"].([]any)
	if !ok || len(addrs) == 0 {
		return ""
	}
	addr, ok := addrs[0].(map[string]any)
	if !ok {
		return ""
	}
	ip, _ := addr["ip"].(string)
	return ip
}
