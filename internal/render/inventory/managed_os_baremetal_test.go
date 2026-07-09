package inventory

import (
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	desiredstate "github.com/crmarques/bootwright/internal/state/desired"
)

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
		image := component["osInstall"].(map[string]any)["image"].(map[string]any)
		if image["sourceOnTarget"] != true {
			t.Fatalf("component %v sourceOnTarget = %v, want true for a controller-driven bare-metal install", component["name"], image["sourceOnTarget"])
		}
		if image["sourceId"] != "rhel-9.7-x86_64-dvd.iso" {
			t.Fatalf("component %v sourceId = %v, want the media key", component["name"], image["sourceId"])
		}
		if image["effectiveSourcePath"] != image["path"] {
			t.Fatalf("component %v effectiveSourcePath = %v, want image.path %v", component["name"], image["effectiveSourcePath"], image["path"])
		}
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
