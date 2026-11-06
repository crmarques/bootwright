package agentinstall

import (
	"reflect"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func physicalWithLabelledNIC(label string, binding bool) api.Catalog {
	objects := physicalCatalog().Objects()
	for index, object := range objects {
		if object.Kind() == api.Machine && object.Name() == "metal-01" {
			spec := object.Spec().
				WithPath(api.ListValue(api.MapValue(text("nicRef", label), text("macAddress", "aa:bb:cc:dd:ee:01"))), "substrate", "baremetal", "nics").
				WithPath(api.ListValue(api.MapValue(text("name", label))), "hardware", "nics").
				WithPath(api.StringValue(label), "hardware", "boot", "nicRef")
			if binding {
				spec = spec.WithPath(api.ListValue(api.MapValue(text("nicRef", label), text("interfaceName", "enp1s0"))), "network", "interfaceBinding")
			}
			objects[index] = object.WithSpec(spec)
		}
	}
	return api.NewCatalog(objects)
}

func firstHost(t *testing.T, media MediaRequest) map[string]any {
	t.Helper()
	hosts, _ := media.AgentConfig["hosts"].([]any)
	host, _ := hosts[0].(map[string]any)
	return host
}

func TestAgentConfigNamesAnInterfaceByItsBoundOperatingSystemName(t *testing.T) {
	identity, _, _ := onlyRequests(t, physicalWithLabelledNIC("enp1s0", true))
	bound, install, _ := onlyRequests(t, physicalWithLabelledNIC("nic0", true))
	want := []any{map[string]any{"name": "enp1s0", "macAddress": "aa:bb:cc:dd:ee:01"}}
	if got := firstHost(t, bound)["interfaces"]; !reflect.DeepEqual(got, want) {
		t.Fatalf("hosts[0].interfaces = %#v, want %#v", got, want)
	}
	if !reflect.DeepEqual(firstHost(t, bound), firstHost(t, identity)) {
		t.Fatalf("a bound label changed the host entry:\n%#v\n%#v", firstHost(t, bound), firstHost(t, identity))
	}
	config, _ := firstHost(t, bound)["networkConfig"].(map[string]any)
	found := false
	for _, raw := range config["interfaces"].([]any) {
		if entry := raw.(map[string]any); entry["name"] == "enp1s0" {
			found = entry["mac-address"] == "aa:bb:cc:dd:ee:01"
		}
	}
	if !found {
		t.Fatalf("networkConfig does not carry the bound NIC's address on enp1s0: %#v", config)
	}
	// The pre-boot proof still names the hardware NIC by its authored label.
	if got := install.Nodes[0].Hardware.Interfaces; len(got) != 1 || got[0].Name != "nic0" || got[0].MACAddress != "aa:bb:cc:dd:ee:01" {
		t.Fatalf("hardware proof interfaces = %+v", got)
	}
}

func TestADerivedIdentityBindingGivesTheSameHostEntryAsAnAuthoredOne(t *testing.T) {
	authored, _, _ := onlyRequests(t, physicalWithLabelledNIC("enp1s0", true))
	derived, _, _ := onlyRequests(t, physicalWithLabelledNIC("enp1s0", false))
	if !reflect.DeepEqual(firstHost(t, authored), firstHost(t, derived)) {
		t.Fatalf("authored and derived identity bindings differ:\n%#v\n%#v", firstHost(t, authored), firstHost(t, derived))
	}
}
