package nmstate

import "testing"

func TestMergeNamedSequenceMergesByName(t *testing.T) {
	base := map[string]any{"interfaces": []any{
		map[string]any{"name": "primary", "ipv4": map[string]any{"enabled": true}},
	}}
	patch := map[string]any{"interfaces": []any{
		map[string]any{"name": "primary", "mtu": 9000},
		map[string]any{"name": "secondary", "ipv4": map[string]any{"enabled": false}},
	}}
	Merge(base, patch)
	interfaces := base["interfaces"].([]any)
	if len(interfaces) != 2 {
		t.Fatalf("interfaces = %#v, want 2 entries", interfaces)
	}
	primary := interfaces[0].(map[string]any)
	if primary["mtu"] != 9000 {
		t.Fatalf("primary not merged by name: %#v", primary)
	}
	if primary["ipv4"].(map[string]any)["enabled"] != true {
		t.Fatalf("primary base field lost: %#v", primary)
	}
}

func TestMergePositionalSequenceMergesByIndex(t *testing.T) {
	base := map[string]any{"routes": []any{
		map[string]any{"destination": "0.0.0.0/0"},
	}}
	patch := map[string]any{"routes": []any{
		map[string]any{"next-hop-address": "192.0.2.1"},
	}}
	Merge(base, patch)
	route := base["routes"].([]any)[0].(map[string]any)
	if route["destination"] != "0.0.0.0/0" || route["next-hop-address"] != "192.0.2.1" {
		t.Fatalf("positional merge lost a field: %#v", route)
	}
}

func TestShapeErrorsRejectsMixedSequence(t *testing.T) {
	base := map[string]any{"interfaces": []any{map[string]any{"name": "primary"}}}
	patch := map[string]any{"interfaces": []any{"primary", map[string]any{"name": "secondary"}}}
	errs := ShapeErrors("overrides", base, patch)
	if len(errs) != 1 {
		t.Fatalf("ShapeErrors = %#v, want one error", errs)
	}
}

func TestEffectiveConfigInjectsInterfaceAddress(t *testing.T) {
	template := map[string]any{"interfaces": []any{
		map[string]any{"name": "primary", "ipv4": map[string]any{"enabled": true}},
	}}
	out := EffectiveConfig(template, nil, []InterfaceAddress{
		{Interface: "primary", Family: "ipv4", IP: "192.0.2.20", PrefixLength: 24},
	})
	if !InterfaceHasStaticIP(out, "primary") {
		t.Fatalf("injected address not present: %#v", out)
	}
	if template["interfaces"].([]any)[0].(map[string]any)["ipv4"].(map[string]any)["address"] != nil {
		t.Fatalf("EffectiveConfig mutated the input template")
	}
}

func TestSetInterfaceAddressEnablesDisabledFamily(t *testing.T) {
	config := map[string]any{"interfaces": []any{
		map[string]any{"name": "primary", "ipv6": map[string]any{"enabled": false}},
	}}
	SetInterfaceAddress(config, InterfaceAddress{
		Interface: "primary", Family: "ipv6", IP: "fd00:132::20", PrefixLength: 64,
	})
	family := config["interfaces"].([]any)[0].(map[string]any)["ipv6"].(map[string]any)
	if enabled, _ := family["enabled"].(bool); !enabled {
		t.Fatalf("family carrying a static address must be enabled: %#v", family)
	}
	if !InterfaceHasStaticIP(config, "primary") {
		t.Fatalf("injected ipv6 address not present: %#v", config)
	}
}

func TestInterfaceHasStaticIP(t *testing.T) {
	config := map[string]any{"interfaces": []any{
		map[string]any{"name": "primary", "ipv4": map[string]any{
			"address": []any{map[string]any{"ip": "192.0.2.20", "prefix-length": 24}},
		}},
		map[string]any{"name": "secondary", "ipv4": map[string]any{"enabled": true}},
	}}
	if !InterfaceHasStaticIP(config, "primary") {
		t.Fatalf("primary should report a static IP")
	}
	if InterfaceHasStaticIP(config, "secondary") {
		t.Fatalf("secondary has no static IP")
	}
}
