package inventory

import (
	"reflect"
	"testing"
)

func nmstateIPv4(ip string, prefix int) map[string]any {
	return map[string]any{
		"enabled": true,
		"address": []any{map[string]any{"ip": ip, "prefix-length": prefix}},
	}
}

func TestKickstartNetworkInterfacesPrefersDefaultRouteVLAN(t *testing.T) {
	config := map[string]any{
		"interfaces": []any{
			map[string]any{
				"name": "bond0", "type": "bond",
				"link-aggregation": map[string]any{
					"mode":    "active-backup",
					"options": map[string]any{"miimon": "100"},
					"port":    []any{"ens1f0", "ens1f1"},
				},
			},
			map[string]any{
				"name": "bond0.744", "type": "vlan",
				"vlan": map[string]any{"base-iface": "bond0", "id": 744},
				"ipv4": nmstateIPv4("10.7.7.193", 28),
			},
			map[string]any{
				"name": "bond0.743", "type": "vlan",
				"vlan": map[string]any{"base-iface": "bond0", "id": 743},
				"ipv4": nmstateIPv4("10.7.7.129", 28),
			},
		},
		"routes": map[string]any{
			"config": []any{map[string]any{
				"destination":        "0.0.0.0/0",
				"next-hop-address":   "10.7.7.142",
				"next-hop-interface": "bond0.743",
			}},
		},
	}
	stanzas := kickstartNetworkInterfaces(config)
	if len(stanzas) != 1 {
		t.Fatalf("stanzas = %v, want one merged bond+VLAN stanza", stanzas)
	}
	want := map[string]any{
		"device":      "bond0",
		"vlanID":      743,
		"bondSlaves":  []string{"ens1f0", "ens1f1"},
		"bondOptions": "mode=active-backup,miimon=100",
		"bootproto":   "static",
		"ip":          "10.7.7.129",
		"prefix":      28,
		"netmask":     "255.255.255.240",
		"hostname":    true,
	}
	if !reflect.DeepEqual(stanzas[0], want) {
		t.Fatalf("stanza = %v, want %v", stanzas[0], want)
	}
}

func TestKickstartNetworkInterfacesMinimalBondVLANPrimaryOnly(t *testing.T) {
	config := map[string]any{
		"interfaces": []any{
			map[string]any{
				"name": "ens1f0", "type": "ethernet", "mtu": 9000,
			},
			map[string]any{
				"name": "ens1f1", "type": "ethernet", "mtu": 9000,
			},
			map[string]any{
				"name": "bond0", "type": "bond", "mtu": 9000,
				"link-aggregation": map[string]any{
					"mode": "802.3ad",
					"options": map[string]any{
						"lacp_rate": "fast",
						"miimon":    "100",
					},
					"port": []any{"ens1f0", "ens1f1"},
				},
			},
			map[string]any{
				"name": "bond0.743", "type": "vlan", "mtu": 9000,
				"vlan": map[string]any{"base-iface": "bond0", "id": 743},
				"ipv4": nmstateIPv4("10.7.7.129", 28),
			},
		},
		"routes": map[string]any{
			"config": []any{map[string]any{
				"destination":        "0.0.0.0/0",
				"next-hop-address":   "10.7.7.142",
				"next-hop-interface": "bond0.743",
			}},
		},
	}
	stanzas := kickstartNetworkInterfaces(config)
	if len(stanzas) != 1 {
		t.Fatalf("stanzas = %v, want a single merged bond+VLAN stanza (no per-slave lines)", stanzas)
	}
	want := map[string]any{
		"device":      "bond0",
		"vlanID":      743,
		"bondSlaves":  []string{"ens1f0", "ens1f1"},
		"bondOptions": "mode=802.3ad,lacp_rate=fast,miimon=100",
		"bootproto":   "static",
		"ip":          "10.7.7.129",
		"prefix":      28,
		"netmask":     "255.255.255.240",
		"hostname":    true,
	}
	if !reflect.DeepEqual(stanzas[0], want) {
		t.Fatalf("merged stanza = %v, want %v (no mtu key)", stanzas[0], want)
	}
	if _, present := stanzas[0]["mtu"]; present {
		t.Fatalf("merged stanza carries mtu %v; kickstart must omit MTU (post-install nmstate owns it)", stanzas[0]["mtu"])
	}
}

func TestKickstartNetworkPrefersIPv4DefaultRoute(t *testing.T) {
	config := map[string]any{
		"interfaces": []any{
			map[string]any{
				"name": "bond0.744", "type": "vlan",
				"vlan": map[string]any{"base-iface": "bond0", "id": 744},
				"ipv4": nmstateIPv4("10.7.7.193", 28),
			},
			map[string]any{
				"name": "bond0.743", "type": "vlan",
				"vlan": map[string]any{"base-iface": "bond0", "id": 743},
				"ipv4": nmstateIPv4("10.7.7.129", 28),
			},
		},
		"routes": map[string]any{
			"config": []any{
				map[string]any{
					"destination":        "::/0",
					"next-hop-address":   "fd00::1",
					"next-hop-interface": "bond0.744",
				},
				map[string]any{
					"destination":        "0.0.0.0/0",
					"next-hop-address":   "10.7.7.142",
					"next-hop-interface": "bond0.743",
				},
			},
		},
	}
	stanzas := kickstartNetworkInterfaces(config)
	if len(stanzas) != 1 || stanzas[0]["ip"] != "10.7.7.129" {
		t.Fatalf("stanzas = %v, want the IPv4-routed bond0.743 VLAN", stanzas)
	}
	if gateway := networkConfigGatewayFromMap(config); gateway != "10.7.7.142" {
		t.Fatalf("gateway = %v, want the IPv4 default route next-hop", gateway)
	}
}

func TestKickstartNetworkInterfacesFallsBackWhenRoutedInterfaceHasNoIPv4(t *testing.T) {
	config := map[string]any{
		"interfaces": []any{
			map[string]any{
				"name": "bond0.744", "type": "vlan",
				"vlan": map[string]any{"base-iface": "bond0", "id": 744},
				"ipv4": nmstateIPv4("10.7.7.193", 28),
			},
		},
		"routes": map[string]any{
			"config": []any{map[string]any{
				"destination":        "0.0.0.0/0",
				"next-hop-address":   "10.7.7.142",
				"next-hop-interface": "bond0",
			}},
		},
	}
	stanzas := kickstartNetworkInterfaces(config)
	if len(stanzas) != 1 || stanzas[0]["ip"] != "10.7.7.193" {
		t.Fatalf("stanzas = %v, want fallback to the first interface with an IPv4", stanzas)
	}
}

func TestKickstartNetworkInterfacesVLANOverEthernet(t *testing.T) {
	config := map[string]any{
		"interfaces": []any{
			map[string]any{
				"name": "eno1", "type": "ethernet",
				"mac-address": "52:54:00:aa:bb:cc",
			},
			map[string]any{
				"name": "public", "type": "vlan",
				"vlan": map[string]any{"base-iface": "eno1", "id": 20},
				"ipv4": nmstateIPv4("192.168.20.5", 24),
			},
		},
	}
	stanzas := kickstartNetworkInterfaces(config)
	if len(stanzas) != 1 {
		t.Fatalf("stanzas = %v, want one VLAN stanza", stanzas)
	}
	got := stanzas[0]
	if got["device"] != "52:54:00:aa:bb:cc" || got["vlanID"] != 20 || got["interfaceName"] != "public" {
		t.Fatalf("stanza = %v, want device by parent MAC, vlanID 20, interfaceName public", got)
	}
}
