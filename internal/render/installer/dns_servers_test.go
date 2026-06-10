package installer

import (
	"reflect"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	stateview "github.com/crmarques/bootwright/internal/state/view"
)

func TestResolveClusterDNSServersAppendsNameResolutionRefs(t *testing.T) {
	state := dnsRefState(v1alpha1.EnvironmentNameResolutionComponent{
		Name:       "default",
		Management: v1alpha1.EnvironmentComponentExternal,
		Address:    "192.168.130.53",
	})
	ci := dnsRefInfra(state)
	got := ResolveClusterDNSServers(state, ci, state.NetworkConfigs[0])
	want := []string{"10.0.0.1", "192.168.130.53"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestResolveClusterDNSServersResolvesManagedBindAddress(t *testing.T) {
	state := dnsRefState(v1alpha1.EnvironmentNameResolutionComponent{
		Name:         "default",
		Management:   v1alpha1.EnvironmentComponentManaged,
		ComponentRef: v1alpha1.LocalObjectReference{Name: "dns"},
	})
	state.InfraComponents = []v1alpha1.InfraComponent{{
		Metadata: v1alpha1.Metadata{Name: "dns"},
		Spec: v1alpha1.InfraComponentSpec{
			Type: v1alpha1.ComponentSlotNameResolution, NameResolution: &v1alpha1.NameResolutionComponent{
				Implementation: v1alpha1.InfraComponentTypeDnsmasq,
				MachineRef:     v1alpha1.LocalObjectReference{Name: "host"},
				BindAddress:    "192.168.130.53",
				Port:           53,
			}},
	}}
	ci := dnsRefInfra(state)
	got := ResolveClusterDNSServers(state, ci, state.NetworkConfigs[0])
	want := []string{"10.0.0.1", "192.168.130.53"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestResolveClusterDNSServersDeduplicates(t *testing.T) {
	state := dnsRefState(v1alpha1.EnvironmentNameResolutionComponent{
		Name:       "default",
		Management: v1alpha1.EnvironmentComponentExternal,
		Address:    "10.0.0.1",
	})
	state.NetworkConfigs[0].Spec.Template.NetworkConfig["dns-resolver"].(map[string]any)["config"].(map[string]any)["server"] = []any{"10.0.0.1", "10.0.0.1"}
	ci := dnsRefInfra(state)
	got := ResolveClusterDNSServers(state, ci, state.NetworkConfigs[0])
	want := []string{"10.0.0.1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestAgentNetworkConfigAppendsNameResolutionRefs(t *testing.T) {
	state := dnsRefState(v1alpha1.EnvironmentNameResolutionComponent{
		Name:       "default",
		Management: v1alpha1.EnvironmentComponentExternal,
		Address:    "192.168.130.53",
	})
	ci := dnsRefInfra(state)
	got := AgentNetworkConfig(state, ci, ci.Machines[0], "")
	want := []string{"10.0.0.1", "192.168.130.53"}
	if servers := NetworkConfigDNSServers(got); !reflect.DeepEqual(servers, want) {
		t.Fatalf("got %v, want %v", servers, want)
	}
}

func TestAgentNetworkConfigCreatesDNSResolverForNameResolutionRefs(t *testing.T) {
	state := dnsRefState(v1alpha1.EnvironmentNameResolutionComponent{
		Name:       "default",
		Management: v1alpha1.EnvironmentComponentExternal,
		Address:    "192.168.130.53",
	})
	delete(state.NetworkConfigs[0].Spec.Template.NetworkConfig, "dns-resolver")
	ci := dnsRefInfra(state)
	got := AgentNetworkConfig(state, ci, ci.Machines[0], "")
	want := []string{"192.168.130.53"}
	if servers := NetworkConfigDNSServers(got); !reflect.DeepEqual(servers, want) {
		t.Fatalf("got %v, want %v", servers, want)
	}
}

func TestAgentNetworkConfigUsesMachineOverrideDNSServers(t *testing.T) {
	state := dnsRefState(v1alpha1.EnvironmentNameResolutionComponent{
		Name:       "default",
		Management: v1alpha1.EnvironmentComponentExternal,
		Address:    "192.168.130.53",
	})
	state.Machines[0].Spec.Network.Config.Overrides = map[string]any{
		"dns-resolver": map[string]any{"config": map[string]any{"server": []any{"10.0.0.2"}}},
	}
	ci := dnsRefInfra(state)
	got := AgentNetworkConfig(state, ci, ci.Machines[0], "")
	want := []string{"10.0.0.2", "192.168.130.53"}
	if servers := NetworkConfigDNSServers(got); !reflect.DeepEqual(servers, want) {
		t.Fatalf("got %v, want %v", servers, want)
	}
}

func TestAgentNetworkConfigMergesNamedInterfaceOverrides(t *testing.T) {
	state := dnsRefState(v1alpha1.EnvironmentNameResolutionComponent{Name: "default"})
	state.NetworkConfigs[0].Spec.Template.NetworkConfig["interfaces"] = []any{
		map[string]any{
			"name":  "primary",
			"type":  "ethernet",
			"state": "up",
			"ipv4": map[string]any{
				"enabled": true,
				"dhcp":    false,
			},
			"ipv6": map[string]any{
				"enabled": false,
			},
		},
		map[string]any{
			"name":        "secondary",
			"type":        "ethernet",
			"state":       "up",
			"description": "kept",
		},
		map[string]any{
			"name":  "bond0",
			"type":  "bond",
			"state": "up",
			"link-aggregation": map[string]any{
				"mode": "active-backup",
				"port": []any{"primary", "secondary"},
			},
		},
	}
	state.Machines[0].Spec.Network.Config.Overrides = map[string]any{
		"interfaces": []any{map[string]any{
			"name": "primary",
			"ipv4": map[string]any{
				"address": []any{map[string]any{"ip": "192.168.130.20", "prefix-length": 24}},
			},
		}},
	}

	ci := dnsRefInfra(state)
	got := AgentNetworkConfig(state, ci, ci.Machines[0], "")
	interfaces := got["interfaces"].([]any)
	if len(interfaces) != 3 {
		t.Fatalf("interfaces got %v, want three merged interfaces", interfaces)
	}
	primary := interfaces[0].(map[string]any)
	if primary["type"] != "ethernet" || primary["state"] != "up" {
		t.Fatalf("primary base fields were not preserved: %v", primary)
	}
	ipv4 := primary["ipv4"].(map[string]any)
	if ipv4["enabled"] != true || ipv4["dhcp"] != false {
		t.Fatalf("primary ipv4 base fields were not preserved: %v", ipv4)
	}
	addresses := ipv4["address"].([]any)
	address := addresses[0].(map[string]any)
	if address["ip"] != "192.168.130.20" || address["prefix-length"] != 24 {
		t.Fatalf("primary ipv4 address got %v", address)
	}
	ipv6 := primary["ipv6"].(map[string]any)
	if ipv6["enabled"] != false {
		t.Fatalf("primary ipv6 base fields were not preserved: %v", ipv6)
	}
	secondary := interfaces[1].(map[string]any)
	if secondary["name"] != "secondary" || secondary["description"] != "kept" {
		t.Fatalf("secondary interface was not preserved: %v", secondary)
	}
	bond := interfaces[2].(map[string]any)
	aggregation := bond["link-aggregation"].(map[string]any)
	if bond["name"] != "bond0" || aggregation["mode"] != "active-backup" {
		t.Fatalf("bond interface was not preserved: %v", bond)
	}
}

func TestAgentNetworkConfigMergesPositionalMapListOverrides(t *testing.T) {
	state := dnsRefState(v1alpha1.EnvironmentNameResolutionComponent{Name: "default"})
	state.NetworkConfigs[0].Spec.Template.NetworkConfig["routes"] = map[string]any{
		"config": []any{
			map[string]any{
				"destination":        "0.0.0.0/0",
				"next-hop-address":   "10.7.3.252",
				"next-hop-interface": "bond0.730",
				"table-id":           254,
				"metric":             400,
			},
			map[string]any{
				"destination":        "10.7.4.0/24",
				"next-hop-address":   "10.7.3.253",
				"next-hop-interface": "bond0.730",
			},
		},
	}
	state.Machines[0].Spec.Network.Config.Overrides = map[string]any{
		"routes": map[string]any{
			"config": []any{map[string]any{
				"next-hop-interface": "ens65f0",
			}},
		},
	}

	ci := dnsRefInfra(state)
	got := AgentNetworkConfig(state, ci, ci.Machines[0], "")
	routes := got["routes"].(map[string]any)["config"].([]any)
	if len(routes) != 2 {
		t.Fatalf("routes got %v, want two merged routes", routes)
	}
	defaultRoute := routes[0].(map[string]any)
	if defaultRoute["destination"] != "0.0.0.0/0" ||
		defaultRoute["next-hop-address"] != "10.7.3.252" ||
		defaultRoute["next-hop-interface"] != "ens65f0" ||
		defaultRoute["table-id"] != 254 ||
		defaultRoute["metric"] != 400 {
		t.Fatalf("default route was not merged field-by-field: %v", defaultRoute)
	}
	secondaryRoute := routes[1].(map[string]any)
	if secondaryRoute["destination"] != "10.7.4.0/24" ||
		secondaryRoute["next-hop-interface"] != "bond0.730" {
		t.Fatalf("secondary route was not preserved: %v", secondaryRoute)
	}
}

func dnsRefState(entry v1alpha1.EnvironmentNameResolutionComponent) v1alpha1.State {
	return v1alpha1.State{
		Environments: []v1alpha1.Environment{{
			Spec: v1alpha1.EnvironmentSpec{
				InfraComponents: v1alpha1.EnvironmentInfraComponentsSpec{
					NameResolution: []v1alpha1.EnvironmentNameResolutionComponent{entry},
				},
			},
		}},
		NetworkConfigs: []v1alpha1.NetworkConfig{{
			Metadata: v1alpha1.Metadata{Name: "lab-net"},
			Spec: v1alpha1.NetworkConfigSpec{
				MachineNetwork:     []v1alpha1.MachineNetworkCIDR{{CIDR: "192.168.130.0/24"}},
				NameResolutionRefs: []v1alpha1.LocalObjectReference{{Name: "default"}},
				Template: v1alpha1.NetworkConfigTemplate{
					NetworkConfig: map[string]any{
						"dns-resolver": map[string]any{"config": map[string]any{"server": []any{"10.0.0.1"}}},
					},
				},
			},
		}},
		Machines: []v1alpha1.Machine{{
			Metadata: v1alpha1.Metadata{Name: "master-0"},
			Spec: v1alpha1.MachineSpec{
				OS: v1alpha1.MachineOSSpec{
					Provided: v1alpha1.BoolPtr(false),
				},
				Network: v1alpha1.MachineNetwork{
					Config: v1alpha1.MachineNetworkConfig{
						NetworkConfigRef: v1alpha1.LocalObjectReference{Name: "lab-net"},
					},
				},
			},
		}},
		ContainerClusters: []v1alpha1.ContainerCluster{{
			Metadata: v1alpha1.Metadata{Name: "c1"},
			Spec: v1alpha1.ContainerClusterSpec{Hosts: []v1alpha1.OCPHostSpec{{
				Hostname:   "master-0",
				MachineRef: v1alpha1.LocalObjectReference{Name: "master-0"},
			}}},
		}},
	}
}

func dnsRefInfra(state v1alpha1.State) v1alpha1.ClusterInstall {
	ci, _ := stateview.ClusterInstallForContainerCluster(state, state.ContainerClusters[0])
	return ci
}
