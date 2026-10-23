package agentinstall

import (
	"encoding/json"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/substrate"
)

func TestBondVLANAndAddresslessProjectionGolden(t *testing.T) {
	for _, mode := range []string{"active-backup", "802.3ad"} {
		t.Run(mode, func(t *testing.T) {
			ethernet := func(name string) api.Value {
				return api.MapValue(text("name", name), text("type", "ethernet"), text("state", "up"), field("ipv4", api.MapValue(field("enabled", api.BoolValue(false)))), field("ipv6", api.MapValue(field("enabled", api.BoolValue(false)))))
			}
			interfaces := api.ListValue(ethernet("eno1"), ethernet("eno2"), ethernet("eno3"), api.MapValue(text("name", "bond0"), text("type", "bond"), text("state", "up"), field("link-aggregation", api.MapValue(text("mode", mode), field("port", api.StringList("eno1", "eno2"))))), api.MapValue(text("name", "bond0.100"), text("type", "vlan"), text("state", "up"), field("ipv4", api.MapValue(field("enabled", api.BoolValue(true)))), field("vlan", api.MapValue(text("base-iface", "bond0"), number("id", "100")))))
			native := api.MapValue(field("interfaces", interfaces), field("routes", api.MapValue(field("config", api.ListValue(api.MapValue(text("destination", "0.0.0.0/0"), text("next-hop-interface", "bond0.100"), text("next-hop-address", "198.51.100.1")))))))
			inline := api.MapValue(field("machineNetwork", api.ListValue(api.MapValue(text("cidr", "198.51.100.0/24")))), field("nmstate", native))
			addresses := api.ListValue(api.MapValue(text("name", "ip"), text("interface", "bond0.100"), text("address", "198.51.100.21/24")))
			networkSpec := api.MapValue(field("inline", inline), field("addresses", addresses), text("installAddressRef", "ip"))
			node := api.NewObject(api.Machine, "node", api.Value{}, api.MapValue(field("network", networkSpec)))
			physical := []substrate.Interface{{Name: "eno1", MACAddress: "02:00:00:00:00:01"}, {Name: "eno2", MACAddress: "02:00:00:00:00:02"}, {Name: "eno3", MACAddress: "02:00:00:00:00:03"}}
			network, err := networkConfig(api.NewCatalog(nil), node, physical, nil)
			if err != nil {
				t.Fatal(err)
			}
			for _, raw := range network["interfaces"].([]any) {
				entry := raw.(map[string]any)
				name := entry["name"].(string)
				if name == "eno1" || name == "eno2" {
					if entry["identifier"] != "mac-address" {
						t.Fatal(entry)
					}
				}
				if entry["type"] == "bond" || entry["type"] == "vlan" {
					if _, ok := entry["mac-address"]; ok {
						t.Fatal(entry)
					}
				}
			}
			output := map[string]any{"networkConfig": network, "interfaces": nodeInterfaces(nodeProjection{interfaces: physical})}
			data, err := json.Marshal(output)
			if err != nil {
				t.Fatal(err)
			}
			matchesGolden(t, "node-network-"+mode, data)
		})
	}
}
